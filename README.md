# pwplay

Audio player for Linux built on PipeWire, written in Go.

## Features

- Multi-format: FLAC, MP3, WAV, OGG
- Gapless playback with intelligent preloading
- Seek support (all formats)
- HTTP/HTTPS URL streaming (downloaded to temp file for reliable playback)
- Client/server architecture with REST API
- Interactive TUI remote client
- Interactive local player with keyboard controls
- Lock-free ring buffer for real-time audio
- Directory scanning (recursive)

## Project Structure

```
cmd/
  server/       pwplay-server   REST API audio server
  client/       pwplay-client   Interactive TUI remote client
  player/       pwplay-player   Interactive local player
client/         Go client library for the REST API
player/         Core player engine and multi-format decoders
pipewire/       PipeWire C bindings via cgo
examples/       Standalone demo programs
```

## Prerequisites

PipeWire development libraries and Go 1.22+.

```bash
# Ubuntu/Debian
sudo apt install libpipewire-0.3-dev pkg-config

# Fedora
sudo dnf install pipewire-devel pkg-config

# Arch
sudo pacman -S pipewire pkg-config
```

## Building

```bash
make build
```

Produces: `pwplay-server`, `pwplay-client`, `pwplay-player`

## Usage

### Server

Start the audio server with files, directories, or URLs:

```bash
pwplay-server ~/Music/album
pwplay-server track1.flac track2.mp3 ~/Music/jazz/
pwplay-server http://example.com/music.flac local.ogg
```

Starts paused on `:8080`. Control via the REST API or the TUI client.
See [WEBSERVICE.md](WEBSERVICE.md) for the full API reference.

### TUI Client

Connect to a running server:

```bash
pwplay-client              # connects to localhost:8080
pwplay-client myhost:8080  # remote server
```

Single-keypress controls with live status display, progress bar, and playlist view.
See [CLIENT.md](CLIENT.md) for details.

### Interactive Local Player

Play audio directly (no server needed):

```bash
pwplay-player ~/Music/album
```

Controls: `space` play/pause, `n`/`p` next/prev, `f`/`b` seek +/-10s, `s` stop, `i` info, `q` quit.

## Go Packages

### player

Core playback engine.

```go
import "git.sr.ht/~uid/pwplay/player"

files, _ := player.ExpandPlaylist([]string{"~/Music"})
p, _ := player.NewPlayer(files, true)
p.Play()
p.SeekRelative(30)
fmt.Println(p.Position(), p.TrackDuration())
```

### client

HTTP client library for the server API.

```go
import "git.sr.ht/~uid/pwplay/client"

c := client.New("http://localhost:8080")
c.Play()
c.SeekRelative(-10)
s, _ := c.Status()
```

### pipewire

Low-level PipeWire bindings.

```go
import "git.sr.ht/~uid/pwplay/pipewire"

pipewire.Init()
defer pipewire.Deinit()

stream, _ := pipewire.NewStream("My App", format, callback)
stream.Connect(format)
```

## Documentation

- [WEBSERVICE.md](WEBSERVICE.md) - REST API reference
- [CLIENT.md](CLIENT.md) - Client library and TUI client
- [FORMATS.md](FORMATS.md) - Supported audio formats
- [INSTALL.md](INSTALL.md) - Detailed installation guide
- [BENCHMARKS.md](BENCHMARKS.md) - Performance benchmarks
- [CHANGELOG.md](CHANGELOG.md) - Version history

## License

MIT
