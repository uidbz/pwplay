package player

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

// Lock-free ring buffer using atomic operations for single producer/consumer.
//
// read and write are MONOTONIC total interleaved-sample counts (never wrap);
// the physical slot is index = pos % size. Because they only ever advance,
// write serves as a global "samples ever produced" clock and read as a global
// "samples ever consumed" clock. Track-boundary markers are anchored to write
// offsets and resolved against read (see trackBoundary), which is what makes
// position/track reporting follow the audio actually leaving the ring rather
// than the decoder that runs up to a full buffer ahead of it.
type RingBuffer struct {
	buffer []float32
	size   int
	read   uint64 // atomic; monotonic count of interleaved samples consumed
	write  uint64 // atomic; monotonic count of interleaved samples produced
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buffer: make([]float32, size),
		size:   size,
	}
}

func (rb *RingBuffer) Write(samples []float32) int {
	written := 0
	for _, sample := range samples {
		writePos := atomic.LoadUint64(&rb.write)
		readPos := atomic.LoadUint64(&rb.read)

		// Full when the unread span equals capacity.
		if writePos-readPos >= uint64(rb.size) {
			break
		}

		rb.buffer[writePos%uint64(rb.size)] = sample
		atomic.StoreUint64(&rb.write, writePos+1)
		written++
	}
	return written
}

func (rb *RingBuffer) Read(samples []float32) int {
	read := 0
	for i := range samples {
		readPos := atomic.LoadUint64(&rb.read)
		writePos := atomic.LoadUint64(&rb.write)

		// Empty when read has caught up to write.
		if readPos == writePos {
			break
		}

		samples[i] = rb.buffer[readPos%uint64(rb.size)]
		atomic.StoreUint64(&rb.read, readPos+1)
		read++
	}
	return read
}

func (rb *RingBuffer) Available() int {
	return int(atomic.LoadUint64(&rb.write) - atomic.LoadUint64(&rb.read))
}

// Clear discards all pending (written-but-unread) samples while keeping the
// monotonic counters intact: read is advanced to write rather than both being
// zeroed. This preserves the global sample clock across seeks/track-loads so
// boundary markers anchored to write offsets stay valid.
func (rb *RingBuffer) Clear() {
	atomic.StoreUint64(&rb.read, atomic.LoadUint64(&rb.write))
}

// WritePos returns the monotonic count of interleaved samples produced so far.
func (rb *RingBuffer) WritePos() uint64 { return atomic.LoadUint64(&rb.write) }

// ReadPos returns the monotonic count of interleaved samples consumed so far.
func (rb *RingBuffer) ReadPos() uint64 { return atomic.LoadUint64(&rb.read) }

type Player struct {
	stream       Sink
	ringBuffer   *RingBuffer
	playlist     []string
	currentTrack int32
	currentFile  AudioDecoder
	nextFile     AudioDecoder
	paused       int32
	stopped      int32
	stopReset    int32
	eof          int32
	nextTrack    int32
	prevTrack    int32
	gotoTrack    int32 // atomic; -1 = no request, else the index to jump to
	addTrack     chan string
	removeTrack  chan int
	clearTracks  chan struct{}
	stopDecode   chan bool
	decodeDone   chan bool
	// sampleRate/channels are int32 accessed atomically: they are written by
	// the decoder thread when the first track is loaded (lazy stream creation)
	// and read by HTTP handlers via Position/Seek/TrackDuration.
	sampleRate int32
	channels   int32
	// streamOpts is retained so the stream can be created lazily on the first
	// track load when the player is constructed with an empty playlist.
	streamOpts  SinkOptions
	sinkFactory SinkFactory
	mu          sync.RWMutex

	// Seek support: seekTarget holds the target sample position for a pending
	// seek, or -1 when no seek is pending. The decoder thread polls this value
	// and performs the seek when it finds a non-negative value.
	seekTarget int64 // atomic; -1 = no pending seek

	// Position/track reporting is driven by the ring buffer's consumer clock,
	// not the decoder. The decoder runs up to a full buffer (~3s) ahead of the
	// audio actually leaving the ring, so resetting counters when the DECODER
	// crosses a track boundary reports the new track ~3s before you hear it and
	// mis-attributes the outgoing track's buffered tail to the incoming track.
	//
	// Instead, every time the decoder starts feeding a track's samples it
	// records a boundary anchored to the ring's write offset (the exact sample
	// count at which that track's first sample lands). Position()/CurrentTrack()
	// then resolve the boundary whose write offset the consumer (read clock) has
	// actually reached, so reporting flips precisely when the audio does.
	boundaries []trackBoundary
	boundMu    sync.Mutex

	// Volume as a linear gain factor stored atomically as uint32 (float32 bits).
	// 0.0 = silent, 1.0 = unity gain (default), values > 1.0 = amplify.
	volume uint32 // atomic; stores math.Float32bits(gain)

	// Passthrough mode: exclusive device access, no PipeWire resampling/mixing,
	// no software volume. Enables bit-perfect output.
	passthrough bool
}

// trackBoundary marks where a track's audio begins within the ring buffer's
// monotonic output stream. writeOffset is the ring write count at which the
// track's first sample was produced; the consumer reaches it exactly when all
// prior tracks' samples have been played, so track transitions in the reported
// position/number line up sample-accurately with the audio.
type trackBoundary struct {
	writeOffset uint64 // ring.write value where this track's first sample lands
	track       int    // playlist index
	startSample int64  // per-channel position within the track at writeOffset
	duration    int64  // per-channel total samples, or -1 if unknown
}

// pushBoundary records a new track boundary at the current ring write offset.
// Used for gapless transitions, where earlier tracks' samples are still queued
// ahead of this one.
func (p *Player) pushBoundary(track int, startSample, duration int64) {
	off := p.ringBuffer.WritePos()
	p.boundMu.Lock()
	p.boundaries = append(p.boundaries, trackBoundary{off, track, startSample, duration})
	p.boundMu.Unlock()
}

// resetBoundaries replaces the boundary list with a single entry at the current
// ring write offset. Used after the ring is cleared (seek, stop-rewind,
// loadTrack) when no prior audio remains queued, so reporting snaps immediately
// to the new track/position.
func (p *Player) resetBoundaries(track int, startSample, duration int64) {
	off := p.ringBuffer.WritePos()
	p.boundMu.Lock()
	p.boundaries = []trackBoundary{{off, track, startSample, duration}}
	p.boundMu.Unlock()
}

// activeBoundary returns the boundary the consumer is currently within (the
// last one whose write offset the read clock has reached), the current consumed
// sample count, and whether a boundary exists. Fully-consumed boundaries are
// pruned as a side effect.
func (p *Player) activeBoundary() (trackBoundary, uint64, bool) {
	consumed := p.ringBuffer.ReadPos()
	p.boundMu.Lock()
	defer p.boundMu.Unlock()

	if len(p.boundaries) == 0 {
		return trackBoundary{}, consumed, false
	}

	active := 0
	for i, b := range p.boundaries {
		if b.writeOffset <= consumed {
			active = i
		} else {
			break
		}
	}
	if active > 0 {
		p.boundaries = p.boundaries[active:]
	}
	return p.boundaries[0], consumed, true
}

// playbackTrack returns the playlist index of the track currently being heard,
// falling back to the decoder's index before any boundary is recorded.
func (p *Player) playbackTrack() int {
	if b, _, ok := p.activeBoundary(); ok {
		return b.track
	}
	return int(atomic.LoadInt32(&p.currentTrack))
}

// ExpandPlaylist takes a list of paths (files or directories) and returns
// a list of audio files. Directories are scanned recursively for supported formats.
func ExpandPlaylist(paths []string) ([]string, error) {
	var files []string
	supported := map[string]bool{
		".flac": true,
		".mp3":  true,
		".wav":  true,
		".ogg":  true,
		".opus": true,
	}

	for _, path := range paths {
		// Pass HTTP(S) URLs through without filesystem checks
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			files = append(files, path)
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("cannot access %s: %w", path, err)
		}

		if info.IsDir() {
			// Walk directory recursively
			var dirFiles []string
			err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					ext := strings.ToLower(filepath.Ext(filePath))
					if supported[ext] {
						dirFiles = append(dirFiles, filePath)
					}
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("error scanning directory %s: %w", path, err)
			}
			// Sort only within a directory expansion so a filesystem walk yields
			// a stable order. Explicitly listed paths keep their caller-provided
			// order — an album's tracks arrive pre-ordered and must not be
			// re-sorted (their stream URLs would otherwise sort by content hash).
			sort.Strings(dirFiles)
			files = append(files, dirFiles...)
		} else {
			// Single file
			ext := strings.ToLower(filepath.Ext(path))
			if supported[ext] {
				files = append(files, path)
			} else {
				return nil, fmt.Errorf("unsupported file format: %s", path)
			}
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no audio files found")
	}

	return files, nil
}

// PlayerOptions configures optional player behavior.
type PlayerOptions struct {
	// StartPaused starts the player in a paused state.
	StartPaused bool
	// Passthrough disables software volume control, prevents PipeWire
	// channel remixing, and requests the audio device run at the
	// stream's native sample rate.
	Passthrough bool
	// Exclusive requests sole access to the audio device. Other
	// streams are disconnected while playing. May cause silence if
	// the session manager cannot grant exclusive access.
	Exclusive bool
	// Sink overrides the audio output factory; nil uses the platform
	// default (PipeWire on Linux, OpenSL ES on Android). Used by tests
	// to drive the player's audio callback without a sound device.
	Sink SinkFactory
}

func NewPlayer(files []string, startPaused bool) (*Player, error) {
	return NewPlayerWithOptions(files, PlayerOptions{StartPaused: startPaused})
}

// NewPlayerWithOptions creates a player for the given files. files may be
// empty, in which case the player starts with an empty playlist and the
// PipeWire stream is created lazily when the first track is loaded (see
// AddTrack), so the stream adopts that track's native sample rate and channel
// layout. Until then a default format (44100 Hz, stereo) is reported.
func NewPlayerWithOptions(files []string, opts PlayerOptions) (*Player, error) {
	var firstFile AudioDecoder
	sampleRate, channels := 44100, 2 // default until the first track is loaded
	if len(files) > 0 {
		var err error
		firstFile, err = OpenAudioFile(files[0])
		if err != nil {
			return nil, err
		}
		sampleRate = firstFile.SampleRate()
		channels = firstFile.Channels()
	}

	// Ring capacity only controls buffering depth, not correctness; when
	// starting empty it is sized for the default format and simply holds a
	// little more or less than 3s of audio once a real track sets the rate.
	bufferSize := sampleRate * channels * 3

	sinkFactory := opts.Sink
	if sinkFactory == nil {
		sinkFactory = platformSink
	}

	p := &Player{
		ringBuffer:  NewRingBuffer(bufferSize),
		playlist:    files,
		currentFile: firstFile,
		sampleRate:  int32(sampleRate),
		channels:    int32(channels),
		streamOpts: SinkOptions{
			Passthrough: opts.Passthrough,
			Exclusive:   opts.Exclusive,
		},
		sinkFactory: sinkFactory,
		addTrack:    make(chan string, 10),
		removeTrack: make(chan int, 10),
		clearTracks: make(chan struct{}, 1),
		stopDecode:  make(chan bool),
		decodeDone:  make(chan bool),
		passthrough: opts.Passthrough,
	}

	atomic.StoreInt64(&p.seekTarget, -1)
	atomic.StoreInt32(&p.gotoTrack, -1)
	atomic.StoreUint32(&p.volume, math.Float32bits(1.0))

	if opts.StartPaused {
		atomic.StoreInt32(&p.paused, 1)
	}

	if firstFile == nil {
		// Empty playlist: no stream yet. The decoder thread handles add/remove
		// and creates the stream on the first successful track load.
		go p.decoderThread()
		return p, nil
	}

	// Anchor track 0 at the start of the (empty) ring's output stream.
	p.resetBoundaries(0, 0, firstFile.Duration())

	format := Format{SampleRate: sampleRate, Channels: channels}

	pwStream, err := p.sinkFactory("Audio Player", format, p.processCallback, p.streamOpts)
	if err != nil {
		firstFile.Close()
		return nil, err
	}

	p.stream = pwStream
	go p.decoderThread()

	if err := pwStream.Connect(format); err != nil {
		close(p.stopDecode)
		firstFile.Close()
		return nil, err
	}

	return p, nil
}

func (p *Player) decoderThread() {
	defer close(p.decodeDone)

	for {
		select {
		case <-p.stopDecode:
			return
		case path := <-p.addTrack:
			p.mu.Lock()
			p.playlist = append(p.playlist, path)
			p.mu.Unlock()
			// If we were at EOF, clear it so the new track can be played
			if atomic.LoadInt32(&p.eof) == 1 {
				atomic.StoreInt32(&p.eof, 0)
			}
		case idx := <-p.removeTrack:
			p.mu.Lock()
			if idx >= 0 && idx < len(p.playlist) {
				cur := int(atomic.LoadInt32(&p.currentTrack))
				p.playlist = append(p.playlist[:idx], p.playlist[idx+1:]...)
				newLen := len(p.playlist)
				switch {
				case idx < cur:
					// Removed a track before the current one; indices shifted
					// down, so follow the still-playing track.
					atomic.StoreInt32(&p.currentTrack, int32(cur-1))
				case idx == cur:
					// Removed the current track. Drop its decoder; the loop
					// reloads whatever now occupies this slot (or stops if the
					// queue is now empty). currentTrack is clamped into range.
					if p.currentFile != nil {
						p.currentFile.Close()
						p.currentFile = nil
					}
					if p.nextFile != nil {
						p.nextFile.Close()
						p.nextFile = nil
					}
					if newLen == 0 {
						atomic.StoreInt32(&p.currentTrack, 0)
						atomic.StoreInt32(&p.stopped, 1)
						atomic.StoreInt32(&p.eof, 1)
						// The queue is empty: no boundary may survive indexing
						// into the old playlist.
						p.boundMu.Lock()
						p.boundaries = nil
						p.boundMu.Unlock()
					} else if cur >= newLen {
						atomic.StoreInt32(&p.currentTrack, int32(newLen-1))
					}
				}
			}
			p.mu.Unlock()
		case <-p.clearTracks:
			// A single clear replaces the whole playlist in one decoder-loop
			// step, so the UI's Clear is instant even mid-play (unlike N
			// one-at-a-time removes, which the loop drains between decode
			// iterations).
			p.mu.Lock()
			p.playlist = p.playlist[:0]
			if p.currentFile != nil {
				p.currentFile.Close()
				p.currentFile = nil
			}
			if p.nextFile != nil {
				p.nextFile.Close()
				p.nextFile = nil
			}
			p.mu.Unlock()
			// Silence the ring immediately: it can hold seconds of buffered
			// audio, which would otherwise keep playing after the clear.
			p.ringBuffer.Clear()
			// Drop recorded boundaries too: they index into the old playlist,
			// so reporting would otherwise keep returning a stale track number
			// for an empty queue.
			p.boundMu.Lock()
			p.boundaries = nil
			p.boundMu.Unlock()
			atomic.StoreInt32(&p.currentTrack, 0)
			atomic.StoreInt32(&p.stopped, 1)
			atomic.StoreInt32(&p.eof, 1)
			atomic.StoreInt64(&p.seekTarget, -1)
		default:
		}

		// Handle pending seek
		target := atomic.LoadInt64(&p.seekTarget)
		if target >= 0 {
			p.mu.RLock()
			currentFile := p.currentFile
			p.mu.RUnlock()

			if currentFile != nil {
				err := currentFile.Seek(target)
				if err != nil {
					log.Printf("Seek failed: %v", err)
				} else {
					// Clear ring buffer so stale audio is discarded immediately,
					// then re-anchor the current track at the seek target so the
					// reported position snaps there.
					p.ringBuffer.Clear()
					p.resetBoundaries(int(atomic.LoadInt32(&p.currentTrack)), target, currentFile.Duration())
				}
			}
			// Clear the seek request regardless of success
			atomic.StoreInt64(&p.seekTarget, -1)
			continue
		}

		// Stop rewinds the current track to its start. Handled here — before
		// the stopped/paused gate — so it takes effect even though Stop() also
		// sets stopped=1. Position tracking is reset unconditionally so the
		// reported position snaps to 0 whether or not a decoder is loaded (the
		// seekTarget path did nothing when currentFile was nil, which is why
		// Stop left the position frozen). Doing the rewind entirely inside the
		// loop also avoids racing Stop()'s writes against the decoder.
		if atomic.LoadInt32(&p.stopReset) == 1 {
			atomic.StoreInt32(&p.stopReset, 0)
			p.mu.RLock()
			currentFile := p.currentFile
			p.mu.RUnlock()
			dur := int64(-1)
			if currentFile != nil {
				if err := currentFile.Seek(0); err != nil {
					log.Printf("Stop rewind failed: %v", err)
				}
				dur = currentFile.Duration()
			}
			p.ringBuffer.Clear()
			p.resetBoundaries(int(atomic.LoadInt32(&p.currentTrack)), 0, dur)
			atomic.StoreInt64(&p.seekTarget, -1)
			continue
		}

		// Track navigation is honored even while paused or stopped so the user
		// can skip around a finished or paused queue. A successful load clears
		// the stopped flag and starts the freshly loaded track playing.
		// Navigate relative to the track being HEARD (playbackTrack), not the
		// decoder's index. The decoder can be up to a track ahead during a
		// gapless transition; skipping from the decoder index would jump over
		// the track the user currently sees playing.
		// A jump to an explicit index (double-click a queue row). Like next/prev
		// it is honored while paused or stopped, and a successful load starts the
		// target track playing.
		if g := atomic.LoadInt32(&p.gotoTrack); g >= 0 {
			atomic.StoreInt32(&p.gotoTrack, -1)
			if p.loadTrack(int(g)) {
				atomic.StoreInt32(&p.stopped, 0)
				atomic.StoreInt32(&p.eof, 0)
			}
			continue
		}

		if atomic.LoadInt32(&p.nextTrack) == 1 {
			atomic.StoreInt32(&p.nextTrack, 0)
			if p.loadTrack(p.playbackTrack() + 1) {
				atomic.StoreInt32(&p.stopped, 0)
				atomic.StoreInt32(&p.eof, 0)
			}
			continue
		}

		if atomic.LoadInt32(&p.prevTrack) == 1 {
			atomic.StoreInt32(&p.prevTrack, 0)
			if p.loadTrack(p.playbackTrack() - 1) {
				atomic.StoreInt32(&p.stopped, 0)
				atomic.StoreInt32(&p.eof, 0)
			}
			continue
		}

		if atomic.LoadInt32(&p.stopped) == 1 || atomic.LoadInt32(&p.paused) == 1 {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		p.mu.RLock()
		currentFile := p.currentFile
		p.mu.RUnlock()

		if currentFile == nil {
			// We are meant to be playing (not paused/stopped) but no decoder is
			// loaded — e.g. the current track was just removed, or playback is
			// restarting from the top. Load the track at the current index.
			if p.loadTrack(int(atomic.LoadInt32(&p.currentTrack))) {
				atomic.StoreInt32(&p.eof, 0)
			} else {
				atomic.StoreInt32(&p.stopped, 1)
				atomic.StoreInt32(&p.eof, 1)
				time.Sleep(10 * time.Millisecond)
			}
			continue
		}

		// Read samples directly from decoder
		sampleBuf := make([]float32, 4096)
		n, err := currentFile.ReadSamples(sampleBuf)
		if err != nil {
			if err == io.EOF {
				// Current track finished. Advance to the next track WITHOUT
				// clearing the ring: the outgoing track's tail is still queued,
				// and the next track's samples must be appended behind it. Prefer
				// the preloaded decoder; if preload missed (it is timing-driven
				// and unreliable), open the next track inline here. Clearing the
				// ring on natural advance — as loadTrack does — would drop the
				// tail, causing a gap and a truncated track, which is exactly the
				// bug this path replaces.
				p.mu.Lock()
				next := p.nextFile
				p.nextFile = nil
				if next == nil {
					nextIdx := int(atomic.LoadInt32(&p.currentTrack)) + 1
					if nextIdx < len(p.playlist) {
						f, e := OpenAudioFile(p.playlist[nextIdx])
						if e != nil {
							log.Printf("Failed to open next track %d: %v", nextIdx+1, e)
						} else {
							next = f
						}
					}
				}
				if next != nil {
					if p.currentFile != nil {
						p.currentFile.Close()
					}
					p.currentFile = next
					newTrack := int(atomic.AddInt32(&p.currentTrack, 1))
					// Anchor the new track at the current write offset; the
					// consumer reaches it exactly when the queued tail finishes.
					p.pushBoundary(newTrack, 0, next.Duration())
					log.Printf("Gapless advance to track %d", newTrack+1)
					p.mu.Unlock()
					continue
				}
				p.mu.Unlock()

				// No further track: stop once the queued tail drains.
				atomic.StoreInt32(&p.stopped, 1)
				atomic.StoreInt32(&p.eof, 1)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Preload next track when buffer is getting low
		available := p.ringBuffer.Available()
		bufferCapacity := int(atomic.LoadInt32(&p.sampleRate)) * int(atomic.LoadInt32(&p.channels)) * 3
		if available < bufferCapacity/2 {
			p.mu.Lock()
			currentIdx := int(atomic.LoadInt32(&p.currentTrack))
			if p.nextFile == nil && currentIdx+1 < len(p.playlist) {
				go p.preloadNextTrack()
			}
			p.mu.Unlock()
		}

		// Write samples to ring buffer
		written := 0
		for written < n {
			// Check for pending seek while writing - abort to handle it immediately
			if atomic.LoadInt64(&p.seekTarget) >= 0 {
				break
			}
			w := p.ringBuffer.Write(sampleBuf[written:n])
			written += w
			if w == 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}
	}
}

func (p *Player) preloadNextTrack() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.nextFile != nil {
		return
	}

	nextIdx := int(atomic.LoadInt32(&p.currentTrack)) + 1
	if nextIdx >= len(p.playlist) {
		return
	}

	nextPath := p.playlist[nextIdx]
	file, err := OpenAudioFile(nextPath)
	if err != nil {
		log.Printf("Failed to preload track %d: %v", nextIdx+1, err)
		return
	}

	p.nextFile = file
	log.Printf("Preloaded next track: %s", filepath.Base(nextPath))
}

func (p *Player) loadTrack(idx int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if idx < 0 || idx >= len(p.playlist) {
		return false
	}

	if p.currentFile != nil {
		p.currentFile.Close()
	}

	if p.nextFile != nil {
		p.nextFile.Close()
		p.nextFile = nil
	}

	file, err := OpenAudioFile(p.playlist[idx])
	if err != nil {
		log.Printf("Failed to load track %d: %v", idx+1, err)
		return false
	}

	// Lazily create the audio sink on the very first track load so the
	// stream uses the track's native sample rate and channel layout. This is
	// what allows the player to start with an empty playlist. The stream
	// format is fixed from then on (tracks with a different format play at
	// the wrong speed — a documented limitation).
	if p.stream == nil {
		rate, ch := file.SampleRate(), file.Channels()
		// Publish the format before Connect starts the thread loop: the first
		// process callback can run immediately after and reads these values.
		atomic.StoreInt32(&p.sampleRate, int32(rate))
		atomic.StoreInt32(&p.channels, int32(ch))
		format := Format{SampleRate: rate, Channels: ch}
		stream, err := p.sinkFactory("Audio Player", format, p.processCallback, p.streamOpts)
		if err != nil {
			log.Printf("Failed to create stream: %v", err)
			file.Close()
			return false
		}
		if err := stream.Connect(format); err != nil {
			log.Printf("Failed to connect stream: %v", err)
			stream.Destroy()
			file.Close()
			return false
		}
		p.stream = stream
		log.Printf("Stream started: %d Hz, %d ch", rate, ch)
	}

	p.currentFile = file
	atomic.StoreInt32(&p.currentTrack, int32(idx))
	// A direct load (next/prev/removal reload) discards queued audio, so clear
	// the ring and re-anchor this track at the current output offset.
	p.ringBuffer.Clear()
	p.resetBoundaries(idx, 0, file.Duration())

	log.Printf("Now playing [%d/%d]: %s", idx+1, len(p.playlist), filepath.Base(p.playlist[idx]))
	return true
}

func (p *Player) processCallback(buffer []byte, frames int) int {
	if atomic.LoadInt32(&p.paused) == 1 {
		for i := range buffer {
			buffer[i] = 0
		}
		return frames
	}

	output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])), frames*int(atomic.LoadInt32(&p.channels)))
	read := p.ringBuffer.Read(output)
	// The ring's monotonic read clock (advanced inside Read) is the playback
	// position; no separate counter is needed.

	// Apply volume gain
	gain := math.Float32frombits(atomic.LoadUint32(&p.volume))
	if gain != 1.0 {
		for i := 0; i < read; i++ {
			output[i] *= gain
		}
	}

	for i := read; i < len(output); i++ {
		output[i] = 0
	}

	return frames
}

// Control methods
func (p *Player) Play() {
	// If the queue previously played past its end, restart from the top: drop
	// the exhausted decoder and point at track 0 so the decoder loop reloads it.
	if atomic.LoadInt32(&p.eof) == 1 {
		atomic.StoreInt32(&p.currentTrack, 0)
		atomic.StoreInt32(&p.eof, 0)
		p.mu.Lock()
		if p.currentFile != nil {
			p.currentFile.Close()
			p.currentFile = nil
		}
		if p.nextFile != nil {
			p.nextFile.Close()
			p.nextFile = nil
		}
		p.mu.Unlock()
	}
	atomic.StoreInt32(&p.paused, 0)
	atomic.StoreInt32(&p.stopped, 0)
}

func (p *Player) Pause() {
	atomic.StoreInt32(&p.paused, 1)
}

func (p *Player) Stop() {
	// Stop is not Pause: halt playback AND rewind the current track to its
	// start so a subsequent Play restarts it from the beginning. The actual
	// rewind (decoder seek + ring-buffer clear + position reset) is done by the
	// decoder loop when it sees stopReset, keeping all buffer mutation on one
	// goroutine.
	atomic.StoreInt32(&p.stopped, 1)
	atomic.StoreInt32(&p.stopReset, 1)
}

func (p *Player) Next() {
	atomic.StoreInt32(&p.nextTrack, 1)
}

func (p *Player) Previous() {
	atomic.StoreInt32(&p.prevTrack, 1)
}

// Goto jumps playback to the track at idx. Out-of-range requests are ignored by
// the decoder loop (loadTrack bounds-checks the index).
func (p *Player) Goto(idx int) {
	atomic.StoreInt32(&p.gotoTrack, int32(idx))
}

func (p *Player) AddTrack(path string) {
	p.addTrack <- path
}

func (p *Player) RemoveTrack(idx int) {
	p.removeTrack <- idx
}

// ClearTracks empties the playlist in one decoder-loop step and stops
// playback: far faster than removing tracks one at a time, and correct when
// nothing has been loaded yet (a bare playlist clear with no Play press).
func (p *Player) ClearTracks() {
	p.clearTracks <- struct{}{}
}

// MoveItems moves playlist items in the range [from, from+count) to start at
// index dst. The dst is the target index in the playlist *before* the items
// are removed from their original position. Returns an error if the indices
// are out of bounds.
func (p *Player) MoveItems(from, count, dst int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.playlist)
	if from < 0 || count <= 0 || from+count > n {
		return fmt.Errorf("invalid range: from=%d count=%d playlist=%d", from, count, n)
	}
	if dst < 0 || dst > n-count {
		return fmt.Errorf("invalid destination: dst=%d (valid 0..%d)", dst, n-count)
	}
	if dst == from {
		return nil // no-op
	}

	// Track where the currently playing track ends up
	cur := int(atomic.LoadInt32(&p.currentTrack))

	// Extract the items being moved
	items := make([]string, count)
	copy(items, p.playlist[from:from+count])

	// Build new playlist: remove the range, then insert at dst
	rest := make([]string, 0, n-count)
	rest = append(rest, p.playlist[:from]...)
	rest = append(rest, p.playlist[from+count:]...)

	newPlaylist := make([]string, 0, n)
	newPlaylist = append(newPlaylist, rest[:dst]...)
	newPlaylist = append(newPlaylist, items...)
	newPlaylist = append(newPlaylist, rest[dst:]...)
	p.playlist = newPlaylist

	// Remap recorded boundary track indices so the reported playback track
	// follows the reorder. Boundaries hold *playlist indices*, and reporting
	// reads them (see playbackTrack); without this the poll would keep returning
	// the moved track's old index and the UI's now-playing marker would not
	// follow. Build old→new by replaying the same rebuild on an index array.
	restIdx := make([]int, 0, n-count)
	for i := 0; i < from; i++ {
		restIdx = append(restIdx, i)
	}
	for i := from + count; i < n; i++ {
		restIdx = append(restIdx, i)
	}
	newOrder := make([]int, 0, n)
	newOrder = append(newOrder, restIdx[:dst]...)
	for i := from; i < from+count; i++ {
		newOrder = append(newOrder, i)
	}
	newOrder = append(newOrder, restIdx[dst:]...)
	remap := make([]int, n)
	for newPos, old := range newOrder {
		remap[old] = newPos
	}
	p.boundMu.Lock()
	for i := range p.boundaries {
		if t := p.boundaries[i].track; t >= 0 && t < n {
			p.boundaries[i].track = remap[t]
		}
	}
	p.boundMu.Unlock()

	// Update currentTrack index to follow the currently playing track
	if cur >= from && cur < from+count {
		// Current track is inside the moved range
		offset := cur - from
		atomic.StoreInt32(&p.currentTrack, int32(dst+offset))
	} else {
		// Current track is outside the moved range -- compute its new position
		newCur := cur
		if cur >= from+count {
			newCur -= count // items were removed before this position
		} else if cur >= from {
			// already handled above
		}
		if newCur >= dst {
			newCur += count // items were inserted before this position
		}
		atomic.StoreInt32(&p.currentTrack, int32(newCur))
	}

	return nil
}

// SetVolume sets the playback volume as a linear gain factor.
// 0.0 = silent, 1.0 = unity gain (default). Values above 1.0 amplify.
// The value is clamped to [0.0, 2.0].
// In passthrough mode this is a no-op (volume is always 1.0).
func (p *Player) SetVolume(v float64) {
	if p.passthrough {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 2 {
		v = 2
	}
	atomic.StoreUint32(&p.volume, math.Float32bits(float32(v)))
}

// Volume returns the current volume as a linear gain factor (0.0 - 2.0).
func (p *Player) Volume() float64 {
	return float64(math.Float32frombits(atomic.LoadUint32(&p.volume)))
}

// IsPassthrough returns true if the player is in bit-perfect passthrough mode.
func (p *Player) IsPassthrough() bool {
	return p.passthrough
}

// Seek seeks to the given position in seconds within the current track.
// The seek is performed asynchronously by the decoder thread for immediate,
// gap-free playback. The ring buffer is cleared so no stale audio is heard.
func (p *Player) Seek(seconds float64) {
	sampleRate := int(atomic.LoadInt32(&p.sampleRate))
	if sampleRate <= 0 {
		return
	}
	target := int64(seconds * float64(sampleRate))
	if target < 0 {
		target = 0
	}
	atomic.StoreInt64(&p.seekTarget, target)
}

// SeekRelative seeks forward or backward by the given number of seconds
// relative to the current playback position.
func (p *Player) SeekRelative(seconds float64) {
	currentPos := p.Position()
	newPos := currentPos + seconds
	if newPos < 0 {
		newPos = 0
	}
	p.Seek(newPos)
}

// Status methods
func (p *Player) IsPlaying() bool {
	return atomic.LoadInt32(&p.paused) == 0 && atomic.LoadInt32(&p.stopped) == 0
}

func (p *Player) IsPaused() bool {
	return atomic.LoadInt32(&p.paused) == 1
}

func (p *Player) IsStopped() bool {
	return atomic.LoadInt32(&p.stopped) == 1
}

func (p *Player) IsEOF() bool {
	return atomic.LoadInt32(&p.eof) == 1
}

// CurrentTrack returns the playlist index of the track currently being heard.
func (p *Player) CurrentTrack() int {
	return p.playbackTrack()
}

func (p *Player) CurrentFile() string {
	idx := p.playbackTrack()
	p.mu.RLock()
	defer p.mu.RUnlock()
	if idx >= 0 && idx < len(p.playlist) {
		return p.playlist[idx]
	}
	return ""
}

func (p *Player) Playlist() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	playlist := make([]string, len(p.playlist))
	copy(playlist, p.playlist)
	return playlist
}

// Position returns the current playback position in seconds within the
// current track.
func (p *Player) Position() float64 {
	sampleRate := int(atomic.LoadInt32(&p.sampleRate))
	channels := int(atomic.LoadInt32(&p.channels))
	if sampleRate <= 0 || channels <= 0 {
		return 0
	}
	b, consumed, ok := p.activeBoundary()
	if !ok {
		return 0
	}
	// Interleaved samples consumed since this track's first sample landed,
	// converted to per-channel and offset by where the track started (nonzero
	// after a seek).
	var perChan int64
	if consumed > b.writeOffset {
		perChan = int64(consumed-b.writeOffset) / int64(channels)
	}
	pos := b.startSample + perChan
	return float64(pos) / float64(sampleRate)
}

// TrackDuration returns the total duration of the current track in seconds,
// or -1 if unknown.
func (p *Player) TrackDuration() float64 {
	sampleRate := int(atomic.LoadInt32(&p.sampleRate))
	b, _, ok := p.activeBoundary()
	if !ok || b.duration <= 0 || sampleRate <= 0 {
		return -1
	}
	return float64(b.duration) / float64(sampleRate)
}

// CurrentTrackMetadata returns metadata for the current track
func (p *Player) CurrentTrackMetadata() (*TrackMetadata, error) {
	file := p.CurrentFile()
	if file == "" {
		return nil, fmt.Errorf("no current track")
	}
	return ExtractMetadata(file)
}

// PlaylistMetadata returns metadata for all tracks in the playlist
func (p *Player) PlaylistMetadata() []*TrackMetadata {
	playlist := p.Playlist()
	metadata := make([]*TrackMetadata, len(playlist))

	for i, file := range playlist {
		meta, err := ExtractMetadata(file)
		if err != nil {
			// If we can't extract metadata, create a minimal entry with the filename
			meta = &TrackMetadata{
				Title: file,
			}
		}
		metadata[i] = meta
	}

	return metadata
}

func (p *Player) Close() {
	close(p.stopDecode)
	<-p.decodeDone

	p.mu.Lock()
	if p.currentFile != nil {
		p.currentFile.Close()
	}
	if p.nextFile != nil {
		p.nextFile.Close()
	}
	p.mu.Unlock()

	if p.stream != nil {
		p.stream.Destroy()
	}
}
