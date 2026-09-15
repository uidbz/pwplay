package player

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// readAll drains the decoder and returns the total per-channel samples read
// and the sum of absolute sample values (to distinguish audio from silence).
func readAll(t *testing.T, d AudioDecoder) (int64, float64) {
	t.Helper()
	buf := make([]float32, 4096)
	var total int64
	var absSum float64
	for {
		n, err := d.ReadSamples(buf)
		for i := 0; i < n; i++ {
			if buf[i] < 0 {
				absSum -= float64(buf[i])
			} else {
				absSum += float64(buf[i])
			}
		}
		total += int64(n / d.Channels())
		if err == io.EOF {
			return total, absSum
		}
		if err != nil {
			t.Fatalf("ReadSamples failed: %v", err)
		}
		if n == 0 {
			t.Fatal("ReadSamples made no progress without returning EOF")
		}
	}
}

func TestOpenOpusFile(t *testing.T) {
	d, err := OpenAudioFile("testdata/sine_stereo.opus")
	if err != nil {
		t.Fatalf("OpenAudioFile failed: %v", err)
	}
	defer d.Close()

	if _, ok := d.(*OPUSDecoder); !ok {
		t.Errorf("decoder type = %T, want *OPUSDecoder", d)
	}
	if got := d.SampleRate(); got != 48000 {
		t.Errorf("SampleRate = %d, want 48000 (Opus native rate)", got)
	}
	if got := d.Channels(); got != 2 {
		t.Errorf("Channels = %d, want 2", got)
	}
	if got := d.BitsPerSample(); got != 16 {
		t.Errorf("BitsPerSample = %d, want 16", got)
	}
	// 1 second of audio at 48 kHz; allow encoder/trim tolerance.
	if got := d.Duration(); got < 47000 || got > 49000 {
		t.Errorf("Duration = %d, want ~48000", got)
	}
	if got := d.Position(); got != 0 {
		t.Errorf("Position = %d, want 0", got)
	}
}

func TestOpusReadToEOF(t *testing.T) {
	d, err := OpenAudioFile("testdata/sine_stereo.opus")
	if err != nil {
		t.Fatalf("OpenAudioFile failed: %v", err)
	}
	defer d.Close()

	total, absSum := readAll(t, d)

	// The stream must end-trim to the granule-indicated duration, not emit
	// the padding the last packets decode to.
	if dur := d.Duration(); dur > 0 && total != dur {
		t.Errorf("total samples read = %d, want duration %d", total, dur)
	}
	if absSum <= 0 {
		t.Error("decoded audio is silent, want a sine wave")
	}
	if got := d.Position(); got != total {
		t.Errorf("Position = %d, want %d after full read", got, total)
	}
}

func TestOpusSeek(t *testing.T) {
	d, err := OpenAudioFile("testdata/sine_stereo.opus")
	if err != nil {
		t.Fatalf("OpenAudioFile failed: %v", err)
	}
	defer d.Close()

	dur := d.Duration()
	if dur <= 0 {
		t.Fatalf("Duration = %d, want > 0 for seek test", dur)
	}

	// Read a little, then seek forward.
	buf := make([]float32, 4096)
	if _, err := d.ReadSamples(buf); err != nil {
		t.Fatalf("ReadSamples failed: %v", err)
	}

	mid := dur / 2
	if err := d.Seek(mid); err != nil {
		t.Fatalf("Seek(%d) failed: %v", mid, err)
	}
	if got := d.Position(); got != mid {
		t.Errorf("Position after forward seek = %d, want %d", got, mid)
	}
	// Audio must still flow (and not be silent) after the seek.
	n, err := d.ReadSamples(buf)
	if err != nil {
		t.Fatalf("ReadSamples after seek failed: %v", err)
	}
	silent := true
	for i := 0; i < n; i++ {
		if buf[i] != 0 {
			silent = false
			break
		}
	}
	if silent {
		t.Error("decoded audio after seek is silent")
	}

	// Seek backwards to the start (exercises the rewind path).
	if err := d.Seek(0); err != nil {
		t.Fatalf("Seek(0) failed: %v", err)
	}
	if got := d.Position(); got != 0 {
		t.Errorf("Position after seek to 0 = %d, want 0", got)
	}

	// After rewinding, the whole stream is readable again up to duration.
	total, absSum := readAll(t, d)
	if total != dur {
		t.Errorf("total samples after rewind = %d, want %d", total, dur)
	}
	if absSum <= 0 {
		t.Error("decoded audio after rewind is silent")
	}
}

func TestOpusMono(t *testing.T) {
	d, err := OpenAudioFile("testdata/sine_mono.opus")
	if err != nil {
		t.Fatalf("OpenAudioFile failed: %v", err)
	}
	defer d.Close()

	if got := d.Channels(); got != 1 {
		t.Errorf("Channels = %d, want 1", got)
	}
	if got := d.SampleRate(); got != 48000 {
		t.Errorf("SampleRate = %d, want 48000", got)
	}
	total, absSum := readAll(t, d)
	if total < 47000 || total > 49000 {
		t.Errorf("total samples = %d, want ~48000", total)
	}
	if absSum <= 0 {
		t.Error("decoded mono audio is silent")
	}
}

// The Ogg container carries both Vorbis and Opus; files with a .ogg
// extension must be routed by content sniffing, not by extension alone.
func TestOggExtensionSniffing(t *testing.T) {
	tmpDir := t.TempDir()

	// Opus content in a .ogg file must decode with the Opus decoder.
	opusAsOgg := filepath.Join(tmpDir, "opus_as_ogg.ogg")
	data, err := os.ReadFile("testdata/sine_stereo.opus")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(opusAsOgg, data, 0644); err != nil {
		t.Fatal(err)
	}
	d, err := OpenAudioFile(opusAsOgg)
	if err != nil {
		t.Fatalf("OpenAudioFile(%s) failed: %v", opusAsOgg, err)
	}
	if _, ok := d.(*OPUSDecoder); !ok {
		t.Errorf("Opus-in-.ogg decoder type = %T, want *OPUSDecoder", d)
	}
	d.Close()

	// Vorbis content in a .ogg file must keep using the Vorbis decoder.
	d, err = OpenAudioFile("testdata/sine_vorbis.ogg")
	if err != nil {
		t.Fatalf("OpenAudioFile(vorbis) failed: %v", err)
	}
	if _, ok := d.(*OGGDecoder); !ok {
		t.Errorf("Vorbis-in-.ogg decoder type = %T, want *OGGDecoder", d)
	}
	d.Close()
}

func TestOpusOverHTTP(t *testing.T) {
	data, err := os.ReadFile("testdata/sine_stereo.opus")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	// Extension-based detection.
	mux.HandleFunc("/track.opus", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(data)
	})
	// Content-type-based detection (no usable extension).
	mux.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/opus")
		w.Write(data)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, path := range []string{"/track.opus", "/stream"} {
		d, err := OpenAudioFile(srv.URL + path)
		if err != nil {
			t.Fatalf("OpenAudioFile(%s) failed: %v", path, err)
		}
		if got := d.SampleRate(); got != 48000 {
			t.Errorf("%s: SampleRate = %d, want 48000", path, got)
		}
		total, absSum := readAll(t, d)
		if total < 47000 || total > 49000 {
			t.Errorf("%s: total samples = %d, want ~48000", path, total)
		}
		if absSum <= 0 {
			t.Errorf("%s: decoded audio is silent", path)
		}
		d.Close()
	}
}
