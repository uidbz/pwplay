package player

import (
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// pumpRealtime plays seconds of audio through the fake sink at roughly the
// sound card's pace (the tracker never credits more than wall time).
func pumpRealtime(s *fakeSink, rate int, seconds float64) {
	const step = 10 * time.Millisecond
	frames := int(float64(rate) * step.Seconds())
	for elapsed := 0.0; elapsed < seconds; elapsed += step.Seconds() {
		s.pump(frames)
		time.Sleep(step)
	}
}

func TestPlayTrackerRecordsHeardTime(t *testing.T) {
	dir := t.TempDir()
	const rate = 4000
	a := writeWAV(t, dir, "a.wav", rate, 1, rate*12/10) // 1.2 s
	b := writeWAV(t, dir, "b.wav", rate, 1, rate*5/2)   // 2.5 s: fully buffered before the seek

	var mu sync.Mutex
	var recs []PlayRecord
	sink := &fakeSink{}
	p, err := NewPlayerWithOptions(nil, PlayerOptions{
		StartPaused: true, Sink: fakeSinkFactory(sink), PlayTrackInterval: 5 * time.Millisecond,
		OnPlay: func(r PlayRecord) { mu.Lock(); recs = append(recs, r); mu.Unlock() },
	})
	if err != nil {
		t.Fatal(err)
	}
	p.AddTrack(a)
	p.AddTrack(b)
	waitFor(t, 2*time.Second, "playlist", func() bool { return len(p.Playlist()) == 2 })
	p.Play()
	waitFor(t, 2*time.Second, "sink", func() bool { return sink.connected() > 0 })
	waitFor(t, 2*time.Second, "buffer", func() bool { return p.ringBuffer.Available() > rate })

	// All of a (gapless into b), then 1.2 s of b with a forward seek in the
	// middle: the skipped second must not count as heard.
	pumpRealtime(sink, rate, 1.8)
	waitFor(t, 2*time.Second, "b decoded to its end", p.IsEOF)
	p.Seek(1.5)
	waitFor(t, 2*time.Second, "seek", func() bool { return p.Position() >= 1.49 })
	waitFor(t, 2*time.Second, "refill", func() bool { return p.ringBuffer.Available() >= rate*9/10 })
	pumpRealtime(sink, rate, 0.6)
	p.Goto(0) // ends b's play
	waitFor(t, 2*time.Second, "goto", func() bool { return p.CurrentTrack() == 0 })
	time.Sleep(30 * time.Millisecond)
	p.Close() // the restarted a has < 1 s heard: dropped

	mu.Lock()
	defer mu.Unlock()
	if len(recs) != 2 {
		t.Fatalf("records = %+v, want 2", recs)
	}
	if filepath.Base(recs[0].URL) != "a.wav" || math.Abs(recs[0].Listened-1.2) > 0.1 || math.Abs(recs[0].Duration-1.2) > 0.01 {
		t.Errorf("a: %+v", recs[0])
	}
	// 0.6 s before the seek + 0.6 s after it.
	if filepath.Base(recs[1].URL) != "b.wav" || math.Abs(recs[1].Listened-1.2) > 0.15 || recs[1].Position < 2.0 {
		t.Errorf("b: %+v", recs[1])
	}
}

func TestPlayLogPersistsAndSequences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "history.jsonl")
	l, err := OpenPlayLog(path, "pwplay", 3)
	if err != nil {
		t.Fatal(err)
	}
	src := l.Source()
	for i := 0; i < 5; i++ {
		if r, err := l.Append(PlayRecord{URL: "u", Listened: float64(i)}); err != nil || r.Seq != uint64(i+1) {
			t.Fatalf("append %d: %+v %v", i, r, err)
		}
	}
	if got := l.Since(3); len(got) != 2 || got[0].Seq != 4 {
		t.Fatalf("since 3: %+v", got)
	}
	l2, err := OpenPlayLog(path, "pwplay", 3)
	if err != nil {
		t.Fatal(err)
	}
	if l2.Source() != src || l2.LastSeq() != 5 || len(l2.Since(0)) != 3 {
		t.Fatalf("reopen: source %s/%s seq %d n %d", l2.Source(), src, l2.LastSeq(), len(l2.Since(0)))
	}
	if r, _ := l2.Append(PlayRecord{URL: "v"}); r.Seq != 6 {
		t.Fatalf("seq after reopen %d", r.Seq)
	}
	mem, _ := OpenPlayLog("", "x", 0)
	if mem.Source() == src {
		t.Fatal("memory log reused a source")
	}
}
