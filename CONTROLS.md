# Interactive Playlist Player

Full-featured FLAC player with keyboard controls for play, pause, stop, next, and previous.

## Features

✅ Play/Pause control
✅ Next/Previous track
✅ Stop playback
✅ Track info display
✅ Gapless playback
✅ 16/24/32-bit support

## Usage

```bash
pwplay-player file1.flac file2.flac file3.flac
```

## Keyboard Controls

| Key | Action |
|-----|--------|
| `space` | Toggle Play/Pause |
| `n` | Next track |
| `p` | Previous track |
| `s` | Stop playback |
| `i` | Show track info |
| `h` or `?` | Show help |
| `q` | Quit |

## Example Session

```
$ pwplay-player track1.flac track2.flac track3.flac

2026/03/12 17:32:43 Playlist: 3 tracks
  [1] track1.flac
  [2] track2.flac
  [3] track3.flac
2026/03/12 17:32:43 Now playing [1/3]: track1.flac

Controls:
  space    - Play/Pause
  n        - Next track
  ...

> [press space to pause]
⏸  Paused

> [press space to resume]
▶  Playing

> n
⏭  Next track
Now playing [2/3]: track2.flac

> i
🎵 [2/3] track2.flac

> q
Bye!
```

## Build

```bash
go build -o playlist_interactive ./examples/playlist_player_interactive.go
```

## Comparison

| Feature | playlist_player | playlist_interactive |
|---------|----------------|---------------------|
| Gapless | ✅ | ✅ |
| Bit depths | 16/24/32 | 16/24/32 |
| Controls | None | Full |
| Mode | Auto | Interactive |
