package player

// playlog.go: play tracking. The player is the one place that knows exactly
// what was heard — the ring buffer's read clock advances only while audio
// actually leaves for the sound device, track boundaries are sample-accurate,
// and seeks/pauses consume nothing — so it reports finished plays itself
// (PlayerOptions.OnPlay) instead of leaving clients to infer them from
// polled positions. PlayLog is a small persistent, sequence-numbered log of
// those records that pwplay-server exposes over GET /history, so a client
// that was not running while the server played can still pick the plays up.

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// PlayRecord is one finished play of one playlist entry.
type PlayRecord struct {
	// Seq is assigned by PlayLog.Append (0 until then): strictly increasing
	// within one log Source.
	Seq       uint64    `json:"seq"`
	URL       string    `json:"url"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	// Listened is the audio actually heard, in seconds: seeks skip nothing
	// forward and replay nothing twice, pauses add nothing.
	Listened float64 `json:"listened"`
	// Duration is the track's length in seconds (0 when unknown).
	Duration float64 `json:"duration"`
	// Position is where playback left the track, in seconds.
	Position float64 `json:"position"`
}

// minRecordedListen drops plays too short to mean anything (a track loaded
// and immediately replaced).
const minRecordedListen = 1.0

// defaultPlayTrackInterval is how often the tracker samples the read clock.
const defaultPlayTrackInterval = 250 * time.Millisecond

// openPlay is the play being tracked.
type openPlay struct {
	url          string
	offset       uint64 // writeOffset of the boundary the play began at / last re-anchored to
	startedAt    time.Time
	lastConsumed uint64
	listened     float64
	duration     float64
	position     float64
	rate, chans  int
}

// playTracker samples the player and emits finished plays.
type playTracker struct {
	p        *Player
	onPlay   func(PlayRecord)
	interval time.Duration
	stop     chan struct{}
	done     chan struct{}
	cur      *openPlay
	stopped  atomic.Bool
	// lastTick and budget cap what one sample may credit: audio cannot be
	// heard faster than wall time, so a read-clock jump (the ring cleared by
	// a seek or a track load fast-forwards it past samples never played) is
	// not mistaken for listening.
	lastTick time.Time
	budget   float64
}

func newPlayTracker(p *Player, onPlay func(PlayRecord), interval time.Duration) *playTracker {
	if interval <= 0 {
		interval = defaultPlayTrackInterval
	}
	return &playTracker{p: p, onPlay: onPlay, interval: interval, stop: make(chan struct{}), done: make(chan struct{})}
}

func (t *playTracker) run() {
	defer close(t.done)
	tick := time.NewTicker(t.interval)
	defer tick.Stop()
	for {
		select {
		case <-t.stop:
			t.sample()
			t.finish()
			return
		case <-tick.C:
			t.sample()
		}
	}
}

// close stops the tracker, flushing the open play. Idempotent.
func (t *playTracker) close() {
	if t.stopped.CompareAndSwap(false, true) {
		close(t.stop)
	}
	<-t.done
}

func (t *playTracker) finish() {
	c := t.cur
	t.cur = nil
	if c == nil || c.listened < minRecordedListen {
		return
	}
	t.onPlay(PlayRecord{
		URL:       c.url,
		StartedAt: c.startedAt,
		EndedAt:   time.Now(),
		Listened:  c.listened,
		Duration:  c.duration,
		Position:  c.position,
	})
}

// accrue credits the open play with the samples consumed up to upTo.
func (t *playTracker) accrue(upTo uint64) {
	c := t.cur
	if c == nil {
		return
	}
	if upTo > c.lastConsumed && c.rate > 0 && c.chans > 0 {
		sec := float64(upTo-c.lastConsumed) / float64(c.chans) / float64(c.rate)
		if sec > t.budget {
			sec = t.budget
		}
		c.listened += sec
		t.budget -= sec
	}
	if upTo > c.lastConsumed {
		c.lastConsumed = upTo
	}
}

// playbackEnded reports whether nothing is audible any more. The decoder
// sets stopped as soon as it has read the last track to its end, while the
// ring still holds up to its full length of that track's tail: the play goes
// on until the tail has drained (or a seek resumes decoding).
func (p *Player) playbackEnded() bool {
	if !p.IsStopped() {
		return false
	}
	return atomic.LoadInt32(&p.endStop) == 0 || p.ringBuffer.Available() == 0
}

func (t *playTracker) sample() {
	p := t.p
	now := time.Now()
	if t.lastTick.IsZero() {
		t.lastTick = now
	}
	t.budget = now.Sub(t.lastTick).Seconds()*1.05 + 0.02
	t.lastTick = now
	b, consumed, ok := p.activeBoundary()
	url := ""
	if ok {
		p.mu.RLock()
		if b.track >= 0 && b.track < len(p.playlist) {
			url = p.playlist[b.track]
		}
		p.mu.RUnlock()
	}
	if !ok || url == "" || p.playbackEnded() {
		t.accrue(consumed)
		t.finish()
		return
	}
	// A new play starts when the heard entry changes, or the same entry is
	// re-anchored at its start (restart, repeat, a gapless repeat of the same
	// URL). A re-anchor mid-track is a seek: same play.
	if c := t.cur; c != nil && (c.url != url || (b.writeOffset != c.offset && b.startSample == 0)) {
		// Samples before the new boundary still belong to the old play.
		limit := consumed
		if b.writeOffset < limit {
			limit = b.writeOffset
		}
		t.accrue(limit)
		t.finish()
	}
	if t.cur == nil {
		t.cur = &openPlay{url: url, offset: b.writeOffset, startedAt: now, lastConsumed: b.writeOffset}
	}
	c := t.cur
	if b.writeOffset != c.offset {
		// Seek: the ring was cleared and the read clock jumped to the new
		// anchor. Nothing between the old read position and it was heard.
		c.offset = b.writeOffset
		if b.writeOffset > c.lastConsumed {
			c.lastConsumed = b.writeOffset
		}
	}
	c.rate, c.chans = b.rate, b.channels
	if b.duration > 0 && b.rate > 0 {
		c.duration = float64(b.duration) / float64(b.rate)
	}
	t.accrue(consumed)
	// From the same snapshot: a separate Position() call could already see
	// the next track's boundary (a Goto landing between the two reads).
	c.position = b.position(consumed)
	if p.IsEOF() && p.playbackEnded() {
		t.finish()
	}
}

// PlayLog is a bounded, sequence-numbered log of plays, optionally persisted
// as JSON lines. The first line of the file is a header naming the log's
// Source: a stable identity minted when the file is created, so a consumer
// can key de-duplication on (Source, Seq) — deleting the file mints a new
// source and sequence numbers may safely restart.
type PlayLog struct {
	mu      sync.Mutex
	path    string
	source  string
	seq     uint64
	records []PlayRecord
	max     int
	onDisk  int
}

type playLogHeader struct {
	Source string `json:"source"`
}

// DefaultPlayLogSize is how many records a PlayLog keeps.
const DefaultPlayLogSize = 10000

// NewPlayLogSource mints a source identity: prefix@hostname/<random>.
func NewPlayLogSource(prefix string) string {
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown"
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s@%s/%s", prefix, host, hex.EncodeToString(b[:]))
}

// OpenPlayLog opens (or creates) the log at path, keeping at most max
// records (<= 0: DefaultPlayLogSize). An empty path keeps the log in memory
// only, under a fresh source. prefix names new sources ("pwplay", …).
func OpenPlayLog(path, prefix string, max int) (*PlayLog, error) {
	if max <= 0 {
		max = DefaultPlayLogSize
	}
	l := &PlayLog{path: path, max: max}
	if path == "" {
		l.source = NewPlayLogSource(prefix)
		return l, nil
	}
	f, err := os.Open(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		l.source = NewPlayLogSource(prefix)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := l.rewrite(); err != nil {
			return nil, err
		}
		return l, nil
	case err != nil:
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	first := true
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if first {
			first = false
			var h playLogHeader
			if err := json.Unmarshal(line, &h); err != nil || h.Source == "" {
				return nil, fmt.Errorf("play log %s: bad header", path)
			}
			l.source = h.Source
			continue
		}
		var r PlayRecord
		if err := json.Unmarshal(line, &r); err != nil {
			continue // a torn last line after a crash
		}
		l.records = append(l.records, r)
		l.onDisk++
		if r.Seq > l.seq {
			l.seq = r.Seq
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if l.source == "" {
		l.source = NewPlayLogSource(prefix)
		if err := l.rewrite(); err != nil {
			return nil, err
		}
	}
	if len(l.records) > l.max {
		l.records = l.records[len(l.records)-l.max:]
	}
	return l, nil
}

// Source returns the log's identity.
func (l *PlayLog) Source() string { return l.source }

// LastSeq returns the newest sequence number (0 = empty).
func (l *PlayLog) LastSeq() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq
}

// Append assigns the next sequence number to r, stores and persists it, and
// returns it.
func (l *PlayLog) Append(r PlayRecord) (PlayRecord, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	r.Seq = l.seq
	l.records = append(l.records, r)
	if len(l.records) > l.max {
		l.records = l.records[len(l.records)-l.max:]
	}
	if l.path == "" {
		return r, nil
	}
	if l.onDisk+1 > 2*l.max {
		return r, l.rewrite()
	}
	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return r, err
	}
	defer f.Close()
	line, _ := json.Marshal(r)
	if _, err := f.Write(append(line, '\n')); err != nil {
		return r, err
	}
	l.onDisk++
	return r, nil
}

// rewrite replaces the file with the header and the retained records.
// Called with l.mu held (or before the log is shared).
func (l *PlayLog) rewrite() error {
	tmp := l.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	_ = enc.Encode(playLogHeader{Source: l.source})
	for _, r := range l.records {
		_ = enc.Encode(r)
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	l.onDisk = len(l.records)
	return os.Rename(tmp, l.path)
}

// Since returns the records with Seq > seq, oldest first.
func (l *PlayLog) Since(seq uint64) []PlayRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []PlayRecord
	for _, r := range l.records {
		if r.Seq > seq {
			out = append(out, r)
		}
	}
	return out
}
