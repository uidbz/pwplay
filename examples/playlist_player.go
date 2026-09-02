package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/uidbz/pwplay/pipewire"
	"github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
)

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

type PlaylistPlayer struct {
	stream       *pipewire.Stream
	ringBuffer   *RingBuffer
	playlist     []string
	currentTrack int
	currentFile  *flac.Stream
	nextFile     *flac.Stream // Preloaded next track
	eof          bool
	stopDecode   chan bool
	decodeDone   chan bool
	sampleRate   int
	channels     int
	mu           sync.Mutex
}

func NewPlaylistPlayer(files []string) (*PlaylistPlayer, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("empty playlist")
	}

	// Open first file to get format
	firstFile, err := flac.Open(files[0])
	if err != nil {
		return nil, fmt.Errorf("failed to open first file: %w", err)
	}

	info := firstFile.Info
	log.Printf("Format: %d Hz, %d channels, %d bits", info.SampleRate, info.NChannels, info.BitsPerSample)

	bufferSize := int(info.SampleRate) * int(info.NChannels) * 3
	ringBuffer := NewRingBuffer(bufferSize)

	player := &PlaylistPlayer{
		ringBuffer:   ringBuffer,
		playlist:     files,
		currentTrack: 0,
		currentFile:  firstFile,
		sampleRate:   int(info.SampleRate),
		channels:     int(info.NChannels),
		stopDecode:   make(chan bool),
		decodeDone:   make(chan bool),
	}

	format := pipewire.AudioFormat{
		SampleRate: int(info.SampleRate),
		Channels:   int(info.NChannels),
	}

	pwStream, err := pipewire.NewStream("Playlist Player", format, player.processCallback)
	if err != nil {
		firstFile.Close()
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	player.stream = pwStream

	go player.decoderThread()

	log.Println("Buffering...")
	for player.ringBuffer.Available() < bufferSize/4 && !player.IsEOF() {
		time.Sleep(10 * time.Millisecond)
	}

	if err := pwStream.Connect(format); err != nil {
		close(player.stopDecode)
		firstFile.Close()
		return nil, fmt.Errorf("failed to connect stream: %w", err)
	}

	return player, nil
}

func (p *PlaylistPlayer) decoderThread() {
	defer close(p.decodeDone)

	for {
		select {
		case <-p.stopDecode:
			return
		default:
		}

		if p.currentFile == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if p.ringBuffer.Space() < 4096*p.channels {
			time.Sleep(5 * time.Millisecond)
			continue
		}

		frame, err := p.currentFile.ParseNext()
		if err != nil {
			if err == io.EOF {
				// Current track finished - check for preloaded next
				p.mu.Lock()
				if p.nextFile != nil {
					// Gapless transition to preloaded track
					if p.currentFile != nil {
						p.currentFile.Close()
					}
					p.currentFile = p.nextFile
					p.nextFile = nil
					p.currentTrack++
					log.Printf("Gapless transition to track %d", p.currentTrack+1)
					p.mu.Unlock()
					continue
				}
				p.mu.Unlock()

				// No preloaded track - try normal advance
				if !p.nextTrack() {
					p.mu.Lock()
					p.eof = true
					p.mu.Unlock()
					return
				}
				continue
			}
			log.Printf("Error reading frame: %v", err)
			p.mu.Lock()
			p.eof = true
			p.mu.Unlock()
			return
		}

		// Preload next track when buffer is getting low
		available := p.ringBuffer.Available()
		bufferCapacity := p.sampleRate * p.channels * 3
		if available < bufferCapacity/2 {
			p.mu.Lock()
			if p.nextFile == nil && p.currentTrack+1 < len(p.playlist) {
				go p.preloadNextTrack()
			}
			p.mu.Unlock()
		}

		bitsPerSample := p.currentFile.Info.BitsPerSample
		samples := p.convertSamples(frame, bitsPerSample)

		written := 0
		for written < len(samples) {
			n := p.ringBuffer.Write(samples[written:])
			if n == 0 {
				time.Sleep(1 * time.Millisecond)
			}
			written += n
		}
	}
}

func (p *PlaylistPlayer) convertSamples(frame *frame.Frame, bitsPerSample uint8) []float32 {
	samples := make([]float32, 0, frame.Subframes[0].NSamples*p.channels)
	var divisor float32

	switch bitsPerSample {
	case 16:
		divisor = 32768.0 // 2^15
	case 24:
		divisor = 8388608.0 // 2^23
	case 32:
		divisor = 2147483648.0 // 2^31
	default:
		divisor = float32(int32(1) << (bitsPerSample - 1))
	}

	for i := 0; i < frame.Subframes[0].NSamples; i++ {
		for ch := 0; ch < p.channels; ch++ {
			sample := frame.Subframes[ch].Samples[i]
			normalized := float32(sample) / divisor
			samples = append(samples, normalized)
		}
	}
	return samples
}

func (p *PlaylistPlayer) preloadNextTrack() {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if already preloaded
	if p.nextFile != nil {
		return
	}

	nextIdx := p.currentTrack + 1
	if nextIdx >= len(p.playlist) {
		return // No next track
	}

	nextPath := p.playlist[nextIdx]
	file, err := flac.Open(nextPath)
	if err != nil {
		log.Printf("Failed to preload track %d: %v", nextIdx+1, err)
		return
	}

	p.nextFile = file
	log.Printf("Preloaded next track: %s", filepath.Base(nextPath))
}

func (p *PlaylistPlayer) nextTrack() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currentFile != nil {
		p.currentFile.Close()
		p.currentFile = nil
	}

	// Clear preloaded file since we're manually advancing
	if p.nextFile != nil {
		p.nextFile.Close()
		p.nextFile = nil
	}

	p.currentTrack++
	if p.currentTrack >= len(p.playlist) {
		log.Println("Playlist finished")
		return false
	}

	filename := p.playlist[p.currentTrack]
	log.Printf("Now playing [%d/%d]: %s", p.currentTrack+1, len(p.playlist), filepath.Base(filename))

	file, err := flac.Open(filename)
	if err != nil {
		log.Printf("Failed to open %s: %v", filename, err)
		return p.nextTrack()
	}

	p.currentFile = file
	return true
}

func (p *PlaylistPlayer) processCallback(buffer []byte, frames int) int {
	if p.IsEOF() && p.ringBuffer.Available() == 0 {
		for i := range buffer {
			buffer[i] = 0
		}
		return 0
	}

	samplesNeeded := frames * p.channels
	output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])), samplesNeeded)

	read := p.ringBuffer.Read(output)

	if read < samplesNeeded {
		for i := read; i < samplesNeeded; i++ {
			output[i] = 0
		}
	}

	return frames
}

func (p *PlaylistPlayer) Close() {
	close(p.stopDecode)
	<-p.decodeDone

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

	if p.stream != nil {
		p.stream.Destroy()
		p.stream = nil
	}
}

func (p *PlaylistPlayer) IsEOF() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.eof
}

func (p *PlaylistPlayer) CurrentTrack() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.currentTrack < len(p.playlist) {
		return p.playlist[p.currentTrack]
	}
	return ""
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <flac-file1> [flac-file2] ...\n", os.Args[0])
		os.Exit(1)
	}

	files := os.Args[1:]

	if err := pipewire.Init(); err != nil {
		log.Fatalf("Failed to initialize PipeWire: %v", err)
	}
	defer pipewire.Deinit()

	log.Printf("Playlist: %d tracks", len(files))
	for i, f := range files {
		log.Printf("  [%d] %s", i+1, filepath.Base(f))
	}

	player, err := NewPlaylistPlayer(files)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}
	defer player.Close()

	log.Printf("Now playing [1/%d]: %s", len(files), filepath.Base(files[0]))
	log.Println("Playing... Press Ctrl+C to stop")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			log.Println("\nInterrupted, stopping playback...")
			return
		case <-ticker.C:
			if player.IsEOF() && player.ringBuffer.Available() == 0 {
				log.Println("Playlist finished")
				time.Sleep(200 * time.Millisecond)
				return
			}
		}
	}
}
