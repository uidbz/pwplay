# Web Service API

REST API for controlling the pwplay audio server.

## Features

- Play/Pause/Stop controls
- Next/Previous track navigation and direct track jump
- Seek (absolute and relative)
- Volume control
- Add/Remove/Move tracks dynamically
- Track metadata and album cover endpoints
- Directory scanning (recursive)
- HTTP URL playback
- Gapless playback
- Multi-format: FLAC, MP3, WAV, OGG, OPUS
- Passthrough mode (no resampling, no remixing, no software volume)
- Optional exclusive device access

## Starting the Service

```bash
pwplay-server ~/Music/album
pwplay-server ~/Music/jazz ~/Music/classical
pwplay-server track1.flac track2.mp3 http://example.com/music.flac
pwplay-server            # empty queue, controlled entirely over HTTP
```

Directories are scanned recursively for supported audio files. Files are sorted alphabetically. Server starts on `http://localhost:8080` in a paused state.

With no arguments the server starts with an empty queue and is driven entirely
via the REST API: enqueue files, directories, or URLs with `POST /add`, then
use `/play` or `/goto` to start playback.

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

See the [Passthrough and Exclusive Modes](../README.md#passthrough-and-exclusive-modes) section in the README for details.

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
  "passthrough": false,
  "currentTrack": 0,
  "currentFile": "track1.flac",
  "playlist": ["track1.flac", "track2.flac", "track3.flac"],
  "totalTracks": 3,
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

Stops playback and rewinds to the start of the current track.

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

### POST /goto

Jump to and play the track at the given 0-based playlist index.

```bash
curl -X POST http://localhost:8080/goto \
  -H "Content-Type: application/json" \
  -d '{"index": 3}'
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

```json
{
  "status": "seeked",
  "position": 32.5
}
```

### POST /volume

Set playback volume (0.0 = silent, 1.0 = default, 2.0 = max). Ignored in passthrough mode.

```bash
curl -X POST http://localhost:8080/volume \
  -H "Content-Type: application/json" \
  -d '{"volume": 0.8}'
```

### POST /add

Add files, directories, or URLs to the playlist. Accepts a `paths` array. Directories are expanded recursively on the server.

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

Remove track by 0-based index. Removing the current track stops and restarts playback at the track that takes its place.

```bash
curl -X POST http://localhost:8080/remove \
  -H "Content-Type: application/json" \
  -d '{"index": 2}'
```

### POST /move

Move a range of playlist items to a new position. The currently playing track keeps playing and follows the move.

```bash
# Move items at indices 5,6,7 to start at index 0
curl -X POST http://localhost:8080/move \
  -H "Content-Type: application/json" \
  -d '{"from": 5, "count": 3, "to": 0}'
```

### GET /metadata

Metadata for the current track (tags such as title, artist, album, track number). Returns 404 if unavailable.

```bash
curl http://localhost:8080/metadata
```

```json
{
  "title": "Song Title",
  "album": "Album Name",
  "artist": "Artist",
  "albumArtist": "Artist",
  "composer": "",
  "genre": "Jazz",
  "year": 2024,
  "track": 3,
  "trackTotal": 12,
  "disc": 1,
  "discTotal": 1,
  "lyrics": "",
  "comment": "",
  "format": "FLAC",
  "hasPicture": true
}
```

### GET /playlist-metadata

Metadata for all tracks in the playlist, as a JSON array.

```bash
curl http://localhost:8080/playlist-metadata
```

### GET /cover

Album cover image of the current track. The `Content-Type` header carries the image MIME type (e.g. `image/jpeg`). Returns 404 if the track has no embedded cover.

```bash
curl -OJ http://localhost:8080/cover
```

### GET /history

Finished plays recorded by the player (the engine's `PlayerOptions.OnPlay`
hook): the entry heard, the seconds actually heard (`listened` — seeks skip
nothing forward and replay nothing twice, pauses add nothing), the track
length and where playback left it. Plays shorter than one second are not
recorded. The log is persisted as JSON lines at `-history`
(default `$XDG_STATE_HOME/pwplay/history.jsonl`; `-history off` keeps it in
memory) and holds the last 10 000 plays.

`since` returns only records with a larger sequence number. Sequence numbers
are unique per `source` — an identity minted when the log file is created —
so a consumer de-duplicates on (source, seq) and resumes from `lastSeq`.

```bash
curl 'http://localhost:8080/history?since=41'
```

```json
{
  "source": "pwplay@host/1a2b3c4d",
  "lastSeq": 42,
  "records": [
    {"seq": 42, "url": "http://filehost/abc…", "startedAt": "2026-10-11T17:00:00Z",
     "endedAt": "2026-10-11T17:04:10Z", "listened": 248.3, "duration": 250.1, "position": 250.1}
  ]
}
```

## Build

```bash
make build
# or
go build -o pwplay-server ./cmd/server
```

## Notes

- The service starts paused; use `/play` to begin
- The audio format (sample rate, channels) for the whole session is set by the first track loaded — the first startup argument, or, when started with an empty queue, the first track added via `/add`
- HTTP URLs are downloaded to a temp file before playback for reliability and seek support
- Seek is immediate with no audible gap
- Directories are scanned recursively for `.flac`, `.mp3`, `.wav`, `.ogg`, `.opus`
- In passthrough mode, `/volume` is a no-op (gain fixed at 1.0)
