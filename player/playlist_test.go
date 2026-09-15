package player

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPlaylistFiles(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create test files
	files := []string{
		filepath.Join(tmpDir, "track1.flac"),
		filepath.Join(tmpDir, "track2.mp3"),
		filepath.Join(tmpDir, "track3.wav"),
	}

	for _, f := range files {
		if err := os.WriteFile(f, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Test with individual files
	result, err := ExpandPlaylist(files)
	if err != nil {
		t.Fatalf("ExpandPlaylist failed: %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 files, got %d", len(result))
	}
}

func TestExpandPlaylistDirectory(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create audio files
	audioFiles := map[string]string{
		filepath.Join(tmpDir, "a.flac"): "test",
		filepath.Join(tmpDir, "b.mp3"):  "test",
		filepath.Join(subDir, "c.wav"):  "test",
		filepath.Join(subDir, "d.ogg"):  "test",
	}

	// Create non-audio file (should be ignored)
	if err := os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	for path, content := range audioFiles {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Test with directory
	result, err := ExpandPlaylist([]string{tmpDir})
	if err != nil {
		t.Fatalf("ExpandPlaylist failed: %v", err)
	}

	if len(result) != 4 {
		t.Errorf("Expected 4 audio files, got %d", len(result))
	}

	// Verify files are sorted
	for i := 1; i < len(result); i++ {
		if result[i-1] > result[i] {
			t.Errorf("Files not sorted: %s > %s", result[i-1], result[i])
		}
	}
}

func TestExpandPlaylistMixed(t *testing.T) {
	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create files in directory
	dirFile := filepath.Join(tmpDir, "a.flac")
	if err := os.WriteFile(dirFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create standalone file
	standaloneDir := t.TempDir()
	standaloneFile := filepath.Join(standaloneDir, "b.mp3")
	if err := os.WriteFile(standaloneFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Test with mix of directory and file
	result, err := ExpandPlaylist([]string{tmpDir, standaloneFile})
	if err != nil {
		t.Fatalf("ExpandPlaylist failed: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result))
	}
}

func TestExpandPlaylistEmpty(t *testing.T) {
	// Test with empty directory
	tmpDir := t.TempDir()

	_, err := ExpandPlaylist([]string{tmpDir})
	if err == nil {
		t.Error("Expected error for empty directory")
	}
}

func TestExpandPlaylistNonexistent(t *testing.T) {
	_, err := ExpandPlaylist([]string{"/nonexistent/path"})
	if err == nil {
		t.Error("Expected error for nonexistent path")
	}
}

func TestExpandPlaylistUnsupportedFormat(t *testing.T) {
	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(txtFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ExpandPlaylist([]string{txtFile})
	if err == nil {
		t.Error("Expected error for unsupported format")
	}
}

func TestExpandPlaylistSupportedFormats(t *testing.T) {
	tmpDir := t.TempDir()

	formats := []string{".flac", ".mp3", ".wav", ".ogg", ".opus", ".FLAC", ".MP3"}

	for _, ext := range formats {
		filename := filepath.Join(tmpDir, "test"+ext)
		if err := os.WriteFile(filename, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ExpandPlaylist([]string{tmpDir})
	if err != nil {
		t.Fatalf("ExpandPlaylist failed: %v", err)
	}

	// Should find all files (case-insensitive)
	if len(result) != 7 {
		t.Errorf("Expected 7 files, got %d", len(result))
	}
}
