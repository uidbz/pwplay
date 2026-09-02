# Client Package

Go client library for the pwplay server API.

```go
import "github.com/uidbz/pwplay/client"
```

## Usage

```go
c := client.New("http://localhost:8080")

s, _ := c.Status()
fmt.Printf("Track: %s [%.0fs / %.0fs] vol %d%%\n",
    s.CurrentFile, s.Position, s.TrackDuration, int(s.Volume*100))

c.Play()
c.Pause()
c.Stop()
c.Next()
c.Previous()
c.Goto(3)          // jump to playlist index 3

c.Seek(120.0)        // jump to 2:00
c.SeekRelative(10)   // forward 10 seconds
c.SeekRelative(-30)  // backward 30 seconds

c.SetVolume(0.8)     // 80% volume (ignored in passthrough mode)

c.AddTracks("/path/to/song.flac")                  // single file
c.AddTracks("/path/to/album", "http://url/song.mp3") // multiple
c.RemoveTrack(3)                                    // 0-based index
c.MoveItems(5, 3, 0)                               // move items 5,6,7 to index 0
```

## API Reference

### client.New

```go
func New(baseURL string) *Client
```

Creates a client for the server at the given base URL.

### Status

```go
func (c *Client) Status() (*Status, error)
```

| Field | Type | Description |
|---|---|---|
| `Playing` | `bool` | True if actively playing |
| `Paused` | `bool` | True if paused |
| `Stopped` | `bool` | True if stopped |
| `Passthrough` | `bool` | True if the server runs in passthrough mode |
| `CurrentTrack` | `int` | 0-based index of current track |
| `CurrentFile` | `string` | Filename of current track |
| `Playlist` | `[]string` | All tracks in the playlist |
| `TotalTracks` | `int` | Number of tracks in playlist |
| `Position` | `float64` | Playback position in seconds |
| `TrackDuration` | `float64` | Track duration in seconds (-1 if unknown) |
| `Volume` | `float64` | Volume as linear gain (0.0-2.0, always 1.0 in passthrough) |

### Playback Controls

```go
func (c *Client) Play() error
func (c *Client) Pause() error
func (c *Client) Stop() error
func (c *Client) Next() error
func (c *Client) Previous() error
func (c *Client) Goto(index int) error
```

### Seek

```go
func (c *Client) Seek(position float64) error
func (c *Client) SeekRelative(offset float64) error
```

### Volume

```go
func (c *Client) SetVolume(volume float64) error
```

Sets volume from 0.0 (silent) to 2.0 (200%). Default is 1.0. Ignored in passthrough mode.

### Playlist Management

```go
func (c *Client) AddTracks(paths ...string) error
func (c *Client) RemoveTrack(index int) error
func (c *Client) MoveItems(from, count, dst int) error
```

`AddTracks` accepts file paths, directories, or HTTP URLs. Directories are expanded recursively on the server. `MoveItems` moves items `[from, from+count)` to start at index `dst`.

### Metadata

```go
func (c *Client) Metadata() (*Metadata, error)
func (c *Client) PlaylistMetadata() ([]*Metadata, error)
func (c *Client) CoverURL() string
```

`Metadata` returns tags for the current track (title, artist, album, track/disc numbers, genre, year, lyrics, comment, format, `HasPicture`). `PlaylistMetadata` returns metadata for all tracks. `CoverURL` returns the URL of the server's `/cover` endpoint, which serves the current track's album art.

## TUI Client

```bash
pwplay-client              # connects to localhost:8080
pwplay-client myhost:8080  # remote server
```

### Controls

| Key | Action |
|---|---|
| `space` | Play/Pause |
| `s` | Stop |
| `n` / `p` | Next / Previous track |
| `f` / `+` | Seek forward 10s |
| `b` / `-` | Seek backward 10s |
| `F` / `B` | Seek forward / backward 30s |
| `0` | Seek to start of track |
| `1`-`9` | Seek to 10%-90% of track |
| `v` / `V` | Volume down / up (5%) |
| `m` | Mute / unmute |
| `l` | Toggle playlist display |
| `a` | Add track (prompts for path) |
| `r` | Remove track (prompts for number) |
| `q` | Quit |
