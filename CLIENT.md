# Client Package

Go client library for the pwplay server API.

```go
import "git.sr.ht/~uid/pwplay/client"
```

## Usage

```go
c := client.New("http://localhost:8080")

s, _ := c.Status()
fmt.Printf("Track: %s [%.0fs / %.0fs]\n", s.CurrentFile, s.Position, s.TrackDuration)

c.Play()
c.Pause()
c.Stop()
c.Next()
c.Previous()

c.Seek(120.0)        // jump to 2:00
c.SeekRelative(10)   // forward 10 seconds
c.SeekRelative(-30)  // backward 30 seconds

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
| `CurrentTrack` | `int` | 0-based index of current track |
| `CurrentFile` | `string` | Filename of current track |
| `Playlist` | `[]string` | All tracks in the playlist |
| `TotalTracks` | `int` | Number of tracks in playlist |
| `Position` | `float64` | Playback position in seconds |
| `TrackDuration` | `float64` | Track duration in seconds (-1 if unknown) |

### Playback Controls

```go
func (c *Client) Play() error
func (c *Client) Pause() error
func (c *Client) Stop() error
func (c *Client) Next() error
func (c *Client) Previous() error
```

### Seek

```go
func (c *Client) Seek(position float64) error
func (c *Client) SeekRelative(offset float64) error
```

### Playlist Management

```go
func (c *Client) AddTracks(paths ...string) error
func (c *Client) RemoveTrack(index int) error
func (c *Client) MoveItems(from, count, dst int) error
```

`AddTracks` accepts file paths, directories, or HTTP URLs. Directories are expanded recursively on the server. `MoveItems` moves items `[from, from+count)` to start at index `dst`.

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
| `l` | Toggle playlist display |
| `a` | Add track (prompts for path) |
| `r` | Remove track (prompts for number) |
| `q` | Quit |
