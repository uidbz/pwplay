# Usage Guide

## Applications

The project includes three applications for different use cases:

### 1. Simple Playlist (`simple_playlist`)

Auto-playing playlist that starts playback immediately.

**Features:**
- Automatic playback start
- Gapless transitions
- No user interaction required
- Perfect for scripting

**Usage:**
```bash
# Play individual files
pwplay-player song1.flac song2.mp3 song3.ogg

# Play entire directory (recursive)
pwplay-player /path/to/music/

# Mix files and directories
pwplay-player intro.flac /path/to/album/ outro.mp3
```

**Example:**
```bash
$ pwplay-player ~/Music/Jazz/
2026/03/13 17:00:00 Playlist: 15 tracks
2026/03/13 17:00:00 Playing... Press Ctrl+C to stop
2026/03/13 17:00:00 Preloaded next track: 02-song.flac
2026/03/13 17:00:03 Gapless transition to track 2
...
```

### 2. Interactive Playlist (`pwplay-player`)

Keyboard-controlled player with manual controls.

**Features:**
- Starts paused, waits for user input
- Keyboard controls for playback
- Track information display
- Full playlist navigation

**Usage:**
```bash
pwplay-player /path/to/music/
```

**Controls:**
| Key | Action |
|-----|--------|
| `space` | Play/Pause |
| `n` | Next track |
| `p` | Previous track |
| `s` | Stop |
| `i` | Track info |
| `h` or `?` | Help |
| `q` | Quit |

**Example Session:**
```bash
$ pwplay-player ~/Music/
2026/03/13 17:00:00 Playlist: 5 tracks
2026/03/13 17:00:00   [1] 01-intro.flac
2026/03/13 17:00:00   [2] 02-verse.mp3
2026/03/13 17:00:00   [3] 03-chorus.wav
2026/03/13 17:00:00   [4] 04-bridge.ogg
2026/03/13 17:00:00   [5] 05-outro.flac
2026/03/13 17:00:00 Ready. Press 'space' to start playback

> [press space to start]
2026/03/13 17:00:05 ▶  Playing

> [press i for info]
Track: [1/5] 01-intro.flac
Status: Playing

> [press n for next]
2026/03/13 17:00:10 Now playing [2/5]: 02-verse.mp3
```

### 3. Web Service (`pwplay-server`)

REST API for remote control via HTTP.

**Features:**
- Full playback control via HTTP
- JSON responses
- Add/remove tracks dynamically
- Status monitoring
- HTTP URL streaming support

**Usage:**
```bash
# Start service on default port 8080
pwplay-server /path/to/music/

# Or specify port
PORT=3000 pwplay-server /path/to/music/
```

**API Endpoints:**

| Method | Endpoint | Description | Response |
|--------|----------|-------------|----------|
| GET | `/play` | Start playback | `{"status": "playing"}` |
| GET | `/pause` | Pause playback | `{"status": "paused"}` |
| GET | `/stop` | Stop playback | `{"status": "stopped"}` |
| GET | `/next` | Next track | `{"status": "next"}` |
| GET | `/previous` | Previous track | `{"status": "previous"}` |
| GET | `/status` | Get player status | See below |
| POST | `/add?url=<file>` | Add track | `{"status": "added"}` |
| POST | `/remove?index=<n>` | Remove track | `{"status": "removed"}` |

**Status Response:**
```json
{
  "playing": true,
  "paused": false,
  "stopped": false,
  "current_track": 2,
  "total_tracks": 5,
  "current_file": "02-verse.mp3"
}
```

**Example Commands:**
```bash
# Start playback
curl http://localhost:8080/play

# Get status
curl http://localhost:8080/status

# Skip to next track
curl http://localhost:8080/next

# Add HTTP stream
curl -X POST "http://localhost:8080/add?url=https://example.com/stream.mp3"

# Add local file
curl -X POST "http://localhost:8080/add?url=/path/to/song.flac"

# Remove track 3
curl -X POST "http://localhost:8080/remove?index=2"
```

## Directory Playback

All applications support directory playback with recursive scanning.

**Supported Formats:**
- `.flac` - FLAC (Free Lossless Audio Codec)
- `.mp3` - MP3 (MPEG Audio Layer 3)
- `.wav` - WAV (Waveform Audio)
- `.ogg` - OGG Vorbis

**Features:**
- ✅ Recursive directory scanning
- ✅ Automatic file sorting (alphabetical)
- ✅ Mixed files and directories
- ✅ Case-insensitive extension matching
- ✅ Non-audio files automatically ignored

**Examples:**

```bash
# Play all music in a directory tree
pwplay-player ~/Music/

# Play specific album and additional tracks
pwplay-player ~/Music/Albums/2024/ bonus-track.mp3

# Multiple directories
pwplay-player ~/Music/Jazz/ ~/Music/Classical/

# Mix it all
pwplay-player intro.flac ~/Music/Album/ ~/Downloads/single.mp3
```

**Directory Structure Example:**
```
Music/
├── Album1/
│   ├── 01-song.flac
│   ├── 02-song.flac
│   └── cover.jpg          # Ignored
├── Album2/
│   └── Bonus/
│       └── 01-bonus.mp3
└── single.ogg

$ pwplay-player Music/
Playlist: 4 tracks
  - Music/Album1/01-song.flac
  - Music/Album1/02-song.flac
  - Music/Album2/Bonus/01-bonus.mp3
  - Music/single.ogg
```

## HTTP Streaming

The webservice supports streaming from HTTP/HTTPS URLs.

**Usage:**
```bash
# Add HTTP stream to playlist
curl -X POST "http://localhost:8080/add?url=https://example.com/music/song.mp3"

# Or start with HTTP URL directly
pwplay-player https://example.com/stream.mp3 local-file.flac
```

**Supported:**
- ✅ HTTP and HTTPS URLs
- ✅ FLAC, MP3, OGG formats
- ✅ Mixed with local files

**Not Supported:**
- ❌ WAV over HTTP (requires seekable file)

## Gapless Playback

All applications feature intelligent preloading for truly gapless playback.

**How It Works:**
1. Current track plays from ring buffer
2. When buffer drops below 50%, next track is preloaded
3. At end of track, seamless transition to preloaded track
4. Works across different formats (FLAC → MP3 → WAV → OGG)

**Example:**
```bash
$ pwplay-player album/
2026/03/13 17:00:00 Playing... Press Ctrl+C to stop
2026/03/13 17:00:00 Preloaded next track: 02-track.mp3
2026/03/13 17:00:03 Gapless transition to track 2    # Zero gap!
2026/03/13 17:00:06 Now playing [3/5]: 03-track.wav
2026/03/13 17:00:06 Preloaded next track: 04-track.ogg
2026/03/13 17:00:06 Gapless transition to track 4    # Zero gap!
```

## Performance

The lock-free ring buffer provides excellent real-time performance:

- **Callback time:** <0.1ms (consistent)
- **Buffer overhead:** ~350KB per player
- **CPU usage:** Minimal (<1% on modern systems)
- **Headroom:** 2000x real-time requirements

See [BENCHMARKS.md](BENCHMARKS.md) for detailed performance analysis.

## Troubleshooting

### "No audio files found"

The directory doesn't contain any supported audio files.

**Solution:** Verify files have correct extensions (.flac, .mp3, .wav, .ogg)

### "Unsupported file format"

Trying to play a file with unsupported format.

**Solution:** Convert file to supported format or use directory playback (unsupported files are ignored)

### "Cannot access directory"

Permission or path issue.

**Solution:** Check path exists and is readable:
```bash
ls -la /path/to/music/
```

### No audio output

**Check PipeWire status:**
```bash
systemctl --user status pipewire
```

**Verify routing:**
```bash
pw-cli list-objects | grep -i stream
```

### Choppy playback

Very unlikely with lock-free implementation, but if it occurs:

1. Check system load: `htop`
2. Verify no CPU throttling
3. Check for other audio applications interfering

## Scripting

### Automate Playback

```bash
#!/bin/bash
# Play random album
ALBUM=$(find ~/Music -type d -maxdepth 2 | shuf -n 1)
pwplay-player "$ALBUM"
```

### Web API Integration

```bash
#!/bin/bash
# Web API control script

BASE_URL="http://localhost:8080"

play() {
    curl -s "$BASE_URL/play"
}

pause() {
    curl -s "$BASE_URL/pause"
}

next() {
    curl -s "$BASE_URL/next"
}

status() {
    curl -s "$BASE_URL/status" | jq .
}

# Usage
case "$1" in
    play) play ;;
    pause) pause ;;
    next) next ;;
    status) status ;;
    *) echo "Usage: $0 {play|pause|next|status}" ;;
esac
```

## Advanced Usage

### Shuffle Playback

```bash
# Shuffle files before playing
find ~/Music -name "*.flac" -o -name "*.mp3" | shuf | xargs pwplay-player
```

### Filter by Genre

```bash
# Play only jazz
pwplay-player ~/Music/Jazz/
```

### Queue Management

```bash
# Using web service, dynamically add tracks
for file in ~/Music/NewAlbum/*.flac; do
    curl -X POST "http://localhost:8080/add?url=$file"
done
```

### Remote Control

```bash
# Control player from another machine
ssh music-server "curl http://localhost:8080/play"
```
