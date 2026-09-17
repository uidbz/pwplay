# Changelog

## Unreleased

### Pluggable Audio Sinks (Android support)

The `player` package's audio output is no longer hard-wired to PipeWire:
`PlayerOptions.Sink` accepts a `SinkFactory`, and nil selects the platform
default — PipeWire on Linux (unchanged behavior), OpenSL ES on Android. This
lets embedders (e.g. tie-audio) run the full engine — queue, gapless, seek,
volume, all decoders — on Android, where the pure-Go decoder stack compiles
unchanged and only the sink differs.

#### Changes
- New `player.Sink` interface (`Connect(Format)` / `Destroy()`), plus
  `Format`, `SinkOptions`, `ProcessCallback`, `SinkFactory` types
- `Player.PlayerOptions` gains a `Sink SinkFactory` field (test seam:
  a fake sink can drive the audio callback without a sound device)
- The PipeWire stream is created lazily through the factory, at the first
  track's native format, as before
- `pipewire` package files are now `//go:build linux && !android`
- Fixed `ClearTracks` (and remove-to-empty) leaving stale track boundaries
  behind: `CurrentTrack()` could report an index into the old playlist
  for an empty queue

### Opus Playback Support

Added Opus (Ogg Opus, RFC 7845) decoding via the pure-Go
[github.com/pion/opus](https://github.com/pion/opus) library (with its
bundled `pkg/oggreader` for the Ogg container).

#### Features
- `.opus` extension support for local files and HTTP/HTTPS URLs
- `.ogg` files are content-sniffed (OpusHead vs. Vorbis ID header), so
  Opus content with a `.ogg` extension plays correctly
- Always decoded at 48 kHz (Opus's native rate), mono and stereo
  (channel mapping family 0)
- Sample-exact duration and gapless transitions: encoder pre-skip and
  final-page padding are discarded using the OpusHead pre-skip field and
  the last page's granule position
- Seek support (decode-forward from the nearest packet boundary)
- OpusTags metadata works out of the box (via dhowden/tag)

#### Changes
- `go.mod` now requires Go 1.24 (pion/opus's minimum)

## v2.2.0 - 2026-03-13

### Directory Playback Support

Added ability to play directories with automatic recursive scanning for audio files.

#### Features
- Recursive directory scanning for audio files
- Automatic sorting of files for consistent playback order
- Support for mixing files and directories in a playlist
- Filters for supported formats (FLAC, MP3, WAV, OGG)
- Proper error handling for missing or empty directories

#### New API
- `ExpandPlaylist(paths []string) ([]string, error)` - Expands directories to audio files

#### Usage
```bash
# Play entire directory
pwplay-player /path/to/music/

# Mix files and directories
pwplay-player song.flac /path/to/album/
```

#### Changes
- Updated all applications to support directory arguments
- Added comprehensive test suite for playlist expansion
- Improved usage messages with format information

## v2.1.0 - 2026-03-13

### Lock-Free Ring Buffer

Implemented lock-free ring buffer using atomic operations for improved real-time performance.

#### Changes
- Replaced mutex-based ring buffer with lock-free atomic operations
- Eliminated lock contention in audio callback path
- Improved cache-line efficiency with uint64 atomic positions

#### Performance Benefits
- Zero mutex overhead in real-time audio callback
- Wait-free reads/writes for single producer/consumer
- Better CPU cache utilization
- Reduced latency and jitter

#### Technical Details
- Uses `sync/atomic` LoadUint64/StoreUint64 for position tracking
- Single producer (decoder thread) / single consumer (audio callback)
- Modulo arithmetic for circular buffer management
- Safe for concurrent access without locks

## v2.0.0 - 2026-03-13

### Major Refactoring

Complete refactoring with shared player implementation and multi-format support.

#### Changes
- Created shared `player.Player` struct used by all applications
- Reduced codebase from ~3000 to ~800 lines (73% reduction)
- Implemented gapless playback with intelligent preloading
- Added HTTP URL streaming support
- Added MP3, WAV, and OGG format support
- Three new streamlined applications

#### New Applications
- **simple_playlist**: Auto-playing playlist
- **pwplay-player**: Keyboard-controlled player
- **pwplay-server**: REST API with full control

#### Features
- Gapless playback with next-track preloading
- HTTP/HTTPS URL streaming
- Multi-format support (FLAC, MP3, WAV, OGG)
- 16/24/32-bit audio support (auto-converts to float32)
- All playback controls (play/pause/stop/next/previous)
- Dynamic playlist management (add/remove tracks)
- Thread-safe operations

#### Technical Details
- Unified `AudioDecoder` interface for all formats
- Single source of truth for player logic
- Preloading triggers when buffer < 50% full
- Seamless track transitions with zero gaps
- Proper cleanup on manual track changes

## v1.0.1 - 2026-03-12

### Performance Fix

Fixed choppy audio in FLAC player.

#### Changes
- Implemented ring buffer with separate decoder thread
- Removed I/O operations from real-time audio callback
- Added pre-buffering before playback starts
- Optimized callback to <0.1ms execution time

#### Results
- Smooth playback with no dropouts
- No buffer underruns during playback
- Fast, predictable callback timing
- Follows real-time audio best practices

See docs/BENCHMARKS.md for performance numbers.

## v1.0.0 - 2026-03-12

### Initial Release

Complete Go bindings for PipeWire with working examples.

#### Features
- Native cgo bindings to libpipewire-0.3
- Thread-safe stream management
- Real-time audio callback system
- Float32 audio format support
- Multi-channel audio support
- Automatic sample rate handling

#### Examples
- **FLAC Player**: Complete audio player supporting any FLAC file
- **Tone Generator**: Sine wave synthesizer with configurable frequency

#### Technical Details
- Proper listener and event allocation using spa_hook
- C helper functions to avoid Go pointer issues
- Safe memory management with proper cleanup
- Buffer management for smooth audio streaming

#### Fixed Issues
- Segmentation faults due to improper listener allocation
- Go pointer issues with SPA parameter building
- Memory leaks in stream destruction

#### Audio Format Support
- **Input**: FLAC files with 16, 24, or 32-bit samples (S16/S24/S32)
- **Internal**: Automatic conversion to float32 for PipeWire
- **Output**: Float32 to PipeWire (industry standard for audio APIs)

#### Known Limitations
- Playback only (no recording support yet)
- Limited to mono and stereo (multi-channel untested)

#### Dependencies
- libpipewire-0.3-dev
- pkg-config
- Go 1.22+
- github.com/mewkiz/flac v1.0.10 (for FLAC example)
