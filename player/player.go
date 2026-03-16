package player

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"git.sr.ht/~uid/pwplay/pipewire"
)

// Lock-free ring buffer using atomic operations for single producer/consumer
type RingBuffer struct {
	buffer []float32
	size   int
	read   uint64 // atomic
	write  uint64 // atomic
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buffer: make([]float32, size),
		size:   size,
	}
}

func (rb *RingBuffer) Write(samples []float32) int {
	written := 0
	for _, sample := range samples {
		// Load current positions atomically
		writePos := atomic.LoadUint64(&rb.write)
		readPos := atomic.LoadUint64(&rb.read)

		// Calculate next write position
		nextWrite := (writePos + 1) % uint64(rb.size)

		// Check if buffer is full
		if nextWrite == readPos%uint64(rb.size) {
			break
		}

		// Write sample at current position
		rb.buffer[writePos%uint64(rb.size)] = sample

		// Update write position atomically
		atomic.StoreUint64(&rb.write, nextWrite)
		written++
	}
	return written
}

func (rb *RingBuffer) Read(samples []float32) int {
	read := 0
	for i := range samples {
		// Load current positions atomically
		readPos := atomic.LoadUint64(&rb.read)
		writePos := atomic.LoadUint64(&rb.write)

		// Check if buffer is empty
		if readPos%uint64(rb.size) == writePos%uint64(rb.size) {
			break
		}

		// Read sample at current position
		samples[i] = rb.buffer[readPos%uint64(rb.size)]

		// Update read position atomically
		atomic.StoreUint64(&rb.read, (readPos+1)%uint64(rb.size))
		read++
	}
	return read
}

func (rb *RingBuffer) Available() int {
	readPos := atomic.LoadUint64(&rb.read)
	writePos := atomic.LoadUint64(&rb.write)

	rIdx := readPos % uint64(rb.size)
	wIdx := writePos % uint64(rb.size)

	if wIdx >= rIdx {
		return int(wIdx - rIdx)
	}
	return rb.size - int(rIdx) + int(wIdx)
}

func (rb *RingBuffer) Clear() {
	atomic.StoreUint64(&rb.read, 0)
	atomic.StoreUint64(&rb.write, 0)
}

type Player struct {
	stream       *pipewire.Stream
	ringBuffer   *RingBuffer
	playlist     []string
	currentTrack int32
	currentFile  AudioDecoder
	nextFile     AudioDecoder
	paused       int32
	stopped      int32
	eof          int32
	nextTrack    int32
	prevTrack    int32
	addTrack     chan string
	removeTrack  chan int
	stopDecode   chan bool
	decodeDone   chan bool
	sampleRate   int
	channels     int
	mu           sync.RWMutex

	// Seek support: seekTarget holds the target sample position for a pending
	// seek, or -1 when no seek is pending. The decoder thread polls this value
	// and performs the seek when it finds a non-negative value.
	seekTarget int64 // atomic; -1 = no pending seek

	// Position tracking. The decoder thread writes decoderPos (the decoder's
	// current sample position). The process callback atomically advances
	// playbackPos each time it consumes samples from the ring buffer, but on
	// seek we need to snap it to the new position. We use samplesInBuffer to
	// know the offset between decoder position and actual playback position.
	decoderPos int64 // atomic; per-channel sample position of the decoder
	// playbackSamples counts interleaved samples consumed by the callback
	// since the last seek/track-change. Combined with seekBase, this gives
	// the current playback position.
	seekBase        int64 // atomic; set on seek/track-change to decoder target
	playbackSamples int64 // atomic; interleaved samples consumed since seekBase

	// Duration of the current track in per-channel samples, or -1 if unknown.
	trackDuration int64 // atomic

	// Volume as a linear gain factor stored atomically as uint32 (float32 bits).
	// 0.0 = silent, 1.0 = unity gain (default), values > 1.0 = amplify.
	volume uint32 // atomic; stores math.Float32bits(gain)

	// Passthrough mode: exclusive device access, no PipeWire resampling/mixing,
	// no software volume. Enables bit-perfect output.
	passthrough bool
}

// ExpandPlaylist takes a list of paths (files or directories) and returns
// a list of audio files. Directories are scanned recursively for supported formats.
func ExpandPlaylist(paths []string) ([]string, error) {
	var files []string
	supported := map[string]bool{
		".flac": true,
		".mp3":  true,
		".wav":  true,
		".ogg":  true,
	}

	for _, path := range paths {
		// Pass HTTP(S) URLs through without filesystem checks
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
			files = append(files, path)
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("cannot access %s: %w", path, err)
		}

		if info.IsDir() {
			// Walk directory recursively
			err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					ext := strings.ToLower(filepath.Ext(filePath))
					if supported[ext] {
						files = append(files, filePath)
					}
				}
				return nil
			})
			if err != nil {
				return nil, fmt.Errorf("error scanning directory %s: %w", path, err)
			}
		} else {
			// Single file
			ext := strings.ToLower(filepath.Ext(path))
			if supported[ext] {
				files = append(files, path)
			} else {
				return nil, fmt.Errorf("unsupported file format: %s", path)
			}
		}
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no audio files found")
	}

	// Sort files for consistent playback order
	sort.Strings(files)

	return files, nil
}

// PlayerOptions configures optional player behavior.
type PlayerOptions struct {
	// StartPaused starts the player in a paused state.
	StartPaused bool
	// Passthrough disables software volume control, prevents PipeWire
	// channel remixing, and requests the audio device run at the
	// stream's native sample rate.
	Passthrough bool
	// Exclusive requests sole access to the audio device. Other
	// streams are disconnected while playing. May cause silence if
	// the session manager cannot grant exclusive access.
	Exclusive bool
}

func NewPlayer(files []string, startPaused bool) (*Player, error) {
	return NewPlayerWithOptions(files, PlayerOptions{StartPaused: startPaused})
}

func NewPlayerWithOptions(files []string, opts PlayerOptions) (*Player, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("empty playlist")
	}

	firstFile, err := OpenAudioFile(files[0])
	if err != nil {
		return nil, err
	}

	bufferSize := firstFile.SampleRate() * firstFile.Channels() * 3

	p := &Player{
		ringBuffer:  NewRingBuffer(bufferSize),
		playlist:    files,
		currentFile: firstFile,
		sampleRate:  firstFile.SampleRate(),
		channels:    firstFile.Channels(),
		addTrack:    make(chan string, 10),
		removeTrack: make(chan int, 10),
		stopDecode:  make(chan bool),
		decodeDone:  make(chan bool),
		passthrough: opts.Passthrough,
	}

	atomic.StoreInt64(&p.seekTarget, -1)
	atomic.StoreInt64(&p.trackDuration, firstFile.Duration())
	atomic.StoreUint32(&p.volume, math.Float32bits(1.0))

	if opts.StartPaused {
		atomic.StoreInt32(&p.paused, 1)
	}

	format := pipewire.AudioFormat{
		SampleRate: p.sampleRate,
		Channels:   p.channels,
	}

	streamOpts := pipewire.StreamOptions{
		Passthrough: opts.Passthrough,
		Exclusive:   opts.Exclusive,
	}
	pwStream, err := pipewire.NewStreamWithOptions("Audio Player", format, p.processCallback, streamOpts)
	if err != nil {
		firstFile.Close()
		return nil, err
	}

	p.stream = pwStream
	go p.decoderThread()

	if err := pwStream.Connect(format); err != nil {
		close(p.stopDecode)
		firstFile.Close()
		return nil, err
	}

	return p, nil
}

func (p *Player) decoderThread() {
	defer close(p.decodeDone)

	for {
		select {
		case <-p.stopDecode:
			return
		case path := <-p.addTrack:
			p.mu.Lock()
			p.playlist = append(p.playlist, path)
			p.mu.Unlock()
			// If we were at EOF, clear it so the new track can be played
			if atomic.LoadInt32(&p.eof) == 1 {
				atomic.StoreInt32(&p.eof, 0)
			}
		case idx := <-p.removeTrack:
			p.mu.Lock()
			if idx >= 0 && idx < len(p.playlist) && idx != int(atomic.LoadInt32(&p.currentTrack)) {
				p.playlist = append(p.playlist[:idx], p.playlist[idx+1:]...)
			}
			p.mu.Unlock()
		default:
		}

		// Handle pending seek
		target := atomic.LoadInt64(&p.seekTarget)
		if target >= 0 {
			p.mu.RLock()
			currentFile := p.currentFile
			p.mu.RUnlock()

			if currentFile != nil {
				err := currentFile.Seek(target)
				if err != nil {
					log.Printf("Seek failed: %v", err)
				} else {
					// Clear ring buffer so stale audio is discarded immediately
					p.ringBuffer.Clear()
					// Reset playback position tracking
					atomic.StoreInt64(&p.seekBase, target)
					atomic.StoreInt64(&p.playbackSamples, 0)
					atomic.StoreInt64(&p.decoderPos, target)
				}
			}
			// Clear the seek request regardless of success
			atomic.StoreInt64(&p.seekTarget, -1)
			continue
		}

		if atomic.LoadInt32(&p.stopped) == 1 || atomic.LoadInt32(&p.paused) == 1 {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		if atomic.LoadInt32(&p.nextTrack) == 1 {
			atomic.StoreInt32(&p.nextTrack, 0)
			if p.loadTrack(int(atomic.LoadInt32(&p.currentTrack)) + 1) {
				atomic.StoreInt32(&p.stopped, 0)
			}
			continue
		}

		if atomic.LoadInt32(&p.prevTrack) == 1 {
			atomic.StoreInt32(&p.prevTrack, 0)
			if p.loadTrack(int(atomic.LoadInt32(&p.currentTrack)) - 1) {
				atomic.StoreInt32(&p.stopped, 0)
			}
			continue
		}

		p.mu.RLock()
		currentFile := p.currentFile
		p.mu.RUnlock()

		if currentFile == nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Read samples directly from decoder
		sampleBuf := make([]float32, 4096)
		n, err := currentFile.ReadSamples(sampleBuf)
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
					atomic.AddInt32(&p.currentTrack, 1)
					atomic.StoreInt64(&p.trackDuration, p.currentFile.Duration())
					// Reset position tracking for new track
					atomic.StoreInt64(&p.seekBase, 0)
					atomic.StoreInt64(&p.playbackSamples, 0)
					atomic.StoreInt64(&p.decoderPos, 0)
					log.Printf("Gapless transition to track %d", atomic.LoadInt32(&p.currentTrack)+1)
					p.mu.Unlock()
					continue
				}
				p.mu.Unlock()

				// No preloaded track - try to load next
				if !p.loadTrack(int(atomic.LoadInt32(&p.currentTrack)) + 1) {
					atomic.StoreInt32(&p.stopped, 1)
					atomic.StoreInt32(&p.eof, 1)
				}
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Update decoder position
		if currentFile != nil {
			atomic.StoreInt64(&p.decoderPos, currentFile.Position())
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

		// Write samples to ring buffer
		written := 0
		for written < n {
			// Check for pending seek while writing - abort to handle it immediately
			if atomic.LoadInt64(&p.seekTarget) >= 0 {
				break
			}
			w := p.ringBuffer.Write(sampleBuf[written:n])
			written += w
			if w == 0 {
				time.Sleep(1 * time.Millisecond)
			}
		}
	}
}

func (p *Player) preloadNextTrack() {
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
	file, err := OpenAudioFile(nextPath)
	if err != nil {
		log.Printf("Failed to preload track %d: %v", nextIdx+1, err)
		return
	}

	p.nextFile = file
	log.Printf("Preloaded next track: %s", filepath.Base(nextPath))
}

func (p *Player) loadTrack(idx int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if idx < 0 || idx >= len(p.playlist) {
		return false
	}

	if p.currentFile != nil {
		p.currentFile.Close()
	}

	if p.nextFile != nil {
		p.nextFile.Close()
		p.nextFile = nil
	}

	file, err := OpenAudioFile(p.playlist[idx])
	if err != nil {
		log.Printf("Failed to load track %d: %v", idx+1, err)
		return false
	}

	p.currentFile = file
	atomic.StoreInt32(&p.currentTrack, int32(idx))
	atomic.StoreInt64(&p.trackDuration, file.Duration())
	// Reset position tracking for new track
	atomic.StoreInt64(&p.seekBase, 0)
	atomic.StoreInt64(&p.playbackSamples, 0)
	atomic.StoreInt64(&p.decoderPos, 0)
	p.ringBuffer.Clear()

	log.Printf("Now playing [%d/%d]: %s", idx+1, len(p.playlist), filepath.Base(p.playlist[idx]))
	return true
}

func (p *Player) processCallback(buffer []byte, frames int) int {
	if atomic.LoadInt32(&p.paused) == 1 {
		for i := range buffer {
			buffer[i] = 0
		}
		return frames
	}

	output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])), frames*p.channels)
	read := p.ringBuffer.Read(output)

	// Track playback position: 'read' is number of interleaved samples consumed
	atomic.AddInt64(&p.playbackSamples, int64(read))

	// Apply volume gain
	gain := math.Float32frombits(atomic.LoadUint32(&p.volume))
	if gain != 1.0 {
		for i := 0; i < read; i++ {
			output[i] *= gain
		}
	}

	for i := read; i < len(output); i++ {
		output[i] = 0
	}

	return frames
}

// Control methods
func (p *Player) Play() {
	atomic.StoreInt32(&p.paused, 0)
	atomic.StoreInt32(&p.stopped, 0)
}

func (p *Player) Pause() {
	atomic.StoreInt32(&p.paused, 1)
}

func (p *Player) Stop() {
	atomic.StoreInt32(&p.stopped, 1)
	p.ringBuffer.Clear()
}

func (p *Player) Next() {
	atomic.StoreInt32(&p.nextTrack, 1)
}

func (p *Player) Previous() {
	atomic.StoreInt32(&p.prevTrack, 1)
}

func (p *Player) AddTrack(path string) {
	p.addTrack <- path
}

func (p *Player) RemoveTrack(idx int) {
	p.removeTrack <- idx
}

// MoveItems moves playlist items in the range [from, from+count) to start at
// index dst. The dst is the target index in the playlist *before* the items
// are removed from their original position. Returns an error if the indices
// are out of bounds.
func (p *Player) MoveItems(from, count, dst int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.playlist)
	if from < 0 || count <= 0 || from+count > n {
		return fmt.Errorf("invalid range: from=%d count=%d playlist=%d", from, count, n)
	}
	if dst < 0 || dst > n-count {
		return fmt.Errorf("invalid destination: dst=%d (valid 0..%d)", dst, n-count)
	}
	if dst == from {
		return nil // no-op
	}

	// Track where the currently playing track ends up
	cur := int(atomic.LoadInt32(&p.currentTrack))

	// Extract the items being moved
	items := make([]string, count)
	copy(items, p.playlist[from:from+count])

	// Build new playlist: remove the range, then insert at dst
	rest := make([]string, 0, n-count)
	rest = append(rest, p.playlist[:from]...)
	rest = append(rest, p.playlist[from+count:]...)

	newPlaylist := make([]string, 0, n)
	newPlaylist = append(newPlaylist, rest[:dst]...)
	newPlaylist = append(newPlaylist, items...)
	newPlaylist = append(newPlaylist, rest[dst:]...)
	p.playlist = newPlaylist

	// Update currentTrack index to follow the currently playing track
	if cur >= from && cur < from+count {
		// Current track is inside the moved range
		offset := cur - from
		atomic.StoreInt32(&p.currentTrack, int32(dst+offset))
	} else {
		// Current track is outside the moved range -- compute its new position
		newCur := cur
		if cur >= from+count {
			newCur -= count // items were removed before this position
		} else if cur >= from {
			// already handled above
		}
		if newCur >= dst {
			newCur += count // items were inserted before this position
		}
		atomic.StoreInt32(&p.currentTrack, int32(newCur))
	}

	return nil
}

// SetVolume sets the playback volume as a linear gain factor.
// 0.0 = silent, 1.0 = unity gain (default). Values above 1.0 amplify.
// The value is clamped to [0.0, 2.0].
// In passthrough mode this is a no-op (volume is always 1.0).
func (p *Player) SetVolume(v float64) {
	if p.passthrough {
		return
	}
	if v < 0 {
		v = 0
	}
	if v > 2 {
		v = 2
	}
	atomic.StoreUint32(&p.volume, math.Float32bits(float32(v)))
}

// Volume returns the current volume as a linear gain factor (0.0 - 2.0).
func (p *Player) Volume() float64 {
	return float64(math.Float32frombits(atomic.LoadUint32(&p.volume)))
}

// IsPassthrough returns true if the player is in bit-perfect passthrough mode.
func (p *Player) IsPassthrough() bool {
	return p.passthrough
}

// Seek seeks to the given position in seconds within the current track.
// The seek is performed asynchronously by the decoder thread for immediate,
// gap-free playback. The ring buffer is cleared so no stale audio is heard.
func (p *Player) Seek(seconds float64) {
	sampleRate := p.sampleRate
	if sampleRate <= 0 {
		return
	}
	target := int64(seconds * float64(sampleRate))
	if target < 0 {
		target = 0
	}
	atomic.StoreInt64(&p.seekTarget, target)
}

// SeekRelative seeks forward or backward by the given number of seconds
// relative to the current playback position.
func (p *Player) SeekRelative(seconds float64) {
	currentPos := p.Position()
	newPos := currentPos + seconds
	if newPos < 0 {
		newPos = 0
	}
	p.Seek(newPos)
}

// Status methods
func (p *Player) IsPlaying() bool {
	return atomic.LoadInt32(&p.paused) == 0 && atomic.LoadInt32(&p.stopped) == 0
}

func (p *Player) IsPaused() bool {
	return atomic.LoadInt32(&p.paused) == 1
}

func (p *Player) IsStopped() bool {
	return atomic.LoadInt32(&p.stopped) == 1
}

func (p *Player) IsEOF() bool {
	return atomic.LoadInt32(&p.eof) == 1
}

func (p *Player) CurrentTrack() int {
	return int(atomic.LoadInt32(&p.currentTrack))
}

func (p *Player) CurrentFile() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	idx := int(atomic.LoadInt32(&p.currentTrack))
	if idx >= 0 && idx < len(p.playlist) {
		return p.playlist[idx]
	}
	return ""
}

func (p *Player) Playlist() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	playlist := make([]string, len(p.playlist))
	copy(playlist, p.playlist)
	return playlist
}

// Position returns the current playback position in seconds within the
// current track.
func (p *Player) Position() float64 {
	if p.sampleRate <= 0 || p.channels <= 0 {
		return 0
	}
	base := atomic.LoadInt64(&p.seekBase)
	consumed := atomic.LoadInt64(&p.playbackSamples)
	// consumed is in interleaved samples; divide by channels for per-channel
	pos := base + consumed/int64(p.channels)
	return float64(pos) / float64(p.sampleRate)
}

// TrackDuration returns the total duration of the current track in seconds,
// or -1 if unknown.
func (p *Player) TrackDuration() float64 {
	dur := atomic.LoadInt64(&p.trackDuration)
	if dur <= 0 || p.sampleRate <= 0 {
		return -1
	}
	return float64(dur) / float64(p.sampleRate)
}

func (p *Player) Close() {
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
