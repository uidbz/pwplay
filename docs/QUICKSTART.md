# Quick Start Guide

Get up and running with pwplay in 5 minutes.

## 1. Install Prerequisites

```bash
# Ubuntu/Debian
sudo apt update
sudo apt install libpipewire-0.3-dev pkg-config build-essential

# Fedora
sudo dnf install pipewire-devel pkg-config gcc

# Arch Linux
sudo pacman -S pipewire pkg-config base-devel
```

Verify PipeWire is running:

```bash
systemctl --user status pipewire
```

## 2. Build

```bash
make build
```

This produces `pwplay-server`, `pwplay-client`, and `pwplay-player`.

## 3. Play Something

```bash
./pwplay-player ~/Music/album
```

Press `space` to start playback, `n`/`p` to change tracks, `q` to quit.

No audio files handy? Download a test FLAC:

```bash
wget "https://raw.githubusercontent.com/xiph/flac/master/test/flac-test-files/subset/01%20-%20blocksize%204096.flac" -O test.flac
./pwplay-player test.flac
```

## Troubleshooting

**No audio?**

```bash
# Check PipeWire status
pw-cli list-objects | grep -i stream

# Check audio devices
pactl list sinks
```

**Build errors?**

```bash
# Verify pkg-config finds PipeWire
pkg-config --modversion libpipewire-0.3
```

If cgo rejects PipeWire's compiler flags:

```bash
export CGO_CFLAGS_ALLOW='-fno-strict-overflow'
```

See [INSTALL.md](INSTALL.md) for the detailed installation guide.

## Your First Program

Generate a 440 Hz tone using the `pipewire` package:

```go
package main

import (
    "log"
    "math"
    "os"
    "os/signal"
    "unsafe"

    "github.com/uidbz/pwplay/pipewire"
)

func main() {
    pipewire.Init()
    defer pipewire.Deinit()

    format := pipewire.AudioFormat{
        SampleRate: 48000,
        Channels:   2,
    }

    phase := 0.0
    stream, err := pipewire.NewStreamWithOptions("Beep", format,
        func(buffer []byte, frames int) int {
            output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])),
                                   frames*2)

            for i := 0; i < frames; i++ {
                sample := float32(math.Sin(phase) * 0.3)
                output[i*2] = sample   // Left
                output[i*2+1] = sample // Right
                phase += 2 * math.Pi * 440 / 48000
            }
            return frames
        }, pipewire.StreamOptions{})
    if err != nil {
        log.Fatal(err)
    }

    stream.Connect(format)
    defer stream.Destroy()

    log.Println("Playing 440 Hz tone...")

    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt)
    <-c
}
```

Build and run:

```bash
go run my_player.go
```

## What's Next?

- [README.md](../README.md) — feature overview and Go package APIs
- [USAGE.md](USAGE.md) — the three applications in detail
- [WEBSERVICE.md](WEBSERVICE.md) — REST API reference
- [EXAMPLES.md](EXAMPLES.md) — example programs and patterns
- `examples/` — complete standalone demo programs
