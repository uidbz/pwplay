package player

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// writeWAV writes a 16-bit PCM WAV with frames frames at rate/channels.
// Sample i (interleaved index) has the value sampleValue(i), so tests can
// check that audio arrives complete and in order.
func writeWAV(t *testing.T, dir, name string, rate, channels, frames int) string {
	t.Helper()
	n := frames * channels
	data := make([]byte, n*2)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sampleValue(i)))
	}
	hdr := make([]byte, 44)
	copy(hdr[0:], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(36+len(data)))
	copy(hdr[8:], "WAVE")
	copy(hdr[12:], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1) // PCM
	binary.LittleEndian.PutUint16(hdr[22:], uint16(channels))
	binary.LittleEndian.PutUint32(hdr[24:], uint32(rate))
	binary.LittleEndian.PutUint32(hdr[28:], uint32(rate*channels*2))
	binary.LittleEndian.PutUint16(hdr[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(data)))

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, append(hdr, data...), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// sampleValue is the int16 stored at interleaved index i: a ramp that never
// repeats within a test file and is never zero (silence would be ambiguous
// with the callback's underrun fill).
func sampleValue(i int) int16 { return int16(i%30000 + 1) }

func newFakePlayer(t *testing.T, sink *fakeSink, files ...string) *Player {
	t.Helper()
	p, err := NewPlayerWithOptions(nil, PlayerOptions{StartPaused: true, Sink: fakeSinkFactory(sink)})
	if err != nil {
		t.Fatalf("NewPlayerWithOptions: %v", err)
	}
	for _, f := range files {
		p.AddTrack(f)
	}
	waitFor(t, 2*time.Second, "playlist", func() bool { return len(p.Playlist()) == len(files) })
	return p
}

// A gapless advance into a track with a different sample rate and channel
// count must reopen the sink at the new format — but only after the previous
// track's buffered tail has been consumed, so none of it is played at the new
// format's speed.
func TestFormatChangeReopensSinkAfterTail(t *testing.T) {
	dir := t.TempDir()
	const frames0 = 4410 // 0.1 s
	a := writeWAV(t, dir, "a.wav", 44100, 2, frames0)
	b := writeWAV(t, dir, "b.wav", 48000, 1, 4800)

	sink := &fakeSink{}
	var p *Player
	var consumedAtReopen atomic.Uint64
	sink.onConnect = func(f Format) {
		if f.SampleRate == 48000 {
			consumedAtReopen.Store(p.ringBuffer.ReadPos())
		}
	}
	p = newFakePlayer(t, sink, a, b)
	defer p.Close()

	p.Play()
	waitFor(t, 2*time.Second, "first sink", func() bool { return sink.connected() == 1 })
	if got := sink.current(); got != (Format{44100, 2}) {
		t.Fatalf("first sink format = %+v, want {44100 2}", got)
	}

	// Play the first track out in small periods.
	for sink.connected() == 1 {
		sink.pump(441)
		time.Sleep(time.Millisecond)
	}
	if got := sink.current(); got != (Format{48000, 1}) {
		t.Fatalf("second sink format = %+v, want {48000 1}", got)
	}
	if got, want := consumedAtReopen.Load(), uint64(frames0*2); got != want {
		t.Errorf("ring read position at reopen = %d, want %d (the whole first track)", got, want)
	}
	sink.mu.Lock()
	destroys := sink.destroys
	sink.mu.Unlock()
	if destroys != 1 {
		t.Errorf("sink destroyed %d times, want 1", destroys)
	}

	// Reporting uses the new track's own rate.
	waitFor(t, 2*time.Second, "buffered audio", func() bool { return p.ringBuffer.Available() >= 2400 })
	sink.pump(2400)
	if got := p.CurrentTrack(); got != 1 {
		t.Errorf("CurrentTrack = %d, want 1", got)
	}
	if pos := p.Position(); math.Abs(pos-0.05) > 0.001 {
		t.Errorf("Position = %v, want 0.05 (2400 frames at 48 kHz)", pos)
	}
	if d := p.TrackDuration(); math.Abs(d-0.1) > 0.001 {
		t.Errorf("TrackDuration = %v, want 0.1", d)
	}
}

// An explicit jump discards queued audio, so a format change applies at once.
func TestGotoSwitchesFormatImmediately(t *testing.T) {
	dir := t.TempDir()
	a := writeWAV(t, dir, "a.wav", 44100, 2, 44100)
	b := writeWAV(t, dir, "b.wav", 96000, 2, 9600)

	sink := &fakeSink{}
	p := newFakePlayer(t, sink, a, b)
	defer p.Close()

	p.Play()
	waitFor(t, 2*time.Second, "first sink", func() bool { return sink.connected() == 1 })
	p.Goto(1)
	waitFor(t, 2*time.Second, "reopened sink", func() bool { return sink.connected() == 2 })
	if got := sink.current(); got != (Format{96000, 2}) {
		t.Errorf("sink format after Goto = %+v, want {96000 2}", got)
	}
}

// fillAndPause starts playback, waits for the ring to fill, then pauses —
// the state in which the old decoder loop spun forever on a full ring.
func fillAndPause(t *testing.T, p *Player, sink *fakeSink) {
	t.Helper()
	p.Play()
	waitFor(t, 2*time.Second, "sink", func() bool { return sink.connected() > 0 })
	waitFor(t, 5*time.Second, "full ring", func() bool {
		return p.ringBuffer.Available() == p.ringBuffer.size
	})
	p.Pause()
	time.Sleep(20 * time.Millisecond)
}

// Close must return promptly while paused with a full ring (tie-audio hung on
// window close: Close waited for a decoder stuck retrying a full-ring write).
func TestCloseWhilePausedWithFullRing(t *testing.T) {
	dir := t.TempDir()
	a := writeWAV(t, dir, "a.wav", 8000, 1, 8000*10) // longer than the 3 s ring

	sink := &fakeSink{}
	p := newFakePlayer(t, sink, a)
	fillAndPause(t, p, sink)

	done := make(chan struct{})
	go func() { p.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close did not return while paused with a full ring")
	}
}

// Queue commands must be served while paused with a full ring: AddTrack sends
// on a small buffered channel and blocked once it filled; Goto/Stop did not
// take effect until playback resumed.
func TestCommandsWhilePausedWithFullRing(t *testing.T) {
	dir := t.TempDir()
	a := writeWAV(t, dir, "a.wav", 8000, 1, 8000*10)
	b := writeWAV(t, dir, "b.wav", 16000, 1, 16000)

	sink := &fakeSink{}
	p := newFakePlayer(t, sink, a, b)
	defer p.Close()
	fillAndPause(t, p, sink)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			p.AddTrack(b)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("AddTrack blocked while paused")
	}
	waitFor(t, 2*time.Second, "52 tracks", func() bool { return len(p.Playlist()) == 52 })

	p.Goto(1)
	waitFor(t, 2*time.Second, "goto while paused", func() bool { return p.CurrentTrack() == 1 })

	p.Stop()
	waitFor(t, 2*time.Second, "stop while paused", func() bool { return p.Position() == 0 && p.IsStopped() })
}

// Decoded samples left over when the ring is full are written on a later
// pass, so a pause/resume (and ordinary back-pressure) neither drops nor
// duplicates audio.
func TestNoSampleLossAcrossPause(t *testing.T) {
	dir := t.TempDir()
	const frames = 8000 * 5 // 5 s mono: more than the 3 s ring
	a := writeWAV(t, dir, "a.wav", 8000, 1, frames)

	sink := &fakeSink{}
	p := newFakePlayer(t, sink, a)
	defer p.Close()
	fillAndPause(t, p, sink)
	time.Sleep(30 * time.Millisecond)
	p.Play()

	var got []float32
	deadline := time.Now().Add(10 * time.Second)
	for len(got) < frames && time.Now().Before(deadline) {
		if p.ringBuffer.Available() == 0 {
			time.Sleep(time.Millisecond)
			continue
		}
		out := sink.pumpSamples(500)
		for _, v := range out {
			if v != 0 { // 0 = underrun fill; the ramp has no zeros
				got = append(got, v)
			}
		}
	}
	if len(got) != frames {
		t.Fatalf("received %d samples, want %d", len(got), frames)
	}
	for i, v := range got {
		if want := float32(sampleValue(i)) / 32768; v != want {
			t.Fatalf("sample %d = %v, want %v (dropped or duplicated audio)", i, v, want)
		}
	}
}

// WAV decoding starts at the first PCM sample (not the data chunk header),
// seeks are relative to the PCM data, and reads stop at the chunk's end even
// when metadata chunks follow it.
func TestWAVDecoderDataChunk(t *testing.T) {
	dir := t.TempDir()
	path := writeWAV(t, dir, "a.wav", 8000, 2, 1000)
	// Append a trailing chunk that must not be decoded as audio.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("LIST\x04\x00\x00\x00junk")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	d, err := OpenAudioFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.Duration() != 1000 {
		t.Errorf("Duration = %d, want 1000", d.Duration())
	}
	buf := make([]float32, 4)
	if n, _ := d.ReadSamples(buf); n != 4 || buf[0] != float32(sampleValue(0))/32768 {
		t.Fatalf("first samples = %v (n=%d), want the ramp start", buf, n)
	}
	if err := d.Seek(100); err != nil {
		t.Fatal(err)
	}
	if n, _ := d.ReadSamples(buf); n != 4 || buf[0] != float32(sampleValue(200))/32768 {
		t.Fatalf("after Seek(100): %v, want sample %d", buf, 200)
	}
	total := 4 + 200
	big := make([]float32, 4096)
	for {
		n, err := d.ReadSamples(big)
		total += n
		if err != nil || n == 0 {
			break
		}
	}
	if total != 2000 {
		t.Errorf("decoded %d samples in total, want 2000 (no trailing chunk)", total)
	}
}
