# Web Service API

REST API for controlling the pwplay audio server.

## Features

- Play/Pause/Stop controls
- Next/Previous track navigation
- Seek (absolute and relative)
- Volume control
- Add/Remove/Move tracks dynamically
- Directory scanning (recursive)
- HTTP URL streaming
- Gapless playback
- Multi-format: FLAC, MP3, WAV, OGG
- Passthrough mode (no resampling, no remixing, no software volume)
- Optional exclusive device access

## Starting the Service

```bash
pwplay-server ~/Music/album
pwplay-server ~/Music/jazz ~/Music/classical
pwplay-server track1.flac track2.mp3 http://example.com/music.flac
```

Directories are scanned recursively for supported audio files. Files are sorted alphabetically. Server starts on `http://localhost:8080` in a paused state.

### Flags

```
-passthrough    Disable software volume, prevent resampling and channel remixing
-exclusive      Request exclusive access to the audio device (use with -passthrough)
```

```bash
# High-quality output
pwplay-server -passthrough ~/Music/album

# Audiophile mode
pwplay-server -passthrough -exclusive ~/Music/album
```

See the [Passthrough and Exclusive Modes](README.md#passthrough-and-exclusive-modes) section in the README for details.

## API Endpoints

### GET /status

```bash
curl http://localhost:8080/status
```

```json
{
  "playing": true,
  "paused": false,
  "stopped": false,
  "currentTrack": 0,
  "currentFile": "track1.flac",
  "totalTracks": 3,
  "playlist": ["track1.flac", "track2.flac", "track3.flac"],
  "position": 42.5,
  "trackDuration": 180.0,
  "volume": 1.0
}
```

### POST /play

```bash
curl -X POST http://localhost:8080/play
```

### POST /pause

```bash
curl -X POST http://localhost:8080/pause
```

### POST /stop

```bash
curl -X POST http://localhost:8080/stop
```

### POST /next

```bash
curl -X POST http://localhost:8080/next
```

### POST /previous

```bash
curl -X POST http://localhost:8080/previous
```

### POST /seek

Absolute or relative seek in seconds.

```bash
# Absolute
curl -X POST http://localhost:8080/seek \
  -H "Content-Type: application/json" \
  -d '{"position": 30.5}'

# Relative
curl -X POST http://localhost:8080/seek \
  -H "Content-Type: application/json" \
  -d '{"relative": -10}'
```

### POST /volume

Set playback volume (0.0 = silent, 1.0 = default, 2.0 = max). Ignored in passthrough mode.

```bash
curl -X POST http://localhost:8080/volume \
  -H "Content-Type: application/json" \
  -d '{"volume": 0.8}'
```

### POST /add

Add files, directories, or URLs to the playlist. Accepts a `paths` array.

```bash
curl -X POST http://localhost:8080/add \
  -H "Content-Type: application/json" \
  -d '{"paths": ["/path/to/track.flac", "/path/to/album", "http://example.com/music.ogg"]}'
```

```json
{
  "status": "added",
  "tracksAdded": 12
}
```

### POST /remove

Remove track by 0-based index.

```bash
curl -X POST http://localhost:8080/remove \
  -H "Content-Type: application/json" \
  -d '{"Index": 2}'
```

### POST /move

Move a range of playlist items to a new position.

```bash
# Move items at indices 5,6,7 to start at index 0
curl -X POST http://localhost:8080/move \
  -H "Content-Type: application/json" \
  -d '{"from": 5, "count": 3, "to": 0}'
```

## Build

```bash
make build
# or
go build -o pwplay-server ./cmd/server
```

## Notes

- The service starts paused; use `/play` to begin
- First track determines audio format (sample rate, channels)
- HTTP URLs are downloaded to a temp file before playback for reliability and seek support
- Seek is immediate with no audible gap
- Directories are scanned recursively for `.flac`, `.mp3`, `.wav`, `.ogg`
- In passthrough mode, `/volume` is a no-op (gain fixed at 1.0)
