package pipewire

import (
	"testing"
	"time"
)

func TestInit(t *testing.T) {
	err := Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Deinit()
}

func TestStreamCreation(t *testing.T) {
	err := Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Deinit()

	format := AudioFormat{
		SampleRate: 44100,
		Channels:   2,
	}

	callback := func(buffer []byte, frames int) int {
		// Fill with silence
		for i := range buffer {
			buffer[i] = 0
		}
		return frames
	}

	stream, err := NewStream("Test Stream", format, callback)
	if err != nil {
		t.Skipf("Could not create stream (PipeWire may not be available): %v", err)
	}
	defer stream.Destroy()

	if stream == nil {
		t.Fatal("Stream is nil")
	}
}

func TestStreamConnect(t *testing.T) {
	err := Init()
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	defer Deinit()

	format := AudioFormat{
		SampleRate: 44100,
		Channels:   2,
	}

	callback := func(buffer []byte, frames int) int {
		// Fill with silence
		for i := range buffer {
			buffer[i] = 0
		}
		return frames
	}

	stream, err := NewStream("Test Stream", format, callback)
	if err != nil {
		t.Skipf("Could not create stream: %v", err)
	}
	defer stream.Destroy()

	err = stream.Connect(format)
	if err != nil {
		t.Skipf("Could not connect stream (PipeWire daemon may not be running): %v", err)
	}

	// Let it run for a brief moment
	time.Sleep(100 * time.Millisecond)
}
