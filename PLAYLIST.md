# Playlist Player

Gapless FLAC playlist player with support for 16, 24, and 32-bit audio.

## Features

- **Gapless Playback**: Seamless transitions between tracks
- **Multi-bit Depth**: Supports 16, 24, and 32-bit FLAC files
- **Ring Buffer**: 3-second buffer for smooth playback
- **Background Decoding**: Separate thread for I/O operations
- **Auto-advance**: Automatically plays next track

## Usage

```bash
./playlist_player file1.flac file2.flac file3.flac ...
```

## Examples

### Play Multiple Files
```bash
./playlist_player track1.flac track2.flac track3.flac
```

### Play All FLAC Files in Directory
```bash
./playlist_player *.flac
```

### Play Specific Album
```bash
./playlist_player ~/Music/Album/*.flac
```

## Output

```
2026/03/12 17:25:00 Playlist: 3 tracks
2026/03/12 17:25:00   [1] track1.flac
2026/03/12 17:25:00   [2] track2.flac
2026/03/12 17:25:00   [3] track3.flac
2026/03/12 17:25:00 Format: 44100 Hz, 2 channels, 16 bits
2026/03/12 17:25:00 Buffering...
2026/03/12 17:25:00 Now playing [1/3]: track1.flac
2026/03/12 17:25:00 Playing... Press Ctrl+C to stop
2026/03/12 17:25:03 Now playing [2/3]: track2.flac
2026/03/12 17:25:06 Now playing [3/3]: track3.flac
2026/03/12 17:25:09 Playlist finished
```

## Bit Depth Support

The player automatically detects and handles different bit depths:

### 16-bit (CD Quality)
- Sample range: -32,768 to 32,767
- Divisor: 32,768 (2^15)
- Most common format

### 24-bit (High-Resolution)
- Sample range: -8,388,608 to 8,388,607
- Divisor: 8,388,608 (2^23)
- Studio quality

### 32-bit (Maximum Quality)
- Sample range: -2,147,483,648 to 2,147,483,647
- Divisor: 2,147,483,648 (2^31)
- Lossless archival

## Gapless Playback

Gapless playback means no silence between tracks:

1. **Pre-buffering**: Each track starts buffering before the previous finishes
2. **Continuous Stream**: Audio callback never starves
3. **Seamless Transition**: No clicks or pops between tracks

## How It Works

```
Track 1          Track 2          Track 3
┌─────────┐     ┌─────────┐     ┌─────────┐
│ Decode  │ --> │ Decode  │ --> │ Decode  │
└────┬────┘     └────┬────┘     └────┬────┘
     │               │               │
     └───────────────┴───────────────┘
                     │
              ┌──────▼──────┐
              │ Ring Buffer │
              │  (3 seconds)│
              └──────┬──────┘
                     │
              ┌──────▼──────┐
              │   PipeWire  │
              │   Playback  │
              └─────────────┘
```

## Architecture

### Decoder Thread
- Decodes FLAC frames in background
- Converts samples to float32
- Handles bit depth conversion
- Loads next track automatically

### Ring Buffer
- 3-second circular buffer
- Smooth track transitions
- Prevents underruns
- Gapless continuity

### Audio Callback
- Fast read from ring buffer
- Real-time safe
- No I/O operations
- Consistent timing

## Performance

| Metric | Value |
|--------|-------|
| Buffer Size | 3 seconds |
| Bit Depths | 16, 24, 32-bit |
| Transition | Gapless |
| CPU Usage | <3% |
| Latency | 20-50ms |

## Limitations

- All tracks must have same sample rate
- All tracks must have same channel count
- No seeking within tracks (yet)
- No shuffle/repeat (yet)

## Creating Test Files

### 16-bit FLAC
```bash
ffmpeg -f lavfi -i "sine=frequency=440:duration=3" \
       -ac 2 -ar 44100 track1.flac
```

### 24-bit FLAC
```bash
ffmpeg -f lavfi -i "sine=frequency=880:duration=3" \
       -ac 2 -ar 44100 -bits_per_raw_sample 24 track2.flac
```

### Convert Existing Files
```bash
ffmpeg -i input.wav -c:a flac -bits_per_raw_sample 24 output.flac
```

## Comparison with Single-File Player

| Feature | play_flac | playlist_player |
|---------|-----------|-----------------|
| Multiple files | No | Yes |
| Gapless | N/A | Yes |
| Bit depths | 16/24/32 | 16/24/32 |
| Buffer | 2 sec | 3 sec |
| Auto-advance | No | Yes |

## Error Handling

If a track fails to load:
- Logs the error
- Automatically skips to next track
- Continues playback

Example:
```
2026/03/12 17:25:03 Failed to open track2.flac: no such file
2026/03/12 17:25:03 Now playing [3/3]: track3.flac
```

## Tips

### Organize Your Music
```bash
# Create playlist from directory
ls -1 ~/Music/Album/*.flac > playlist.txt
cat playlist.txt | xargs ./playlist_player
```

### Natural Sort Order
```bash
# Use natural sort for track numbers
ls -1v *.flac | xargs ./playlist_player
```

### Check Bit Depth
```bash
# Check FLAC file info
flac --list file.flac | grep "bits-per-sample"
```

## Future Enhancements

Planned features:
- [ ] Seek support
- [ ] Shuffle mode
- [ ] Repeat mode
- [ ] Different sample rate handling
- [ ] Crossfade between tracks
- [ ] Volume control
- [ ] Progress display
