package player

import (
	"testing"
	"time"
)

// Starting with an empty playlist must not require any audio file or PipeWire
// stream: the stream is created lazily on the first track load. This is what
// lets pwplay-server start with no arguments and be driven entirely via HTTP.
func TestNewPlayerEmptyPlaylist(t *testing.T) {
	p, err := NewPlayerWithOptions(nil, PlayerOptions{StartPaused: true})
	if err != nil {
		t.Fatalf("NewPlayerWithOptions(nil) failed: %v", err)
	}
	defer p.Close()

	if got := len(p.Playlist()); got != 0 {
		t.Errorf("Playlist length = %d, want 0", got)
	}
	if !p.IsPaused() {
		t.Error("IsPaused = false, want true (StartPaused)")
	}
	if p.IsPlaying() {
		t.Error("IsPlaying = true, want false")
	}
	if got := p.CurrentFile(); got != "" {
		t.Errorf("CurrentFile = %q, want empty", got)
	}
	if got := p.Position(); got != 0 {
		t.Errorf("Position = %v, want 0", got)
	}
	if got := p.TrackDuration(); got != -1 {
		t.Errorf("TrackDuration = %v, want -1", got)
	}
	if _, err := p.CurrentTrackMetadata(); err == nil {
		t.Error("CurrentTrackMetadata expected an error with no tracks")
	}

	// Play with an empty queue must not panic or hang; there is simply
	// nothing to load, so the player reports stopped.
	p.Play()
	deadline := time.Now().Add(2 * time.Second)
	for !p.IsStopped() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !p.IsStopped() {
		t.Error("IsStopped = false after Play on empty queue, want true")
	}

	// Tracks can be queued (and removed) without a stream ever existing.
	p.AddTrack("/nonexistent/track.flac")
	deadline = time.Now().Add(2 * time.Second)
	for len(p.Playlist()) != 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := len(p.Playlist()); got != 1 {
		t.Fatalf("Playlist length after AddTrack = %d, want 1", got)
	}

	p.RemoveTrack(0)
	deadline = time.Now().Add(2 * time.Second)
	for len(p.Playlist()) != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := len(p.Playlist()); got != 0 {
		t.Errorf("Playlist length after RemoveTrack = %d, want 0", got)
	}
}

// Seeking within the last track after the decoder has already read it to the
// end (it fits the ring, so the whole track is buffered at once) must resume
// decoding from the target. The decoder's end-of-queue stop used to stick:
// the seek cleared the ring and rewound the decoder, but stopped/eof stayed
// set, so nothing was decoded again and playback sat silent at the target.
func TestSeekAfterLastTrackFullyBuffered(t *testing.T) {
	dir := t.TempDir()
	const rate = 4000
	a := writeWAV(t, dir, "a.wav", rate, 1, rate) // 1 s: fits the 3 s ring

	sink := &fakeSink{}
	p, err := NewPlayerWithOptions([]string{a}, PlayerOptions{Sink: fakeSinkFactory(sink)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	waitFor(t, 2*time.Second, "decoder at EOF", p.IsEOF)
	if got := p.ringBuffer.Available(); got != rate {
		t.Fatalf("buffered %d samples, want the whole track (%d)", got, rate)
	}
	sink.pump(rate / 10) // 0.1 s heard

	p.Seek(0.5)
	waitFor(t, 2*time.Second, "seek", func() bool { return p.ringBuffer.Available() == rate/2 })
	if p.IsEOF() && p.ringBuffer.Available() == 0 {
		t.Fatal("seek left the player stuck at EOF")
	}
	if got := p.Position(); got < 0.49 || got > 0.51 {
		t.Errorf("Position after seek = %.3f, want 0.5", got)
	}
	// The rest of the track plays out, then the queue ends as usual.
	sink.pump(rate / 2)
	if got := p.Position(); got < 0.99 {
		t.Errorf("Position after playing the remainder = %.3f, want 1.0", got)
	}
	waitFor(t, 2*time.Second, "end of queue", func() bool { return p.IsEOF() && !p.IsPlaying() })

	// Seek again once the queue has fully drained: playback resumes.
	p.Seek(0.25)
	waitFor(t, 2*time.Second, "seek after drain", func() bool { return p.ringBuffer.Available() == rate*3/4 })
	sink.pump(rate / 4)
	if got := p.Position(); got < 0.49 || got > 0.51 {
		t.Errorf("Position after seeking a finished queue and playing 0.25 s = %.3f, want 0.5", got)
	}
}

// A seek while stopped by the user (Stop) must not start playback.
func TestSeekWhileStoppedStaysStopped(t *testing.T) {
	dir := t.TempDir()
	const rate = 4000
	a := writeWAV(t, dir, "a.wav", rate, 1, rate)

	sink := &fakeSink{}
	p, err := NewPlayerWithOptions([]string{a}, PlayerOptions{Sink: fakeSinkFactory(sink)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	waitFor(t, 2*time.Second, "decoder at EOF", p.IsEOF)
	p.Stop()
	waitFor(t, 2*time.Second, "stop rewind", func() bool { return p.ringBuffer.Available() == 0 })
	p.Seek(0.5)
	time.Sleep(50 * time.Millisecond)
	if p.IsPlaying() {
		t.Error("seek after Stop started playback")
	}
}
