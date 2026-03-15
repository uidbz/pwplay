// Package client provides a Go client for the PipeWire audio webservice API.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Status represents the current state of the audio player.
type Status struct {
	Playing       bool     `json:"playing"`
	Paused        bool     `json:"paused"`
	Stopped       bool     `json:"stopped"`
	CurrentTrack  int      `json:"currentTrack"`
	CurrentFile   string   `json:"currentFile"`
	Playlist      []string `json:"playlist"`
	TotalTracks   int      `json:"totalTracks"`
	Position      float64  `json:"position"`
	TrackDuration float64  `json:"trackDuration"`
	Volume        float64  `json:"volume"`
}

// Client communicates with the PipeWire audio webservice.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a client for the webservice at the given base URL
// (e.g. "http://localhost:8080").
func New(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}
}

// post sends a POST with an optional JSON body and decodes the response.
func (c *Client) post(path string, body interface{}, out interface{}) error {
	var reqBody *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	} else {
		reqBody = bytes.NewReader(nil)
	}

	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		text := strings.TrimSpace(string(body))
		if text != "" {
			return fmt.Errorf("server error %d: %s", resp.StatusCode, text)
		}
		return fmt.Errorf("server error %d", resp.StatusCode)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// get sends a GET and decodes the JSON response.
func (c *Client) get(path string, out interface{}) error {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("server error %d", resp.StatusCode)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Status returns the current player status.
func (c *Client) Status() (*Status, error) {
	var s Status
	if err := c.get("/status", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Play starts or resumes playback.
func (c *Client) Play() error {
	return c.post("/play", nil, nil)
}

// Pause pauses playback.
func (c *Client) Pause() error {
	return c.post("/pause", nil, nil)
}

// Stop stops playback and clears the buffer.
func (c *Client) Stop() error {
	return c.post("/stop", nil, nil)
}

// Next skips to the next track.
func (c *Client) Next() error {
	return c.post("/next", nil, nil)
}

// Previous goes back to the previous track.
func (c *Client) Previous() error {
	return c.post("/previous", nil, nil)
}

// Seek seeks to an absolute position in seconds.
func (c *Client) Seek(position float64) error {
	return c.post("/seek", map[string]float64{"position": position}, nil)
}

// SeekRelative seeks forward or backward by the given number of seconds.
func (c *Client) SeekRelative(offset float64) error {
	return c.post("/seek", map[string]float64{"relative": offset}, nil)
}

// AddTracks adds one or more files, directories, or URLs to the playlist.
// Directories are expanded recursively on the server.
func (c *Client) AddTracks(paths ...string) error {
	return c.post("/add", map[string][]string{"paths": paths}, nil)
}

// RemoveTrack removes the track at the given index (0-based).
func (c *Client) RemoveTrack(index int) error {
	return c.post("/remove", map[string]int{"Index": index}, nil)
}

// MoveItems moves playlist items in the range [from, from+count) so they
// start at index dst. The dst refers to the position in the playlist before
// the items are removed from their original location.
func (c *Client) MoveItems(from, count, dst int) error {
	return c.post("/move", map[string]int{"from": from, "count": count, "to": dst}, nil)
}

// SetVolume sets the playback volume (0.0 = silent, 1.0 = default, 2.0 = max).
func (c *Client) SetVolume(volume float64) error {
	return c.post("/volume", map[string]float64{"volume": volume}, nil)
}
