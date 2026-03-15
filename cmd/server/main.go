package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"git.sr.ht/~uid/pwplay/pipewire"
	"git.sr.ht/~uid/pwplay/player"
)

var p *player.Player

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <file-or-directory> [file-or-directory] ...\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nSupported formats: FLAC, MP3, WAV, OGG\n")
		fmt.Fprintf(os.Stderr, "Directories are scanned recursively for audio files.\n")
		os.Exit(1)
	}

	paths := os.Args[1:]

	// Expand directories into audio files
	files, err := player.ExpandPlaylist(paths)
	if err != nil {
		log.Fatalf("Failed to build playlist: %v", err)
	}

	if err := pipewire.Init(); err != nil {
		log.Fatal(err)
	}
	defer pipewire.Deinit()

	// Create player (paused - controlled via API)
	p, err = player.NewPlayer(files, true)
	if err != nil {
		log.Fatal(err)
	}
	defer p.Close()

	http.HandleFunc("/play", func(w http.ResponseWriter, r *http.Request) {
		p.Play()
		json.NewEncoder(w).Encode(map[string]string{"status": "playing"})
	})

	http.HandleFunc("/pause", func(w http.ResponseWriter, r *http.Request) {
		p.Pause()
		json.NewEncoder(w).Encode(map[string]string{"status": "paused"})
	})

	http.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		p.Stop()
		json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
	})

	http.HandleFunc("/next", func(w http.ResponseWriter, r *http.Request) {
		p.Next()
		json.NewEncoder(w).Encode(map[string]string{"status": "next"})
	})

	http.HandleFunc("/previous", func(w http.ResponseWriter, r *http.Request) {
		p.Previous()
		json.NewEncoder(w).Encode(map[string]string{"status": "previous"})
	})

	// Seek to an absolute position in seconds.
	// POST /seek {"position": 30.5}
	// Or seek relative to current position:
	// POST /seek {"relative": -10}
	http.HandleFunc("/seek", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Position *float64 `json:"position"`
			Relative *float64 `json:"relative"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		if req.Relative != nil {
			p.SeekRelative(*req.Relative)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":   "seeked",
				"position": p.Position(),
			})
		} else if req.Position != nil {
			p.Seek(*req.Position)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":   "seeked",
				"position": *req.Position,
			})
		} else {
			http.Error(w, `requires "position" or "relative" field`, 400)
		}
	})

	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		idx := p.CurrentTrack()
		file := p.CurrentFile()
		playlist := p.Playlist()

		json.NewEncoder(w).Encode(map[string]interface{}{
			"playing":       p.IsPlaying(),
			"paused":        p.IsPaused(),
			"stopped":       p.IsStopped(),
			"currentTrack":  idx,
			"currentFile":   filepath.Base(file),
			"playlist":      playlist,
			"totalTracks":   len(playlist),
			"position":      p.Position(),
			"trackDuration": p.TrackDuration(),
			"volume":        p.Volume(),
		})
	})

	http.HandleFunc("/add", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Paths []string `json:"paths"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if len(req.Paths) == 0 {
			http.Error(w, `requires non-empty "paths" array`, 400)
			return
		}
		// Expand directories into individual audio files
		expanded, err := player.ExpandPlaylist(req.Paths)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		for _, f := range expanded {
			p.AddTrack(f)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":      "added",
			"tracksAdded": len(expanded),
		})
	})

	http.HandleFunc("/remove", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Index int }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		p.RemoveTrack(req.Index)
		json.NewEncoder(w).Encode(map[string]string{"status": "removed"})
	})

	// Move a range of playlist items to a new position.
	// POST /move {"from": 5, "count": 3, "to": 0}
	// Moves items at indices 5,6,7 to start at index 0.
	http.HandleFunc("/move", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			From  int `json:"from"`
			Count int `json:"count"`
			To    int `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := p.MoveItems(req.From, req.Count, req.To); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "moved"})
	})

	// Set volume (0.0 - 2.0).
	// POST /volume {"volume": 0.8}
	http.HandleFunc("/volume", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Volume float64 `json:"volume"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		p.SetVolume(req.Volume)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"volume": p.Volume(),
		})
	})

	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
