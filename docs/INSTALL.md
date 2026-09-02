# Installation Guide

## Prerequisites

### 1. Install PipeWire Development Libraries

**Ubuntu/Debian:**
```bash
sudo apt update
sudo apt install libpipewire-0.3-dev pkg-config build-essential
```

**Fedora:**
```bash
sudo dnf install pipewire-devel pkg-config gcc
```

**Arch Linux:**
```bash
sudo pacman -S pipewire pkg-config base-devel
```

### 2. Install Go

Make sure you have Go 1.22 or later installed. Check with:
```bash
go version
```

If you need to install Go, download it from https://go.dev/dl/

### 3. Verify PipeWire is Running

```bash
systemctl --user status pipewire
```

If it's not running:
```bash
systemctl --user start pipewire
```

## Building the Project

```bash
make build
```

Produces `pwplay-server`, `pwplay-client`, and `pwplay-player`.

To build with Go directly:

```bash
go mod download
go build -o pwplay-server ./cmd/server
go build -o pwplay-client ./cmd/client
go build -o pwplay-player ./cmd/player
```

Note: depending on your toolchain, cgo may reject PipeWire's
`-fno-strict-overflow` compiler flag. If you see
`invalid flag in pkg-config --cflags: -fno-strict-overflow`, build with:

```bash
export CGO_CFLAGS_ALLOW='-fno-strict-overflow'
```

## Running the Example

1. Find a FLAC file or download a sample:
```bash
# Example: Download a free sample
wget https://github.com/xiph/flac/raw/master/test/flac-test-files/subset/01%20-%20blocksize%204096.flac -O test.flac
```

2. Play it:
```bash
./pwplay-player test.flac
```

3. Press `space` to start playback, `q` to quit

## Troubleshooting

### "Package libpipewire-0.3 was not found"

Install the PipeWire development package as shown in step 1.

### "No audio output"

1. Check if PipeWire is running:
```bash
systemctl --user status pipewire
```

2. List PipeWire objects to verify connection:
```bash
pw-cli list-objects | grep -i stream
```

3. Check system audio:
```bash
pactl list sinks
```

### "Choppy or stuttering audio"

Check system load (`htop`) and verify no CPU throttling. The lock-free
ring buffer makes underruns unlikely; if they occur, they are logged as
warnings by the player.

### "Build errors with cgo"

Make sure you have:
- gcc or clang installed
- pkg-config installed
- PipeWire development headers installed

Verify pkg-config can find PipeWire:
```bash
pkg-config --modversion libpipewire-0.3
pkg-config --cflags --libs libpipewire-0.3
```

## Integration into Your Project

Add the module to your project:

```bash
go get github.com/uidbz/pwplay
```

Then import the package you need:

```go
import "github.com/uidbz/pwplay/player"    // playback engine
import "github.com/uidbz/pwplay/client"    // REST API client
import "github.com/uidbz/pwplay/pipewire"  // low-level PipeWire bindings
```
