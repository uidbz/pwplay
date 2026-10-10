package player

import (
	"sync"
	"testing"
	"time"
	"unsafe"
)

// fakeSink records the format it was connected with and lets the test drive
// the audio callback manually, so the engine can be tested without a sound
// device.
//
// The factory may be called repeatedly (the player reopens the sink at each
// format change); every Connect is recorded in formats, and onConnect (if
// set) runs inside Connect on the player's decoder goroutine.
type fakeSink struct {
	mu        sync.Mutex
	cb        ProcessCallback
	format    Format
	connects  int
	destroys  int
	formats   []Format
	onConnect func(Format)
}

func (s *fakeSink) Connect(format Format) error {
	s.mu.Lock()
	s.format = format
	s.connects++
	s.formats = append(s.formats, format)
	hook := s.onConnect
	s.mu.Unlock()
	if hook != nil {
		hook(format)
	}
	return nil
}

func (s *fakeSink) Destroy() {
	s.mu.Lock()
	s.destroys++
	s.cb = nil
	s.mu.Unlock()
}

// connected returns the number of Connect calls so far.
func (s *fakeSink) connected() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connects
}

// current returns the format of the most recent Connect.
func (s *fakeSink) current() Format {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.format
}

// pump feeds nFrames of audio through the callback, simulating the sound
// card's clock.
func (s *fakeSink) pump(frames int) {
	s.pumpSamples(frames)
}

// pumpSamples is pump returning the interleaved samples the callback wrote.
func (s *fakeSink) pumpSamples(frames int) []float32 {
	s.mu.Lock()
	cb := s.cb
	format := s.format
	s.mu.Unlock()
	if cb == nil || format.Channels <= 0 {
		return nil
	}
	n := frames * format.Channels
	out := make([]float32, n)
	cb(unsafe.Slice((*byte)(unsafe.Pointer(&out[0])), n*4), frames)
	return out
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func fakeSinkFactory(s *fakeSink) SinkFactory {
	return func(name string, format Format, cb ProcessCallback, opts SinkOptions) (Sink, error) {
		s.mu.Lock()
		s.cb = cb
		s.mu.Unlock()
		return s, nil
	}
}

// A custom SinkFactory replaces the platform default: the player must create
// the sink lazily on the first track load, at the track's native format, and
// must report playback progress driven by the sink's callback clock.
func TestPlayerWithFakeSink(t *testing.T) {
	sink := &fakeSink{}
	p, err := NewPlayerWithOptions(nil, PlayerOptions{
		StartPaused: true,
		Sink:        fakeSinkFactory(sink),
	})
	if err != nil {
		t.Fatalf("NewPlayerWithOptions failed: %v", err)
	}
	defer p.Close()

	p.AddTrack("testdata/sine_stereo.opus")
	deadline := time.Now().Add(2 * time.Second)
	for len(p.Playlist()) != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := len(p.Playlist()); got != 1 {
		t.Fatalf("Playlist length = %d, want 1", got)
	}
	if n := sink.connected(); n != 0 {
		t.Fatalf("sink connected %d times before first Play, want 0 (lazy creation)", n)
	}

	p.Play()
	waitFor(t, 2*time.Second, "sink connect", func() bool { return sink.connected() > 0 })
	sink.mu.Lock()
	if sink.connects != 1 {
		sink.mu.Unlock()
		t.Fatalf("sink connects = %d, want 1", sink.connects)
	}
	format := sink.format
	sink.mu.Unlock()
	// Opus decodes at its native 48 kHz.
	if format.SampleRate != 48000 || format.Channels != 2 {
		t.Errorf("sink format = %+v, want {48000 2}", format)
	}

	// Pump ~0.5 s of audio through the callback; the reported position must
	// follow the consumed samples.
	for i := 0; i < 10; i++ {
		// Wait for the decoder to buffer the chunk first, so the pump never
		// underruns on a slow (e.g. -race) run.
		waitFor(t, 2*time.Second, "buffered audio", func() bool {
			return p.ringBuffer.Available() >= 48000/20*2
		})
		sink.pump(48000 / 20)
	}
	if pos := p.Position(); pos < 0.4 || pos > 0.6 {
		t.Errorf("Position = %v, want ~0.5", pos)
	}
	if p.TrackDuration() <= 0 {
		t.Errorf("TrackDuration = %v, want > 0", p.TrackDuration())
	}
	if got := p.CurrentTrack(); got != 0 {
		t.Errorf("CurrentTrack = %d, want 0", got)
	}
}

// After ClearTracks the reported track must not come from a stale boundary
// recorded for the old playlist.
func TestClearTracksDropsBoundaries(t *testing.T) {
	sink := &fakeSink{}
	p, err := NewPlayerWithOptions(nil, PlayerOptions{Sink: fakeSinkFactory(sink)})
	if err != nil {
		t.Fatalf("NewPlayerWithOptions failed: %v", err)
	}
	defer p.Close()

	p.AddTrack("testdata/sine_stereo.opus")
	p.AddTrack("testdata/sine_mono.opus")
	deadline := time.Now().Add(2 * time.Second)
	for len(p.Playlist()) != 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	p.Play()
	waitFor(t, 2*time.Second, "sink connect", func() bool { return sink.connected() > 0 })
	sink.pump(48000 / 10)

	p.ClearTracks()
	deadline = time.Now().Add(2 * time.Second)
	for len(p.Playlist()) != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := p.CurrentTrack(); got != 0 {
		t.Errorf("CurrentTrack after ClearTracks = %d, want 0 (no stale boundary)", got)
	}
}
