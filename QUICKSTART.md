# Quick Start Guide

Get up and running with PipeWire Go bindings in 5 minutes.

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

## 2. Verify PipeWire is Running

```bash
systemctl --user status pipewire
```

If not running:
```bash
systemctl --user start pipewire
```

## 3. Build the Examples

```bash
make build
```

Or manually:
```bash
go build -o play_flac ./examples/play_flac.go
go build -o simple_tone ./examples/simple_tone.go
```

## 4. Test with Tone Generator

```bash
./simple_tone 440
```

You should hear a 440 Hz tone. Press Ctrl+C to stop.

## 5. Test with FLAC File (Optional)

If you have a FLAC file:
```bash
./play_flac /path/to/your/file.flac
```

Or download a test file:
```bash
# Download a small test FLAC
wget "https://raw.githubusercontent.com/xiph/flac/master/test/flac-test-files/subset/01%20-%20blocksize%204096.flac" -O test.flac
./play_flac test.flac
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
pkg-config --cflags --libs libpipewire-0.3
```

## Your First Program

Create `my_player.go`:

```go
package main

import (
    "log"
    "math"
    "os"
    "os/signal"
    "time"
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
    stream, _ := pipewire.NewStream("Beep", format,
        func(buffer []byte, frames int) int {
            output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])),
                                   frames * 2)

            for i := 0; i < frames; i++ {
                sample := float32(math.Sin(phase) * 0.3)
                output[i*2] = sample     // Left
                output[i*2+1] = sample   // Right
                phase += 2 * math.Pi * 440 / 48000
            }
            return frames
        })

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

- Read `EXAMPLES.md` for more examples
- Check `README.md` for API documentation
- See `INSTALL.md` for detailed installation
- Explore the `examples/` directory for complete applications

## Common Use Cases

**Playing audio files:** See `examples/play_flac.go`

**Generating tones:** See `examples/simple_tone.go`

**Real-time synthesis:** Modify the callback in simple_tone.go

**Recording audio:** Use `PW_DIRECTION_INPUT` (see API docs)

## Getting Help

1. Check the documentation files
2. Review the example code
3. Use `pw-cli` for debugging PipeWire
4. Enable debug output: `PIPEWIRE_DEBUG=3 ./your_app`

Happy coding!
