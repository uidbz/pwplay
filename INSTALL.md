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

If you need to install Go, download it from https://go.lang.org/dl/

### 3. Verify PipeWire is Running

```bash
systemctl --user status pipewire
```

If it's not running:
```bash
systemctl --user start pipewire
```

## Building the Project

### Option 1: Using Make

```bash
make install-deps
make build
```

### Option 2: Using Go directly

```bash
go mod download
go build -o play_flac ./examples/play_flac.go
```

## Running the Example

1. Find a FLAC file or download a sample:
```bash
# Example: Download a free sample
wget https://github.com/xiph/flac/raw/master/test/flac-test-files/subset/01%20-%20blocksize%204096.flac -O test.flac
```

2. Run the player:
```bash
./play_flac test.flac
```

3. Stop playback with Ctrl+C

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

Try increasing the buffer size by modifying the `bufferSize` field in `examples/play_flac.go`.

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

To use these bindings in your own project:

1. Copy the `pipewire/` directory to your project
2. Import it: `import "yourproject/pipewire"`
3. Update your `go.mod` as needed

Or reference it as a module:
```go
import "git.sr.ht/~uid/pwplay/pipewire"
```
