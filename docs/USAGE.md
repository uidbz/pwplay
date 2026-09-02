# Usage Guide

The project provides three applications, all sharing the same player engine.

## pwplay-player — Interactive Local Player

Plays audio directly with keyboard controls; no server needed. Starts paused.

```bash
pwplay-player ~/Music/album
pwplay-player song1.flac song2.mp3 song3.ogg
pwplay-player intro.flac ~/Music/Jazz/ outro.mp3
```

### Controls

| Key | Action |
|-----|--------|
| `space` | Play/Pause |
| `n` / `p` | Next / Previous track |
| `f` / `+` | Seek forward 10s |
| `b` / `-` | Seek backward 10s |
| `v` / `V` | Volume down / up (5%) |
| `m` | Mute / unmute |
| `s` | Stop |
| `i` | Track info |
| `h` / `?` | Help |
| `q` | Quit |

Both apps accept `-passthrough` and `-exclusive` flags; see the
[Passthrough and Exclusive Modes](../README.md#passthrough-and-exclusive-modes)
section of the README.

## pwplay-server — REST API Server

Remote-controllable audio server. Starts paused on `:8080`.

```bash
pwplay-server ~/Music/album
pwplay-server track1.flac http://example.com/music.flac
```

### Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/status` | Player status, position, playlist |
| POST | `/play` | Start playback |
| POST | `/pause` | Pause playback |
| POST | `/stop` | Stop playback (rewinds to start of track) |
| POST | `/next` | Next track |
| POST | `/previous` | Previous track |
| POST | `/goto` | Jump to playlist index — `{"index": 3}` |
| POST | `/seek` | Seek — `{"position": 30.5}` or `{"relative": -10}` |
| POST | `/volume` | Set volume — `{"volume": 0.8}` (0.0–2.0) |
| POST | `/add` | Add tracks — `{"paths": ["..."]}` |
| POST | `/remove` | Remove track — `{"index": 2}` |
| POST | `/move` | Move items — `{"from": 5, "count": 3, "to": 0}` |
| GET | `/metadata` | Metadata for current track |
| GET | `/playlist-metadata` | Metadata for all tracks |
| GET | `/cover` | Album cover image of current track |

See [WEBSERVICE.md](WEBSERVICE.md) for the full request/response reference.

### Example

```bash
curl -X POST http://localhost:8080/play
curl http://localhost:8080/status
curl -X POST http://localhost:8080/seek \
  -H "Content-Type: application/json" -d '{"relative": -10}'
curl -X POST http://localhost:8080/add \
  -H "Content-Type: application/json" -d '{"paths": ["~/Music/NewAlbum/"]}'
```

## pwplay-client — Interactive TUI Client

Connects to a running server:

```bash
pwplay-client              # connects to localhost:8080
pwplay-client myhost:8080  # remote server
```

See [CLIENT.md](CLIENT.md) for the key reference.

## Directory Playback

All applications accept directories, which are scanned recursively for
`.flac`, `.mp3`, `.wav`, and `.ogg` files (case-insensitive). Files are
sorted alphabetically; non-audio files are ignored. Files and directories
can be mixed freely:

```bash
pwplay-player intro.flac ~/Music/Album/ ~/Downloads/single.mp3
```

## HTTP Streaming

The server and player accept HTTP/HTTPS URLs. URLs are downloaded to a
temporary file before playback, which provides reliable, seekable playback
without holding the connection open. All formats, including WAV, work
over HTTP.

## Gapless Playback

The next track is decoded into the lock-free ring buffer while the current
one is still playing, so track transitions have no audible gap — including
across different formats (FLAC → MP3 → WAV → OGG).

## Scripting

```bash
#!/bin/bash
# Web API control script
BASE_URL="http://localhost:8080"

case "$1" in
    play)   curl -s -X POST "$BASE_URL/play" ;;
    pause)  curl -s -X POST "$BASE_URL/pause" ;;
    next)   curl -s -X POST "$BASE_URL/next" ;;
    status) curl -s "$BASE_URL/status" | jq . ;;
    *) echo "Usage: $0 {play|pause|next|status}" ;;
esac
```

Remote control from another machine:

```bash
ssh music-server "curl -X POST http://localhost:8080/play"
```

## Troubleshooting

**"No audio files found"** — the directory contains no supported audio
files. Verify extensions are `.flac`, `.mp3`, `.wav`, or `.ogg`.

**No audio output** — check that PipeWire is running:

```bash
systemctl --user status pipewire
pw-cli list-objects | grep -i stream
pactl list sinks
```

**Silence with `-exclusive`** — the session manager could not grant
exclusive access. Try again with `-passthrough` only.

**Choppy playback** — unlikely with the lock-free design, but if it occurs,
check system load and CPU throttling.
