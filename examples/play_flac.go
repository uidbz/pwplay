package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"git.sr.ht/~uid/pwplay/pipewire"
	"github.com/mewkiz/flac"
)

// RingBuffer is a simple lock-free ring buffer for audio samples
type RingBuffer struct {
	buffer []float32
	size   int
	read   int
	write  int
	mu     sync.Mutex
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buffer: make([]float32, size),
		size:   size,
	}
}

func (rb *RingBuffer) Write(samples []float32) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	written := 0
	for _, sample := range samples {
		nextWrite := (rb.write + 1) % rb.size
		if nextWrite == rb.read {
			// Buffer full
			break
		}
		rb.buffer[rb.write] = sample
		rb.write = nextWrite
		written++
	}
	return written
}

func (rb *RingBuffer) Read(samples []float32) int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	read := 0
	for i := range samples {
		if rb.read == rb.write {
			// Buffer empty
			break
		}
		samples[i] = rb.buffer[rb.read]
		rb.read = (rb.read + 1) % rb.size
		read++
	}
	return read
}

func (rb *RingBuffer) Available() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.write >= rb.read {
		return rb.write - rb.read
	}
	return rb.size - rb.read + rb.write
}

func (rb *RingBuffer) Space() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	space := rb.read - rb.write - 1
	if space < 0 {
		space += rb.size
	}
	return space
}

type FlacPlayer struct {
	stream     *pipewire.Stream
	decoder    *flac.Stream
	ringBuffer *RingBuffer
	eof        bool
	channels   int
	stopDecode chan bool
	decodeDone chan bool
	mu         sync.Mutex
}

func NewFlacPlayer(filename string) (*FlacPlayer, error) {
	// Open FLAC file
	stream, err := flac.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open FLAC file: %w", err)
	}

	info := stream.Info
	log.Printf("FLAC info: %d Hz, %d channels, %d bits per sample, %d samples",
		info.SampleRate, info.NChannels, info.BitsPerSample, info.NSamples)

	// Create a large ring buffer (2 seconds of audio)
	bufferSize := int(info.SampleRate) * int(info.NChannels) * 2
	ringBuffer := NewRingBuffer(bufferSize)

	player := &FlacPlayer{
		decoder:    stream,
		ringBuffer: ringBuffer,
		channels:   int(info.NChannels),
		stopDecode: make(chan bool),
		decodeDone: make(chan bool),
	}

	// Create PipeWire stream
	format := pipewire.AudioFormat{
		SampleRate: int(info.SampleRate),
		Channels:   int(info.NChannels),
	}

	pwStream, err := pipewire.NewStream("FLAC Player", format, player.processCallback)
	if err != nil {
		stream.Close()
		return nil, fmt.Errorf("failed to create PipeWire stream: %w", err)
	}

	player.stream = pwStream

	// Start decoder thread
	go player.decoderThread()

	// Wait for buffer to fill a bit before starting playback
	log.Println("Buffering...")
	for player.ringBuffer.Available() < bufferSize/4 && !player.IsEOF() {
		time.Sleep(10 * time.Millisecond)
	}

	// Connect to PipeWire
	if err := pwStream.Connect(format); err != nil {
		close(player.stopDecode)
		stream.Close()
		return nil, fmt.Errorf("failed to connect stream: %w", err)
	}

	return player, nil
}

// decoderThread runs in a separate goroutine to decode FLAC frames
func (p *FlacPlayer) decoderThread() {
	defer close(p.decodeDone)

	bitsPerSample := p.decoder.Info.BitsPerSample
	divisor := float32(int32(1) << (bitsPerSample - 1))

	for {
		select {
		case <-p.stopDecode:
			return
		default:
		}

		// Check if buffer has space
		if p.ringBuffer.Space() < 4096*p.channels {
			time.Sleep(5 * time.Millisecond)
			continue
		}

		// Decode next frame
		frame, err := p.decoder.ParseNext()
		if err != nil {
			if err == io.EOF {
				p.mu.Lock()
				p.eof = true
				p.mu.Unlock()
				return
			}
			log.Printf("Error reading frame: %v", err)
			p.mu.Lock()
			p.eof = true
			p.mu.Unlock()
			return
		}

		// Convert frame samples to float32 and write to ring buffer
		samples := make([]float32, 0, frame.Subframes[0].NSamples*p.channels)
		for i := 0; i < frame.Subframes[0].NSamples; i++ {
			for ch := 0; ch < p.channels; ch++ {
				sample := frame.Subframes[ch].Samples[i]
				normalized := float32(sample) / divisor
				samples = append(samples, normalized)
			}
		}

		// Write to ring buffer
		written := 0
		for written < len(samples) {
			n := p.ringBuffer.Write(samples[written:])
			if n == 0 {
				// Buffer full, wait a bit
				time.Sleep(1 * time.Millisecond)
			}
			written += n
		}
	}
}

// processCallback is called by PipeWire when it needs audio data
// This must be fast and lock-free to avoid dropouts
func (p *FlacPlayer) processCallback(buffer []byte, frames int) int {
	if p.IsEOF() && p.ringBuffer.Available() == 0 {
		// Fill with silence
		for i := range buffer {
			buffer[i] = 0
		}
		return 0
	}

	// Convert buffer to float32 slice
	samplesNeeded := frames * p.channels
	output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])), samplesNeeded)

	// Read from ring buffer
	read := p.ringBuffer.Read(output)

	// Fill remaining with silence if buffer underrun
	if read < samplesNeeded {
		for i := read; i < samplesNeeded; i++ {
			output[i] = 0
		}
		// Log underrun only if it's not at EOF (unexpected underrun)
		if read > 0 && !p.IsEOF() {
			log.Printf("Warning: Buffer underrun - got %d samples, needed %d", read, samplesNeeded)
		}
	}

	return frames
}

func (p *FlacPlayer) Close() {
	// Stop decoder thread
	close(p.stopDecode)
	<-p.decodeDone

	if p.stream != nil {
		p.stream.Destroy()
		p.stream = nil
	}

	if p.decoder != nil {
		p.decoder.Close()
		p.decoder = nil
	}
}

func (p *FlacPlayer) IsEOF() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.eof
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <flac-file>\n", os.Args[0])
		os.Exit(1)
	}

	filename := os.Args[1]

	// Initialize PipeWire
	if err := pipewire.Init(); err != nil {
		log.Fatalf("Failed to initialize PipeWire: %v", err)
	}
	defer pipewire.Deinit()

	log.Printf("Opening FLAC file: %s", filename)

	// Create player
	player, err := NewFlacPlayer(filename)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}
	defer player.Close()

	log.Println("Playing... Press Ctrl+C to stop")

	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for playback to finish or user interrupt
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			log.Println("\nInterrupted, stopping playback...")
			return
		case <-ticker.C:
			if player.IsEOF() && player.ringBuffer.Available() == 0 {
				log.Println("Playback finished")
				time.Sleep(200 * time.Millisecond) // Let the buffer drain
				return
			}
		}
	}
}
