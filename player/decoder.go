package player

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-audio/wav"
	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
	"github.com/mewkiz/flac"
	"github.com/pion/opus"
	"github.com/pion/opus/pkg/oggreader"
)

// AudioDecoder provides a unified interface for all audio formats
type AudioDecoder interface {
	SampleRate() int
	Channels() int
	BitsPerSample() int
	ReadSamples(buf []float32) (int, error)
	// Seek seeks to the given sample position (per-channel sample number).
	// Returns an error if seeking is not supported.
	Seek(samplePos int64) error
	// Position returns the current position in per-channel samples.
	Position() int64
	// Duration returns the total duration in per-channel samples, or -1 if unknown.
	Duration() int64
	Close() error
}

// FLACDecoder wraps flac.Stream
type FLACDecoder struct {
	stream       *flac.Stream
	currentFrame int
	frameBuffer  []float32
	bufferPos    int
	position     int64 // current position in per-channel samples
	duration     int64 // total per-channel samples
}

func newFLACDecoder(stream *flac.Stream) *FLACDecoder {
	return &FLACDecoder{
		stream:      stream,
		frameBuffer: make([]float32, 0),
		duration:    int64(stream.Info.NSamples),
	}
}

func (d *FLACDecoder) SampleRate() int    { return int(d.stream.Info.SampleRate) }
func (d *FLACDecoder) Channels() int      { return int(d.stream.Info.NChannels) }
func (d *FLACDecoder) BitsPerSample() int { return int(d.stream.Info.BitsPerSample) }
func (d *FLACDecoder) Close() error       { return d.stream.Close() }
func (d *FLACDecoder) Position() int64    { return d.position }
func (d *FLACDecoder) Duration() int64    { return d.duration }

func (d *FLACDecoder) Seek(samplePos int64) error {
	if samplePos < 0 {
		samplePos = 0
	}
	if d.duration > 0 && samplePos >= d.duration {
		samplePos = d.duration - 1
	}
	_, err := d.stream.Seek(uint64(samplePos))
	if err != nil {
		return fmt.Errorf("FLAC seek failed: %w", err)
	}
	d.position = samplePos
	// Invalidate buffered frame data so next ReadSamples decodes fresh
	d.frameBuffer = d.frameBuffer[:0]
	d.bufferPos = 0
	return nil
}

func (d *FLACDecoder) ReadSamples(buf []float32) (int, error) {
	samplesRead := 0
	channels := int(d.stream.Info.NChannels)

	for samplesRead < len(buf) {
		// Use buffered samples first
		if d.bufferPos < len(d.frameBuffer) {
			n := copy(buf[samplesRead:], d.frameBuffer[d.bufferPos:])
			d.bufferPos += n
			samplesRead += n
			// Track position: n interleaved samples = n/channels per-channel samples
			d.position += int64(n / channels)
			continue
		}

		// Need to decode next frame
		frame, err := d.stream.ParseNext()
		if err != nil {
			if samplesRead > 0 {
				return samplesRead, nil
			}
			return 0, err
		}

		// Convert frame to float32
		bits := d.stream.Info.BitsPerSample
		var divisor float32
		switch bits {
		case 16:
			divisor = 32768.0
		case 24:
			divisor = 8388608.0
		case 32:
			divisor = 2147483648.0
		default:
			divisor = float32(int32(1) << (bits - 1))
		}

		d.frameBuffer = d.frameBuffer[:0]
		for i := 0; i < frame.Subframes[0].NSamples; i++ {
			for ch := 0; ch < channels; ch++ {
				sample := float32(frame.Subframes[ch].Samples[i]) / divisor
				d.frameBuffer = append(d.frameBuffer, sample)
			}
		}
		d.bufferPos = 0
	}

	return samplesRead, nil
}

// MP3Decoder wraps mp3.Decoder
type MP3Decoder struct {
	decoder    *mp3.Decoder
	closer     io.Closer
	position   int64 // current position in per-channel samples
	duration   int64 // total per-channel samples, -1 if unknown
	channels   int
	sampleRate int
}

func newMP3Decoder(r io.ReadCloser) (*MP3Decoder, error) {
	decoder, err := mp3.NewDecoder(r)
	if err != nil {
		r.Close()
		return nil, err
	}

	channels := 2 // go-mp3 always decodes to stereo
	// Length() returns total bytes; 4 bytes per frame (2 channels * 2 bytes int16)
	totalBytes := decoder.Length()
	var duration int64 = -1
	if totalBytes > 0 {
		bytesPerFrame := int64(channels) * 2 // int16 = 2 bytes per sample
		duration = totalBytes / bytesPerFrame
	}

	return &MP3Decoder{
		decoder:    decoder,
		closer:     r,
		channels:   channels,
		sampleRate: decoder.SampleRate(),
		duration:   duration,
	}, nil
}

func (d *MP3Decoder) SampleRate() int    { return d.sampleRate }
func (d *MP3Decoder) Channels() int      { return d.channels }
func (d *MP3Decoder) BitsPerSample() int { return 16 }
func (d *MP3Decoder) Close() error       { return d.closer.Close() }
func (d *MP3Decoder) Position() int64    { return d.position }
func (d *MP3Decoder) Duration() int64    { return d.duration }

func (d *MP3Decoder) Seek(samplePos int64) error {
	if samplePos < 0 {
		samplePos = 0
	}
	if d.duration > 0 && samplePos >= d.duration {
		samplePos = d.duration - 1
	}
	// go-mp3 Seek uses byte offsets; 4 bytes per stereo frame (2ch * int16)
	byteOffset := samplePos * int64(d.channels) * 2
	_, err := d.decoder.Seek(byteOffset, io.SeekStart)
	if err != nil {
		return fmt.Errorf("MP3 seek failed: %w", err)
	}
	d.position = samplePos
	return nil
}

func (d *MP3Decoder) ReadSamples(buf []float32) (int, error) {
	// MP3 decoder outputs int16
	intBuf := make([]byte, len(buf)*2) // 2 bytes per sample
	n, err := d.decoder.Read(intBuf)
	if err != nil && err != io.EOF {
		return 0, err
	}

	samples := n / 2 // 2 bytes per int16
	for i := 0; i < samples; i++ {
		// Convert int16 to float32
		val := int16(intBuf[i*2]) | int16(intBuf[i*2+1])<<8
		buf[i] = float32(val) / 32768.0
	}

	// Track position: samples are interleaved, so per-channel = samples/channels
	d.position += int64(samples / d.channels)

	if err == io.EOF && samples > 0 {
		return samples, nil
	}
	return samples, err
}

// WAVDecoder wraps wav.Decoder
type WAVDecoder struct {
	file       *os.File
	decoder    *wav.Decoder
	sampleRate int
	channels   int
	bits       int
	buffer     []byte
	position   int64 // current position in per-channel samples
	duration   int64 // total per-channel samples
}

func newWAVDecoder(r io.ReadCloser) (*WAVDecoder, error) {
	// WAV decoder needs ReadSeeker, so we need to handle files differently
	file, ok := r.(*os.File)
	if !ok {
		r.Close()
		return nil, fmt.Errorf("WAV streaming from HTTP not supported yet (needs ReadSeeker)")
	}

	decoder := wav.NewDecoder(file)
	if !decoder.IsValidFile() {
		file.Close()
		return nil, fmt.Errorf("invalid WAV file")
	}

	dur, err := decoder.Duration()
	var duration int64 = -1
	if err == nil {
		duration = int64(dur.Seconds() * float64(decoder.SampleRate))
	}

	return &WAVDecoder{
		file:       file,
		decoder:    decoder,
		sampleRate: int(decoder.SampleRate),
		channels:   int(decoder.NumChans),
		bits:       int(decoder.BitDepth),
		buffer:     make([]byte, 8192),
		duration:   duration,
	}, nil
}

func (d *WAVDecoder) SampleRate() int    { return d.sampleRate }
func (d *WAVDecoder) Channels() int      { return d.channels }
func (d *WAVDecoder) BitsPerSample() int { return d.bits }
func (d *WAVDecoder) Close() error       { return d.file.Close() }
func (d *WAVDecoder) Position() int64    { return d.position }
func (d *WAVDecoder) Duration() int64    { return d.duration }

func (d *WAVDecoder) Seek(samplePos int64) error {
	if samplePos < 0 {
		samplePos = 0
	}
	if d.duration > 0 && samplePos >= d.duration {
		samplePos = d.duration - 1
	}
	// WAV seek is byte-based relative to PCM data start
	bytesPerSample := int64(d.bits / 8)
	byteOffset := samplePos * int64(d.channels) * bytesPerSample
	_, err := d.decoder.Seek(byteOffset, io.SeekStart)
	if err != nil {
		return fmt.Errorf("WAV seek failed: %w", err)
	}
	d.position = samplePos
	return nil
}

func (d *WAVDecoder) ReadSamples(buf []float32) (int, error) {
	// Read raw PCM data directly
	bytesPerSample := d.bits / 8
	bytesNeeded := len(buf) * bytesPerSample
	if bytesNeeded > len(d.buffer) {
		d.buffer = make([]byte, bytesNeeded)
	}

	n, err := d.file.Read(d.buffer[:bytesNeeded])
	if err != nil && err != io.EOF {
		return 0, err
	}

	if n == 0 {
		return 0, err
	}

	// Convert bytes to float32 based on bit depth
	var divisor float32
	samplesRead := n / bytesPerSample

	switch d.bits {
	case 16:
		divisor = 32768.0
		for i := 0; i < samplesRead && i < len(buf); i++ {
			val := int16(d.buffer[i*2]) | int16(d.buffer[i*2+1])<<8
			buf[i] = float32(val) / divisor
		}
	case 24:
		divisor = 8388608.0
		for i := 0; i < samplesRead && i < len(buf); i++ {
			val := int32(d.buffer[i*3]) | int32(d.buffer[i*3+1])<<8 | int32(d.buffer[i*3+2])<<16
			if val&0x800000 != 0 {
				val |= ^0xffffff // Sign extend
			}
			buf[i] = float32(val) / divisor
		}
	case 32:
		divisor = 2147483648.0
		for i := 0; i < samplesRead && i < len(buf); i++ {
			val := int32(d.buffer[i*4]) | int32(d.buffer[i*4+1])<<8 |
				int32(d.buffer[i*4+2])<<16 | int32(d.buffer[i*4+3])<<24
			buf[i] = float32(val) / divisor
		}
	default:
		return 0, fmt.Errorf("unsupported bit depth: %d", d.bits)
	}

	// Track position
	d.position += int64(samplesRead / d.channels)

	if err == io.EOF && samplesRead > 0 {
		return samplesRead, nil
	}
	return samplesRead, err
}

// OGGDecoder wraps oggvorbis.Reader
type OGGDecoder struct {
	reader     *oggvorbis.Reader
	sampleRate int
	channels   int
	closer     io.Closer
}

func newOGGDecoder(r io.ReadCloser) (*OGGDecoder, error) {
	reader, err := oggvorbis.NewReader(r)
	if err != nil {
		r.Close()
		return nil, err
	}

	return &OGGDecoder{
		reader:     reader,
		sampleRate: reader.SampleRate(),
		channels:   reader.Channels(),
		closer:     r,
	}, nil
}

func (d *OGGDecoder) SampleRate() int    { return d.sampleRate }
func (d *OGGDecoder) Channels() int      { return d.channels }
func (d *OGGDecoder) BitsPerSample() int { return 16 } // OGG Vorbis is decoded to float
func (d *OGGDecoder) Close() error       { return d.closer.Close() }
func (d *OGGDecoder) Position() int64    { return d.reader.Position() }
func (d *OGGDecoder) Duration() int64    { return d.reader.Length() }

func (d *OGGDecoder) Seek(samplePos int64) error {
	if samplePos < 0 {
		samplePos = 0
	}
	dur := d.reader.Length()
	if dur > 0 && samplePos >= dur {
		samplePos = dur - 1
	}
	err := d.reader.SetPosition(samplePos)
	if err != nil {
		return fmt.Errorf("OGG seek failed: %w", err)
	}
	return nil
}

func (d *OGGDecoder) ReadSamples(buf []float32) (int, error) {
	n, err := d.reader.Read(buf)
	if err != nil && err != io.EOF {
		return 0, err
	}
	if err == io.EOF && n > 0 {
		return n, nil
	}
	return n, err
}

// OPUSDecoder decodes Ogg Opus (RFC 7845) files using pion/opus.
//
// Opus always decodes at 48 kHz, so SampleRate is 48000 regardless of the
// input sample rate recorded in the OpusHead header (that field is
// informational only). Only channel mapping family 0 (mono/stereo) is
// supported; the pion decoder cannot handle multistream/multichannel files.
type OPUSDecoder struct {
	rs       io.ReadSeeker
	closer   io.Closer
	ogg      *oggreader.OggReader
	decoder  opus.Decoder
	channels int
	preSkip  int64 // per-channel encoder-delay samples to discard at start
	skipLeft int64 // pre-skip samples still to discard
	position int64 // per-channel samples emitted (post pre-skip)
	duration int64 // total per-channel samples (post pre-skip), -1 if unknown
	pcmBuf   []float32
	pcm      []float32 // pending decoded samples (slice of pcmBuf)
	eof      bool
}

// opusSampleRate is the rate Opus decodes to natively.
const opusSampleRate = 48000

// maxOpusPacketSamples is the maximum per-channel samples in one Opus
// packet (120 ms at 48 kHz, per RFC 6716).
const maxOpusPacketSamples = 5760

func newOPUSDecoder(r io.ReadCloser) (*OPUSDecoder, error) {
	// Seeking requires re-reading the stream from the start, and duration
	// detection scans the file tail, so a seekable reader is needed. All
	// inputs are files (local or downloaded temp files), so this always holds.
	rs, ok := r.(io.ReadSeeker)
	if !ok {
		r.Close()
		return nil, fmt.Errorf("OPUS requires a seekable reader")
	}

	// Read the last page's granule position for the duration before the
	// oggreader consumes the stream from the start.
	lastGranule, _ := oggLastGranule(rs)
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		r.Close()
		return nil, err
	}

	ogg, header, err := oggreader.NewWith(rs)
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("failed to parse Opus header: %w", err)
	}

	if header.ChannelMap != 0 {
		r.Close()
		return nil, fmt.Errorf("unsupported Opus channel mapping family %d (only family 0 mono/stereo)", header.ChannelMap)
	}
	channels := int(header.Channels)
	if channels < 1 || channels > 2 {
		r.Close()
		return nil, fmt.Errorf("unsupported Opus channel count %d (only mono/stereo)", channels)
	}

	// RFC 7845: the second packet is the OpusTags comment header; skip it.
	if err := skipOpusTags(ogg); err != nil {
		r.Close()
		return nil, err
	}

	dec, err := opus.NewDecoderWithOutput(opusSampleRate, channels)
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("failed to create Opus decoder: %w", err)
	}

	// Granule positions count from the stream start including the pre-skip,
	// so the playable duration is the final granule minus the pre-skip.
	duration := int64(-1)
	if lastGranule >= 0 {
		duration = lastGranule - int64(header.PreSkip)
		if duration < 0 {
			duration = 0
		}
	}

	return &OPUSDecoder{
		rs:       rs,
		closer:   r,
		ogg:      ogg,
		decoder:  dec,
		channels: channels,
		preSkip:  int64(header.PreSkip),
		skipLeft: int64(header.PreSkip),
		duration: duration,
		pcmBuf:   make([]float32, maxOpusPacketSamples*channels),
	}, nil
}

// skipOpusTags consumes the OpusTags comment header packet that must follow
// the ID header in every Ogg Opus stream.
func skipOpusTags(ogg *oggreader.OggReader) error {
	pkt, _, err := ogg.ParseNextPacket()
	if err != nil {
		return fmt.Errorf("failed to read Opus comment header: %w", err)
	}
	if !bytes.HasPrefix(pkt, []byte("OpusTags")) {
		return fmt.Errorf("missing OpusTags comment header")
	}
	return nil
}

// oggLastGranule returns the granule position of the last Ogg page in the
// stream, or -1 if it cannot be determined. The stream position is not
// preserved; callers must re-seek before further reads.
func oggLastGranule(rs io.ReadSeeker) (int64, error) {
	const tailSize = 128 * 1024 // larger than the max Ogg page size (~65 KB)
	end, err := rs.Seek(0, io.SeekEnd)
	if err != nil {
		return -1, err
	}
	start := end - tailSize
	if start < 0 {
		start = 0
	}
	if _, err := rs.Seek(start, io.SeekStart); err != nil {
		return -1, err
	}
	tail := make([]byte, end-start)
	if _, err := io.ReadFull(rs, tail); err != nil {
		return -1, err
	}

	// Scan backwards for the last well-formed page: "OggS" + version 0, whose
	// declared length lands exactly on EOF or on another "OggS" signature
	// (guards against false positives inside page payload data).
	for i := len(tail) - 27; i >= 0; i-- {
		if !bytes.Equal(tail[i:i+4], []byte("OggS")) || tail[i+4] != 0 {
			continue
		}
		nsegs := int(tail[i+26])
		if i+27+nsegs > len(tail) {
			continue
		}
		pageLen := 27 + nsegs
		for j := 0; j < nsegs; j++ {
			pageLen += int(tail[i+27+j])
		}
		pageEnd := i + pageLen
		if pageEnd > len(tail) {
			continue
		}
		if pageEnd < len(tail) && !bytes.Equal(tail[pageEnd:pageEnd+4], []byte("OggS")) {
			continue
		}
		// Granule position is a little-endian uint64 at header offset 6.
		return int64(binary.LittleEndian.Uint64(tail[i+6 : i+14])), nil
	}
	return -1, fmt.Errorf("no Ogg page found")
}

func (d *OPUSDecoder) SampleRate() int    { return opusSampleRate }
func (d *OPUSDecoder) Channels() int      { return d.channels }
func (d *OPUSDecoder) BitsPerSample() int { return 16 }
func (d *OPUSDecoder) Close() error       { return d.closer.Close() }
func (d *OPUSDecoder) Position() int64    { return d.position }
func (d *OPUSDecoder) Duration() int64    { return d.duration }

func (d *OPUSDecoder) Seek(samplePos int64) error {
	if samplePos < 0 {
		samplePos = 0
	}
	if d.duration > 0 && samplePos >= d.duration {
		samplePos = d.duration - 1
	}

	// Opus packets are stateful, so a seek decodes forward from a packet
	// boundary: rewind to the start when the target is behind the current
	// position, then decode-and-discard up to the target.
	if samplePos < d.position {
		if err := d.rewind(); err != nil {
			return err
		}
	}
	discard := samplePos - d.position
	scratch := make([]float32, 8192*d.channels)
	for discard > 0 {
		chunk := int64(len(scratch) / d.channels)
		if chunk > discard {
			chunk = discard
		}
		n, err := d.ReadSamples(scratch[:chunk*int64(d.channels)])
		discard -= int64(n / d.channels)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("OPUS seek failed: %w", err)
		}
		if n == 0 {
			break
		}
	}
	return nil
}

// rewind returns the decoder to the start of the stream.
func (d *OPUSDecoder) rewind() error {
	if _, err := d.rs.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("OPUS seek failed: %w", err)
	}
	ogg, _, err := oggreader.NewWith(d.rs)
	if err != nil {
		return fmt.Errorf("OPUS seek failed: %w", err)
	}
	if err := skipOpusTags(ogg); err != nil {
		return fmt.Errorf("OPUS seek failed: %w", err)
	}
	if err := d.decoder.Init(opusSampleRate, d.channels); err != nil {
		return fmt.Errorf("OPUS seek failed: %w", err)
	}
	d.ogg = ogg
	d.pcm = nil
	d.skipLeft = d.preSkip
	d.position = 0
	d.eof = false
	return nil
}

func (d *OPUSDecoder) ReadSamples(buf []float32) (int, error) {
	samplesRead := 0
	for samplesRead < len(buf) {
		if len(d.pcm) > 0 {
			n := copy(buf[samplesRead:], d.pcm)
			d.pcm = d.pcm[n:]
			samplesRead += n
			// Track position: n interleaved samples = n/channels per-channel samples
			d.position += int64(n / d.channels)
			continue
		}
		if d.eof {
			if samplesRead > 0 {
				return samplesRead, nil
			}
			return 0, io.EOF
		}
		if err := d.decodeNextPacket(); err != nil {
			if err == io.EOF {
				d.eof = true
				continue
			}
			if samplesRead > 0 {
				return samplesRead, nil
			}
			return 0, err
		}
	}
	return samplesRead, nil
}

// decodeNextPacket decodes the next Opus packet into d.pcm, applying the
// initial pre-skip discard and the end-of-stream duration trim.
func (d *OPUSDecoder) decodeNextPacket() error {
	pkt, _, err := d.ogg.ParseNextPacket()
	if err != nil {
		return err // io.EOF at the end of the stream
	}
	n, err := d.decoder.DecodeToFloat32(pkt, d.pcmBuf)
	if err != nil {
		return fmt.Errorf("Opus decode failed: %w", err)
	}
	d.pcm = d.pcmBuf[:n*d.channels]

	// Discard the encoder delay (pre-skip) once, at the start of the stream.
	if d.skipLeft > 0 {
		skip := int(d.skipLeft) * d.channels
		if skip >= len(d.pcm) {
			d.skipLeft -= int64(n)
			d.pcm = nil
			return nil
		}
		d.pcm = d.pcm[skip:]
		d.skipLeft = 0
	}

	// End-trim: the final page's granule can indicate fewer samples than the
	// packets decode to; never emit past the stream's total duration.
	if d.duration >= 0 {
		remaining := (d.duration - d.position) * int64(d.channels)
		if remaining <= 0 {
			d.pcm = nil
			d.eof = true
			return nil
		}
		if int64(len(d.pcm)) > remaining {
			d.pcm = d.pcm[:remaining]
		}
	}
	return nil
}

// sniffOggCodec peeks at the first Ogg page's first packet to identify the
// codec: "opus" for OpusHead, "vorbis" for a Vorbis ID header, "" if
// undetermined. The stream position is restored before returning.
func sniffOggCodec(r io.ReadCloser) string {
	rs, ok := r.(io.ReadSeeker)
	if !ok {
		return ""
	}
	defer rs.Seek(0, io.SeekStart)

	header := make([]byte, 27)
	if _, err := io.ReadFull(rs, header); err != nil {
		return ""
	}
	if !bytes.Equal(header[:4], []byte("OggS")) {
		return ""
	}
	nsegs := int(header[26])
	segments := make([]byte, nsegs)
	if _, err := io.ReadFull(rs, segments); err != nil {
		return ""
	}
	// The codec signature sits at the start of the first packet, which the
	// first segment always contains in full for both ID headers.
	payload := make([]byte, segments[0])
	if _, err := io.ReadFull(rs, payload); err != nil {
		return ""
	}
	switch {
	case bytes.HasPrefix(payload, []byte("OpusHead")):
		return "opus"
	case bytes.HasPrefix(payload, []byte("\x01vorbis")):
		return "vorbis"
	}
	return ""
}

// downloadToTempFile fetches a URL and writes the contents to a temporary file.
// The returned file is open and seeked to the beginning. The caller must close
// and remove the file when done.
func downloadToTempFile(url string) (*os.File, string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")

	// Determine extension for the temp file
	ext := strings.ToLower(filepath.Ext(url))
	if ext == "" {
		// Guess from content-type
		ct := strings.ToLower(contentType)
		switch {
		case strings.Contains(ct, "flac"):
			ext = ".flac"
		case strings.Contains(ct, "mp3"), strings.Contains(ct, "mpeg"):
			ext = ".mp3"
		case strings.Contains(ct, "wav"), strings.Contains(ct, "wave"):
			ext = ".wav"
		case strings.Contains(ct, "opus"):
			ext = ".opus"
		case strings.Contains(ct, "ogg"), strings.Contains(ct, "vorbis"):
			ext = ".ogg"
		}
	}

	tmp, err := os.CreateTemp("", "pipewire-audio-*"+ext)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, "", fmt.Errorf("failed to download %s: %w", url, err)
	}

	// Seek back to start so the decoder reads from the beginning
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, "", err
	}

	log.Printf("Downloaded %s -> %s", url, tmp.Name())
	return tmp, contentType, nil
}

// tempFileDecoder wraps an AudioDecoder and cleans up the underlying temp file
// when closed.
type tempFileDecoder struct {
	AudioDecoder
	tmpPath string
}

func (d *tempFileDecoder) Close() error {
	err := d.AudioDecoder.Close()
	os.Remove(d.tmpPath)
	return err
}

// OpenAudioFile opens any supported audio format
func OpenAudioFile(path string) (AudioDecoder, error) {
	// Handle HTTP URLs: download to a temp file first so the decoder has a
	// seekable reader and the connection is not held open during playback.
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		tmp, contentType, err := downloadToTempFile(path)
		if err != nil {
			return nil, err
		}

		ext := strings.ToLower(filepath.Ext(path))
		var dec AudioDecoder
		if ext != "" {
			dec, err = openByExtension(ext, tmp, path)
		} else {
			dec, err = openByContentType(contentType, tmp, path)
		}
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return nil, err
		}
		return &tempFileDecoder{AudioDecoder: dec, tmpPath: tmp.Name()}, nil
	}

	// Local file
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(path))
	return openByExtension(ext, r, path)
}

func openByExtension(ext string, r io.ReadCloser, path string) (AudioDecoder, error) {
	switch ext {
	case ".flac":
		// Use NewSeek for seekable FLAC streams when possible
		if rs, ok := r.(io.ReadSeeker); ok {
			stream, err := flac.NewSeek(rs)
			if err != nil {
				r.Close()
				return nil, fmt.Errorf("failed to decode FLAC: %w", err)
			}
			return newFLACDecoder(stream), nil
		}
		// Fallback for non-seekable readers (e.g. HTTP)
		stream, err := flac.New(r)
		if err != nil {
			r.Close()
			return nil, fmt.Errorf("failed to decode FLAC: %w", err)
		}
		return newFLACDecoder(stream), nil

	case ".mp3":
		return newMP3Decoder(r)

	case ".wav":
		return newWAVDecoder(r)

	case ".ogg":
		// The Ogg container carries both Vorbis and Opus; sniff the first
		// packet to pick the right decoder.
		if sniffOggCodec(r) == "opus" {
			return newOPUSDecoder(r)
		}
		return newOGGDecoder(r)

	case ".opus":
		return newOPUSDecoder(r)

	default:
		r.Close()
		return nil, fmt.Errorf("unsupported format: %s (supported: .flac, .mp3, .wav, .ogg, .opus)", ext)
	}
}

func openByContentType(contentType string, r io.ReadCloser, path string) (AudioDecoder, error) {
	ct := strings.ToLower(contentType)

	if strings.Contains(ct, "flac") {
		return openByExtension(".flac", r, path)
	}
	if strings.Contains(ct, "mp3") || strings.Contains(ct, "mpeg") {
		return openByExtension(".mp3", r, path)
	}
	if strings.Contains(ct, "wav") || strings.Contains(ct, "wave") {
		return openByExtension(".wav", r, path)
	}
	if strings.Contains(ct, "opus") {
		return openByExtension(".opus", r, path)
	}
	if strings.Contains(ct, "ogg") || strings.Contains(ct, "vorbis") {
		return openByExtension(".ogg", r, path)
	}

	// Fallback to extension from path
	ext := filepath.Ext(path)
	if ext != "" {
		return openByExtension(ext, r, path)
	}

	r.Close()
	return nil, fmt.Errorf("cannot determine audio format from content-type: %s", contentType)
}
