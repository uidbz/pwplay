# Examples

The `examples/` directory contains standalone demo programs. Each is a
separate `main` package and builds independently:

```bash
go build -o /tmp/simple_tone ./examples/simple_tone.go
```

Note: `go build ./examples` does not work — the directory holds several
independent programs in one package.

## simple_tone.go

Sine wave tone generator demonstrating real-time audio synthesis.

```bash
./simple_tone          # A4 (440 Hz)
./simple_tone 261.63   # middle C
./simple_tone 880      # A5
```

Press Ctrl+C to stop.

### Musical Notes Reference

| Note | Frequency (Hz) |
|------|----------------|
| C4   | 261.63         |
| D4   | 293.66         |
| E4   | 329.63         |
| F4   | 349.23         |
| G4   | 392.00         |
| A4   | 440.00         |
| B4   | 493.88         |
| C5   | 523.25         |

## play_flac.go

Minimal FLAC player using the `pipewire` package directly: manual ring
buffer, decoder loop, and stream callback. A good reference for building
a custom player on the low-level bindings.

```bash
./play_flac ~/Music/song.flac
```

## simple_playlist.go

Auto-playing multi-format playlist built on the `player` package. Starts
playback immediately and runs until the playlist ends.

```bash
./simple_playlist track1.flac track2.mp3 track3.ogg
```

## playlist_player.go

Non-interactive playlist player with a self-contained player
implementation (own ring buffer and decoder thread). Superseded by the
`player` package; kept as a reference for custom implementations.

## playlist_player_interactive.go

Interactive playlist player with keyboard controls, implemented directly
on the `pipewire` package. The production equivalent is `pwplay-player`
(`cmd/player`), which is built on the shared `player` package.

## Writing Your Own

Minimal stream setup:

```go
package main

import (
    "log"
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

    stream, err := pipewire.NewStreamWithOptions("My App", format,
        func(buffer []byte, frames int) int {
            // Convert to interleaved float32 and fill with audio data
            // (silence in this example).
            output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])),
                                   frames*2)
            for i := range output {
                output[i] = 0
            }
            return frames
        }, pipewire.StreamOptions{})
    if err != nil {
        log.Fatal(err)
    }
    defer stream.Destroy()

    if err := stream.Connect(format); err != nil {
        log.Fatal(err)
    }

    time.Sleep(5 * time.Second)
}
```

For high-level playback, use the `player` package instead of writing a
callback yourself — see the README's [Go Packages](../README.md#go-packages)
section.

## Performance Tips

1. **Keep callbacks fast**: The process callback runs in a real-time thread. Avoid:
   - Memory allocation
   - I/O operations
   - Locks (if possible)
   - Complex computations

2. **Buffer ahead**: Prepare audio data before the callback needs it (see
   the ring buffer in `player/player.go`)

3. **Use appropriate buffer sizes**: Balance latency vs. CPU usage

4. **Profile your code**: Use Go's profiling tools to find bottlenecks

## Debugging

Enable PipeWire debug output:
```bash
PIPEWIRE_DEBUG=3 ./your_app
```

List PipeWire objects:
```bash
pw-cli list-objects
```

Monitor stream state:
```bash
pw-top
```

Check for errors:
```bash
journalctl --user -u pipewire -f
```
