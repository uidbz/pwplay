# pwplay

Audio player for Linux built on PipeWire, written in Go.

## Features

- Multi-format: FLAC, MP3, WAV, OGG Vorbis, Opus
- Gapless playback with next-track preloading
- Seek support (all formats)
- Software volume control
- Passthrough mode (no resampling, no remixing, no software volume)
- Optional exclusive device access
- HTTP/HTTPS URL playback (downloaded to a temp file first)
- Client/server architecture with REST API
- Interactive TUI remote client
- Interactive local player with keyboard controls
- Lock-free ring buffer for real-time audio
- Recursive directory scanning

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

PipeWire development libraries and Go 1.24+.

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

To run `pwplay-server` as a per-user service (systemd or OpenRC) controlled
over HTTP:

```bash
sudo make install
```

See [contrib/README.md](contrib/README.md).

## Usage

### Server

Start the audio server with files, directories, or URLs:

```bash
pwplay-server ~/Music/album
pwplay-server track1.flac track2.mp3 ~/Music/jazz/
pwplay-server http://example.com/music.flac local.ogg
pwplay-server   # empty queue, controlled entirely over HTTP (POST /add)
```

Starts paused on `:8080`. Control via the REST API or the TUI client.
See [docs/WEBSERVICE.md](docs/WEBSERVICE.md) for the full API reference.

### TUI Client

Connect to a running server:

```bash
pwplay-client              # connects to localhost:8080
pwplay-client myhost:8080  # remote server
```

Single-keypress controls with live status display, progress bar, and playlist view.
See [docs/CLIENT.md](docs/CLIENT.md) for details.

### Interactive Local Player

Play audio directly (no server needed):

```bash
pwplay-player ~/Music/album
```

Controls: `space` play/pause, `n`/`p` next/prev, `f`/`b` seek ±10s, `v`/`V` vol down/up, `m` mute, `s` stop, `i` info, `q` quit.

## Passthrough and Exclusive Modes

pwplay supports two flags for high-quality audio output. They can be used independently or together.

### `-passthrough`

Optimized for audio quality. Disables all signal processing in pwplay and requests PipeWire run the audio device at the stream's native sample rate.

What it does:
- Disables software volume control (gain is always 1.0)
- Sets `stream.dont-remix` to prevent PipeWire channel remixing
- Sets `node.rate` to request the device match the file's sample rate

What it does NOT do:
- Does not prevent PipeWire from converting float32 to the DAC's native integer format (this is unavoidable since PipeWire's internal format is F32)
- Does not prevent other applications from playing simultaneously (their audio will be mixed at the sink)

```bash
pwplay-server -passthrough ~/Music/album
pwplay-player -passthrough ~/Music/album
```

### `-exclusive`

Requests sole access to the audio sink device. PipeWire's session manager (WirePlumber) will disconnect other streams from the device while this stream is active.

```bash
pwplay-server -exclusive ~/Music/album
pwplay-server -passthrough -exclusive ~/Music/album
```

**Note:** `-exclusive` may cause silence if the session manager cannot grant exclusive access (e.g. another application holds the device, or the desktop environment's audio session cannot be disconnected). If you get no sound with `-exclusive`, try without it -- `-passthrough` alone provides the audio quality benefits.

### Recommended Combinations

| Use case | Flags | Notes |
|---|---|---|
| Normal playback | *(none)* | Software volume, PipeWire handles resampling/mixing |
| High-quality | `-passthrough` | No software processing, native sample rate |
| Audiophile | `-passthrough -exclusive` | No processing, no mixing with other apps |
| Exclusive only | `-exclusive` | Volume control works, sole device access |

## Go Packages

### player

Core playback engine.

```go
import "github.com/uidbz/pwplay/player"

files, _ := player.ExpandPlaylist([]string{"~/Music"})
p, _ := player.NewPlayer(files, true)
p.Play()
p.SeekRelative(30)
p.SetVolume(0.8)
fmt.Println(p.Position(), p.TrackDuration())
```

For passthrough/exclusive:

```go
opts := player.PlayerOptions{
    StartPaused: true,
    Passthrough: true,
    Exclusive:   true,
}
p, _ := player.NewPlayerWithOptions(files, opts)
```

### client

HTTP client library for the server API.

```go
import "github.com/uidbz/pwplay/client"

c := client.New("http://localhost:8080")
c.Play()
c.SeekRelative(-10)
c.SetVolume(0.8)
s, _ := c.Status()
```

### pipewire

Low-level PipeWire bindings.

```go
import "github.com/uidbz/pwplay/pipewire"

pipewire.Init()
defer pipewire.Deinit()

stream, _ := pipewire.NewStreamWithOptions("My App", format, callback,
    pipewire.StreamOptions{Passthrough: true, Exclusive: true})
stream.Connect(format)
```

## Documentation

- [docs/WEBSERVICE.md](docs/WEBSERVICE.md) - REST API reference
- [docs/CLIENT.md](docs/CLIENT.md) - Client library and TUI client
- [docs/USAGE.md](docs/USAGE.md) - Using the applications
- [docs/FORMATS.md](docs/FORMATS.md) - Supported audio formats
- [docs/INSTALL.md](docs/INSTALL.md) - Detailed installation guide
- [docs/QUICKSTART.md](docs/QUICKSTART.md) - Quick start
- [docs/EXAMPLES.md](docs/EXAMPLES.md) - Example programs
- [docs/BENCHMARKS.md](docs/BENCHMARKS.md) - Performance benchmarks
- [CHANGELOG.md](CHANGELOG.md) - Version history

## License

MIT
