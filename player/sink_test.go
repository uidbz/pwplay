package player

import (
	"sync"
	"testing"
	"time"
)

// fakeSink records the format it was connected with and lets the test drive
// the audio callback manually, so the engine can be tested without a sound
// device.
type fakeSink struct {
	mu       sync.Mutex
	cb       ProcessCallback
	format   Format
	connects int
}

func (s *fakeSink) Connect(format Format) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.format = format
	s.connects++
	return nil
}

func (s *fakeSink) Destroy() {}

// pump feeds nFrames of audio through the callback, simulating the sound
// card's clock.
func (s *fakeSink) pump(frames int) {
	s.mu.Lock()
	cb := s.cb
	format := s.format
	s.mu.Unlock()
	if cb == nil {
		return
	}
	buf := make([]byte, frames*format.Channels*4)
	cb(buf, frames)
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
	if sink.connects != 0 {
		t.Fatalf("sink connected %d times before first Play, want 0 (lazy creation)", sink.connects)
	}

	p.Play()
	deadline = time.Now().Add(2 * time.Second)
	for sink.connects == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
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
		sink.pump(48000 / 20)
		time.Sleep(5 * time.Millisecond)
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
	deadline = time.Now().Add(2 * time.Second)
	for sink.connects == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
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
