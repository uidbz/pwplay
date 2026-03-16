package player

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dhowden/tag"
)

// TrackMetadata holds metadata information for an audio track
type TrackMetadata struct {
	Title       string `json:"title"`
	Album       string `json:"album"`
	Artist      string `json:"artist"`
	AlbumArtist string `json:"albumArtist"`
	Composer    string `json:"composer"`
	Genre       string `json:"genre"`
	Year        int    `json:"year"`
	Track       int    `json:"track"`
	TrackTotal  int    `json:"trackTotal"`
	Disc        int    `json:"disc"`
	DiscTotal   int    `json:"discTotal"`
	Lyrics      string `json:"lyrics"`
	Comment     string `json:"comment"`
	Format      string `json:"format"`
	HasPicture  bool   `json:"hasPicture"`
}

// detectAudioFormat determines the actual audio format from file extension
func detectAudioFormat(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(strings.Split(path, "?")[0]), "."))

	// Extract extension from path
	if idx := strings.LastIndex(ext, "."); idx >= 0 {
		ext = ext[idx+1:]
	}

	switch ext {
	case "flac":
		return "FLAC"
	case "mp3":
		return "MP3"
	case "wav", "wave":
		return "WAV"
	case "ogg":
		return "OGG"
	case "m4a", "m4b", "m4p", "alac":
		return "M4A"
	case "mp4":
		return "MP4"
	default:
		return strings.ToUpper(ext)
	}
}

// ExtractMetadata extracts metadata from an audio file
func ExtractMetadata(path string) (*TrackMetadata, error) {
	// Handle HTTP URLs - we'd need to download first or skip
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return &TrackMetadata{
			Title: path,
		}, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		// If we can't read metadata, return basic info
		return &TrackMetadata{
			Title:  path,
			Format: detectAudioFormat(path),
		}, nil
	}

	track, trackTotal := m.Track()
	disc, discTotal := m.Disc()

	return &TrackMetadata{
		Title:       m.Title(),
		Album:       m.Album(),
		Artist:      m.Artist(),
		AlbumArtist: m.AlbumArtist(),
		Composer:    m.Composer(),
		Genre:       m.Genre(),
		Year:        m.Year(),
		Track:       track,
		TrackTotal:  trackTotal,
		Disc:        disc,
		DiscTotal:   discTotal,
		Lyrics:      m.Lyrics(),
		Comment:     m.Comment(),
		Format:      detectAudioFormat(path),
		HasPicture:  m.Picture() != nil,
	}, nil
}

// ExtractAlbumCover extracts the album cover from an audio file
func ExtractAlbumCover(path string) ([]byte, string, error) {
	// Handle HTTP URLs
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return nil, "", fmt.Errorf("album cover extraction not supported for URLs")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	m, err := tag.ReadFrom(f)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read metadata: %w", err)
	}

	pic := m.Picture()
	if pic == nil {
		return nil, "", fmt.Errorf("no album cover found")
	}

	return pic.Data, pic.MIMEType, nil
}

// GetAlbumCoverReader returns a reader for the album cover
func GetAlbumCoverReader(path string) (io.ReadCloser, string, error) {
	// Handle HTTP URLs
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return nil, "", fmt.Errorf("album cover extraction not supported for URLs")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, "", fmt.Errorf("failed to open file: %w", err)
	}

	m, err := tag.ReadFrom(f)
	if err != nil {
		f.Close()
		return nil, "", fmt.Errorf("failed to read metadata: %w", err)
	}

	pic := m.Picture()
	if pic == nil {
		f.Close()
		return nil, "", fmt.Errorf("no album cover found")
	}

	// Return the picture data as a ReadCloser
	// We need to close the file after the data is read
	return io.NopCloser(strings.NewReader(string(pic.Data))), pic.MIMEType, nil
}
