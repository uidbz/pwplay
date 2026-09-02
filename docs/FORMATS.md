# Supported Audio Formats

The player supports multiple audio formats through a unified decoder interface.

## Supported Formats

| Format | Extension | HTTP/HTTPS | Notes |
|--------|-----------|------------|-------|
| FLAC | .flac | Yes | Lossless, 16/24/32-bit |
| MP3 | .mp3 | Yes | Lossy |
| WAV | .wav | Yes | Uncompressed PCM |
| OGG Vorbis | .ogg | Yes | Lossy, variable bitrate |

All formats support seeking and work over HTTP/HTTPS. URLs are downloaded
to a temporary file before playback, so even WAV (which requires a seekable
file) can be streamed.

## Format Details

### FLAC (Free Lossless Audio Codec)
- **Bit depths**: 16, 24, 32-bit
- **Sample rates**: Any (44.1kHz, 48kHz, 96kHz, 192kHz, etc.)
- **Channels**: Mono, stereo
- **Quality**: Lossless compression

### MP3 (MPEG Audio Layer 3)
- **Bit depths**: 16-bit (decoded)
- **Sample rates**: Variable (typically 44.1kHz, 48kHz)
- **Channels**: Decoded as stereo by the go-mp3 library; the player adapts
  the output to the MP3's channel count, so mixed-format playlists with
  mono and stereo files play correctly.
- **Quality**: Lossy compression

### WAV (Waveform Audio File Format)
- **Bit depths**: 16, 24, 32-bit
- **Sample rates**: Any
- **Channels**: Mono, stereo
- **Quality**: Uncompressed PCM

### OGG Vorbis
- **Sample rates**: Variable
- **Channels**: Mono, stereo, multi-channel
- **Quality**: Lossy, typically higher quality than MP3 at the same bitrate

## Usage

```bash
# Mixed format playlist
pwplay-player song1.flac song2.mp3 song3.wav song4.ogg

# From URLs (all formats work over HTTP)
pwplay-server http://example.com/track.mp3 local.flac http://example.com/song.wav
```

## Format Detection

Formats are detected by:

1. **File extension** (`.flac`, `.mp3`, `.wav`, `.ogg`, case-insensitive)
2. **Content-Type header** for HTTP URLs without a usable extension

All decoders convert samples to float32 internally, which is PipeWire's
native format. Integer-to-float conversion is lossless for 16 and 24-bit
sources (float32 provides 24 bits of mantissa precision and ~144 dB of
dynamic range).

## Decoder Libraries

- **FLAC**: github.com/mewkiz/flac
- **MP3**: github.com/hajimehoshi/go-mp3
- **WAV**: github.com/go-audio/wav
- **OGG**: github.com/jfreymuth/oggvorbis

## Gapless Playback

Gapless transitions work across all format combinations:

```
FLAC → MP3 → WAV → OGG (seamless)
```

The next track is preloaded while the current one plays. All tracks in a
playlist are converted to the output stream's channel count and sample
format; the output sample rate and channel layout are determined by the
first track.

## Session Format

The PipeWire stream is opened with the **first track's** sample rate and
channel count and stays fixed for the session. pwplay does not resample:
tracks with a different sample rate play at the wrong speed, and tracks
with a different channel count are misaligned. Keep a playlist's tracks at
one sample rate / channel layout for correct playback.

## Adding a New Format

1. Find a Go decoder library
2. Implement the `AudioDecoder` interface in `player/decoder.go`
3. Add the extension to the `openByExtension()` switch
4. Optionally add content-type detection in `openByContentType()`
