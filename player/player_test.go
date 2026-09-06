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
