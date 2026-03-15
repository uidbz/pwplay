package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unsafe"

	"git.sr.ht/~uid/pwplay/pipewire"
)

// ToneGenerator generates a simple sine wave tone
type ToneGenerator struct {
	stream     *pipewire.Stream
	frequency  float64
	sampleRate int
	channels   int
	phase      float64
}

func NewToneGenerator(freq float64, sampleRate, channels int) (*ToneGenerator, error) {
	gen := &ToneGenerator{
		frequency:  freq,
		sampleRate: sampleRate,
		channels:   channels,
		phase:      0,
	}

	format := pipewire.AudioFormat{
		SampleRate: sampleRate,
		Channels:   channels,
	}

	stream, err := pipewire.NewStream("Tone Generator", format, gen.processCallback)
	if err != nil {
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	gen.stream = stream

	if err := stream.Connect(format); err != nil {
		return nil, fmt.Errorf("failed to connect stream: %w", err)
	}

	return gen, nil
}

func (g *ToneGenerator) processCallback(buffer []byte, frames int) int {
	// Convert buffer to float32 slice
	samplesTotal := frames * g.channels
	output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])), samplesTotal)

	phaseIncrement := 2.0 * math.Pi * g.frequency / float64(g.sampleRate)

	for i := 0; i < frames; i++ {
		// Generate sine wave sample
		sample := float32(math.Sin(g.phase))

		// Write to all channels
		for ch := 0; ch < g.channels; ch++ {
			output[i*g.channels+ch] = sample * 0.3 // Reduce volume to 30%
		}

		g.phase += phaseIncrement
		if g.phase >= 2.0*math.Pi {
			g.phase -= 2.0 * math.Pi
		}
	}

	return frames
}

func (g *ToneGenerator) Close() {
	if g.stream != nil {
		g.stream.Destroy()
		g.stream = nil
	}
}

func main() {
	// Default frequency is A4 (440 Hz)
	frequency := 440.0

	if len(os.Args) > 1 {
		if _, err := fmt.Sscanf(os.Args[1], "%f", &frequency); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid frequency: %s\n", os.Args[1])
			os.Exit(1)
		}
	}

	// Initialize PipeWire
	if err := pipewire.Init(); err != nil {
		log.Fatalf("Failed to initialize PipeWire: %v", err)
	}
	defer pipewire.Deinit()

	log.Printf("Generating %g Hz tone at 48kHz stereo", frequency)

	// Create tone generator
	gen, err := NewToneGenerator(frequency, 48000, 2)
	if err != nil {
		log.Fatalf("Failed to create tone generator: %v", err)
	}
	defer gen.Close()

	log.Println("Playing... Press Ctrl+C to stop")

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for interrupt
	<-sigChan
	log.Println("\nStopping...")
	time.Sleep(100 * time.Millisecond) // Let the audio drain
}
