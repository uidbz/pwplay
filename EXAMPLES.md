# Examples

This document describes the example applications included with the PipeWire Go bindings.

## 1. FLAC Player (`examples/play_flac.go`)

A complete FLAC audio player that demonstrates:
- Opening and decoding FLAC files
- Streaming audio through PipeWire
- Sample rate and channel conversion
- Buffer management for smooth playback
- Graceful shutdown

### Usage

```bash
./play_flac <path-to-flac-file>
```

### Example

```bash
./play_flac ~/Music/song.flac
```

Press Ctrl+C to stop playback.

### Features

- Supports any sample rate and channel count in FLAC files
- Automatically converts to float32 format
- Real-time streaming with buffering
- Clean EOF handling

## 2. Simple Tone Generator (`examples/simple_tone.go`)

A sine wave tone generator that demonstrates:
- Generating audio programmatically
- Phase-accurate synthesis
- Multi-channel output
- Real-time audio processing

### Usage

```bash
./simple_tone [frequency]
```

### Examples

Play A4 (440 Hz):
```bash
./simple_tone
```

Play middle C (261.63 Hz):
```bash
./simple_tone 261.63
```

Play A5 (880 Hz):
```bash
./simple_tone 880
```

Press Ctrl+C to stop playback.

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

## Building Your Own Examples

Here's a minimal example to get started:

```go
package main

import (
    "log"
    "time"
    "unsafe"
    "github.com/uidbz/pwplay/pipewire"
)

func main() {
    // Initialize PipeWire
    pipewire.Init()
    defer pipewire.Deinit()

    // Define audio format
    format := pipewire.AudioFormat{
        SampleRate: 48000,
        Channels:   2,
    }

    // Create stream with callback
    stream, err := pipewire.NewStream("My App", format,
        func(buffer []byte, frames int) int {
            // Convert to float32
            output := unsafe.Slice((*float32)(unsafe.Pointer(&buffer[0])),
                                   frames * 2) // 2 channels

            // Fill with silence (or your audio data)
            for i := range output {
                output[i] = 0
            }

            return frames
        })

    if err != nil {
        log.Fatal(err)
    }
    defer stream.Destroy()

    // Connect and start
    if err := stream.Connect(format); err != nil {
        log.Fatal(err)
    }

    // Keep running
    time.Sleep(5 * time.Second)
}
```

## Advanced Usage

### Custom Buffer Sizes

Modify the callback to handle variable frame counts:

```go
callback := func(buffer []byte, frames int) int {
    // frames tells you how many frames PipeWire is requesting
    // Return the number of frames you actually filled
    return frames
}
```

### Multi-Channel Audio

The bindings support any channel count:

```go
format := pipewire.AudioFormat{
    SampleRate: 48000,
    Channels:   6, // 5.1 surround
}
```

### Sample Rate Conversion

PipeWire handles sample rate conversion automatically, but you can specify your preferred rate:

```go
format := pipewire.AudioFormat{
    SampleRate: 96000, // High-quality audio
    Channels:   2,
}
```

## Performance Tips

1. **Keep callbacks fast**: The process callback runs in a real-time thread. Avoid:
   - Memory allocation
   - I/O operations
   - Locks (if possible)
   - Complex computations

2. **Buffer ahead**: Prepare audio data before the callback needs it

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
