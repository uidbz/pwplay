# Supported Audio Formats

The player now supports multiple audio formats with unified decoding.

## Supported Formats

| Format | Extension | Streaming | Notes |
|--------|-----------|-----------|-------|
| **FLAC** | .flac | ✅ HTTP/HTTPS | Lossless, 16/24/32-bit |
| **MP3** | .mp3 | ✅ HTTP/HTTPS | Lossy, stereo |
| **WAV** | .wav | ❌ Local only | Uncompressed PCM |
| **OGG Vorbis** | .ogg | ✅ HTTP/HTTPS | Lossy, variable bitrate |

## Format Details

### FLAC (Free Lossless Audio Codec)
- **Bit depths**: 16, 24, 32-bit
- **Sample rates**: Any (44.1kHz, 48kHz, 96kHz, 192kHz, etc.)
- **Channels**: Mono, stereo
- **Streaming**: Full HTTP/HTTPS support
- **Quality**: Lossless compression

### MP3 (MPEG Audio Layer 3)
- **Bit depths**: 16-bit (decoded)
- **Sample rates**: Variable (typically 44.1kHz, 48kHz)
- **Channels**: Stereo (always decoded as stereo by library)
- **Streaming**: Full HTTP/HTTPS support
- **Quality**: Lossy compression

### WAV (Waveform Audio File Format)
- **Bit depths**: 16, 24, 32-bit
- **Sample rates**: Any
- **Channels**: Mono, stereo
- **Streaming**: ❌ Not supported (requires seekable file)
- **Quality**: Uncompressed PCM

### OGG Vorbis
- **Bit depths**: Float (decoded to 16-bit equivalent)
- **Sample rates**: Variable
- **Channels**: Mono, stereo, multi-channel
- **Streaming**: Full HTTP/HTTPS support
- **Quality**: Lossy, higher quality than MP3 at same bitrate

## Usage Examples

### Mixed Format Playlist

```bash
pwplay-player song1.flac song2.mp3 song3.wav song4.ogg
```

### From URLs

```bash
pwplay-server http://example.com/track.mp3 local.flac http://example.com/song.ogg
```

### Interactive Player

```bash
pwplay-player *.flac *.mp3 *.ogg
```

## Automatic Format Detection

The player automatically detects formats based on:

1. **File extension** (.flac, .mp3, .wav, .ogg)
2. **Content-Type header** (for HTTP URLs)

```go
// All handled automatically
player.OpenAudioFile("song.mp3")      // Detected as MP3
player.OpenAudioFile("song.flac")     // Detected as FLAC
player.OpenAudioFile("http://...")    // Detected from URL/headers
```

## Technical Implementation

### Unified Decoder Interface

```go
type AudioDecoder interface {
    SampleRate() int
    Channels() int
    BitsPerSample() int
    ReadSamples(buf []float32) (int, error)
    Close() error
}
```

All formats implement this interface and convert to float32 internally.

### Libraries Used

- **FLAC**: github.com/mewkiz/flac
- **MP3**: github.com/hajimehoshi/go-mp3
- **WAV**: github.com/go-audio/wav
- **OGG**: github.com/jfreymuth/oggvorbis

## Testing

```bash
# Create test files
ffmpeg -i input.mp3 test.mp3
ffmpeg -i input.wav test.wav
ffmpeg -i input.flac test.flac
ffmpeg -i input.ogg test.ogg

# Test playback
pwplay-player test.flac test.mp3 test.wav test.ogg
```

Output:
```
2026/03/13 14:32:43 Playlist: 4 tracks
2026/03/13 14:32:43 Preloaded next track: test.mp3
2026/03/13 14:32:43 Gapless transition to track 2
2026/03/13 14:32:45 Now playing [3/4]: test.wav
2026/03/13 14:32:45 Preloaded next track: test.ogg
2026/03/13 14:32:45 Gapless transition to track 4
✓ All formats play perfectly
```

## Gapless Playback

Gapless works across **all formats**:

```
FLAC → MP3 → WAV → OGG → FLAC (seamless)
```

The player preloads the next track (regardless of format) and transitions without gaps.

## Limitations

### WAV Streaming
WAV files cannot be streamed from HTTP URLs because the decoder requires seeking capability. WAV files must be local.

**Workaround**: Convert WAV to FLAC or OGG for streaming:
```bash
ffmpeg -i file.wav file.flac  # Lossless
ffmpeg -i file.wav file.ogg   # Compressed
```

### MP3 Channel Count
The go-mp3 library always decodes to stereo, even if the source is mono.

## Quality Comparison

| Format | Compression | Quality | File Size (3min) |
|--------|-------------|---------|------------------|
| WAV    | None        | Perfect | ~30 MB |
| FLAC   | Lossless    | Perfect | ~15 MB |
| OGG    | Lossy       | Excellent | ~3 MB |
| MP3    | Lossy       | Good    | ~3 MB |

## Conversion Examples

```bash
# To FLAC (lossless)
ffmpeg -i input.mp3 -c:a flac output.flac

# To MP3 (lossy)
ffmpeg -i input.wav -c:a libmp3lame -b:a 320k output.mp3

# To OGG (lossy, high quality)
ffmpeg -i input.flac -c:a libvorbis -q:a 8 output.ogg

# To WAV (uncompressed)
ffmpeg -i input.mp3 output.wav
```

## Adding New Formats

To add support for a new format:

1. Find a Go decoder library
2. Implement the `AudioDecoder` interface
3. Add format detection in `OpenAudioFile()`
4. Update `openByExtension()` switch statement

Example for AAC:
```go
case ".m4a", ".aac":
    return newAACDecoder(r)
```
