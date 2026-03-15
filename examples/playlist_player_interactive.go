package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"git.sr.ht/~uid/pwplay/pipewire"
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

func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.read = 0
	rb.write = 0
}

type InteractivePlayer struct {
	stream       *pipewire.Stream
	ringBuffer   *RingBuffer
	playlist     []string
	currentTrack int32
	currentFile  *flac.Stream
	nextFile     *flac.Stream // Preloaded next track
	paused       int32
	stopped      int32
	nextTrack    int32
	prevTrack    int32
	stopDecode   chan bool
	decodeDone   chan bool
	sampleRate   int
	channels     int
	mu           sync.Mutex
}

func NewInteractivePlayer(files []string) (*InteractivePlayer, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("empty playlist")
	}

	firstFile, err := flac.Open(files[0])
	if err != nil {
		return nil, fmt.Errorf("failed to open first file: %w", err)
	}

	info := firstFile.Info
	log.Printf("Format: %d Hz, %d channels, %d bits", info.SampleRate, info.NChannels, info.BitsPerSample)

	bufferSize := int(info.SampleRate) * int(info.NChannels) * 3
	ringBuffer := NewRingBuffer(bufferSize)

	player := &InteractivePlayer{
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

	pwStream, err := pipewire.NewStream("Interactive Player", format, player.processCallback)
	if err != nil {
		firstFile.Close()
		return nil, fmt.Errorf("failed to create stream: %w", err)
	}

	player.stream = pwStream

	go player.decoderThread()

	log.Println("Buffering...")
	for player.ringBuffer.Available() < bufferSize/4 {
		time.Sleep(10 * time.Millisecond)
	}

	if err := pwStream.Connect(format); err != nil {
		close(player.stopDecode)
		firstFile.Close()
		return nil, fmt.Errorf("failed to connect stream: %w", err)
	}

	return player, nil
}

func (p *InteractivePlayer) decoderThread() {
	defer close(p.decodeDone)

	for {
		select {
		case <-p.stopDecode:
			return
		default:
		}

		if atomic.LoadInt32(&p.stopped) == 1 {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if atomic.LoadInt32(&p.paused) == 1 {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if atomic.LoadInt32(&p.nextTrack) == 1 {
			atomic.StoreInt32(&p.nextTrack, 0)
			p.loadNextTrack()
			continue
		}

		if atomic.LoadInt32(&p.prevTrack) == 1 {
			atomic.StoreInt32(&p.prevTrack, 0)
			p.loadPreviousTrack()
			continue
		}

		p.mu.Lock()
		currentFile := p.currentFile
		p.mu.Unlock()

		if currentFile == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if p.ringBuffer.Available() > p.sampleRate*p.channels*2 {
			time.Sleep(5 * time.Millisecond)
			continue
		}

		frame, err := currentFile.ParseNext()
		if err != nil {
			if err == io.EOF {
				// Check for preloaded next track
				p.mu.Lock()
				if p.nextFile != nil {
					// Gapless transition
					if p.currentFile != nil {
						p.currentFile.Close()
					}
					p.currentFile = p.nextFile
					p.nextFile = nil
					atomic.AddInt32(&p.currentTrack, 1)
					log.Printf("Gapless transition to track %d", atomic.LoadInt32(&p.currentTrack)+1)
					p.mu.Unlock()
					continue
				}
				p.mu.Unlock()

				if !p.autoNextTrack() {
					atomic.StoreInt32(&p.stopped, 1)
				}
				continue
			}
			log.Printf("Error: %v", err)
			atomic.StoreInt32(&p.stopped, 1)
			return
		}

		// Preload next track when buffer is getting low
		available := p.ringBuffer.Available()
		bufferCapacity := p.sampleRate * p.channels * 3
		if available < bufferCapacity/2 {
			p.mu.Lock()
			currentIdx := int(atomic.LoadInt32(&p.currentTrack))
			if p.nextFile == nil && currentIdx+1 < len(p.playlist) {
				go p.preloadNextTrack()
			}
			p.mu.Unlock()
		}

		bitsPerSample := currentFile.Info.BitsPerSample
		samples := p.convertSamples(frame, bitsPerSample)

		written := 0
		for written < len(samples) {
			n := p.ringBuffer.Write(samples[written:])
			written += n
			if n == 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}
	}
}

func (p *InteractivePlayer) convertSamples(frame *frame.Frame, bitsPerSample uint8) []float32 {
	samples := make([]float32, 0, frame.Subframes[0].NSamples*p.channels)
	var divisor float32

	switch bitsPerSample {
	case 16:
		divisor = 32768.0
	case 24:
		divisor = 8388608.0
	case 32:
		divisor = 2147483648.0
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

func (p *InteractivePlayer) preloadNextTrack() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.nextFile != nil {
		return
	}

	nextIdx := int(atomic.LoadInt32(&p.currentTrack)) + 1
	if nextIdx >= len(p.playlist) {
		return
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

func (p *InteractivePlayer) loadTrack(index int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if index < 0 || index >= len(p.playlist) {
		return false
	}

	if p.currentFile != nil {
		p.currentFile.Close()
	}

	// Clear preloaded file on manual track change
	if p.nextFile != nil {
		p.nextFile.Close()
		p.nextFile = nil
	}

	filename := p.playlist[index]
	file, err := flac.Open(filename)
	if err != nil {
		log.Printf("Failed to open %s: %v", filename, err)
		return false
	}

	p.currentFile = file
	atomic.StoreInt32(&p.currentTrack, int32(index))
	p.ringBuffer.Clear()

	log.Printf("Now playing [%d/%d]: %s", index+1, len(p.playlist), filepath.Base(filename))
	return true
}

func (p *InteractivePlayer) autoNextTrack() bool {
	current := int(atomic.LoadInt32(&p.currentTrack))
	return p.loadTrack(current + 1)
}

func (p *InteractivePlayer) loadNextTrack() {
	current := int(atomic.LoadInt32(&p.currentTrack))
	if !p.loadTrack(current + 1) {
		log.Println("End of playlist")
	}
}

func (p *InteractivePlayer) loadPreviousTrack() {
	current := int(atomic.LoadInt32(&p.currentTrack))
	if !p.loadTrack(current - 1) {
		log.Println("Start of playlist")
	}
}

func (p *InteractivePlayer) processCallback(buffer []byte, frames int) int {
	if atomic.LoadInt32(&p.paused) == 1 {
		for i := range buffer {
			buffer[i] = 0
		}
		return frames
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

func (p *InteractivePlayer) Pause() {
	atomic.StoreInt32(&p.paused, 1)
	log.Println("⏸  Paused")
}

func (p *InteractivePlayer) Play() {
	atomic.StoreInt32(&p.paused, 0)
	log.Println("▶  Playing")
}

func (p *InteractivePlayer) Stop() {
	atomic.StoreInt32(&p.stopped, 1)
	p.ringBuffer.Clear()
	log.Println("⏹  Stopped")
}

func (p *InteractivePlayer) Next() {
	atomic.StoreInt32(&p.nextTrack, 1)
	log.Println("⏭  Next track")
}

func (p *InteractivePlayer) Previous() {
	atomic.StoreInt32(&p.prevTrack, 1)
	log.Println("⏮  Previous track")
}

func (p *InteractivePlayer) IsPaused() bool {
	return atomic.LoadInt32(&p.paused) == 1
}

func (p *InteractivePlayer) IsStopped() bool {
	return atomic.LoadInt32(&p.stopped) == 1
}

func (p *InteractivePlayer) CurrentTrackInfo() string {
	idx := int(atomic.LoadInt32(&p.currentTrack))
	if idx >= 0 && idx < len(p.playlist) {
		return fmt.Sprintf("[%d/%d] %s", idx+1, len(p.playlist), filepath.Base(p.playlist[idx]))
	}
	return ""
}

func (p *InteractivePlayer) Close() {
	close(p.stopDecode)
	<-p.decodeDone

	p.mu.Lock()
	if p.currentFile != nil {
		p.currentFile.Close()
	}
	if p.nextFile != nil {
		p.nextFile.Close()
	}
	p.mu.Unlock()

	if p.stream != nil {
		p.stream.Destroy()
	}
}

func printHelp() {
	fmt.Println("\nControls:")
	fmt.Println("  space    - Play/Pause")
	fmt.Println("  n        - Next track")
	fmt.Println("  p        - Previous track")
	fmt.Println("  s        - Stop")
	fmt.Println("  i        - Track info")
	fmt.Println("  h/?      - Help")
	fmt.Println("  q        - Quit")
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

	player, err := NewInteractivePlayer(files)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}
	defer player.Close()

	log.Printf("Now playing [1/%d]: %s", len(files), filepath.Base(files[0]))
	printHelp()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		cmd := strings.TrimSpace(strings.ToLower(scanner.Text()))

		switch cmd {
		case " ", "":
			if player.IsPaused() {
				player.Play()
			} else {
				player.Pause()
			}
		case "n", "next":
			player.Next()
		case "p", "prev", "previous":
			player.Previous()
		case "s", "stop":
			player.Stop()
		case "i", "info":
			fmt.Printf("🎵 %s\n", player.CurrentTrackInfo())
		case "h", "?", "help":
			printHelp()
		case "q", "quit", "exit":
			log.Println("Bye!")
			return
		default:
			fmt.Println("Unknown command. Type 'h' for help")
		}

		if player.IsStopped() {
			log.Println("Playback stopped. Press 'q' to quit")
		}
	}
}
