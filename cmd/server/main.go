package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/uidbz/pwplay/pipewire"
	"github.com/uidbz/pwplay/player"
)

var p *player.Player

func main() {
	passthrough := flag.Bool("passthrough", false, "Disable software volume, prevent resampling and channel remixing")
	exclusive := flag.Bool("exclusive", false, "Request exclusive access to the audio device (use with -passthrough)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <file-or-directory> [file-or-directory] ...\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Supported formats: FLAC, MP3, WAV, OGG\n")
		fmt.Fprintf(os.Stderr, "Directories are scanned recursively for audio files.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	paths := flag.Args()
	if len(paths) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	// Expand directories into audio files
	files, err := player.ExpandPlaylist(paths)
	if err != nil {
		log.Fatalf("Failed to build playlist: %v", err)
	}

	if err := pipewire.Init(); err != nil {
		log.Fatal(err)
	}
	defer pipewire.Deinit()

	opts := player.PlayerOptions{
		StartPaused: true,
		Passthrough: *passthrough,
		Exclusive:   *exclusive,
	}
	p, err = player.NewPlayerWithOptions(files, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer p.Close()

	if *passthrough {
		log.Println("Passthrough mode: no software volume, native sample rate, no channel remix")
	}
	if *exclusive {
		log.Println("Exclusive mode: sole access to audio device")
	}

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

	// Jump to and play the track at the given queue index.
	// POST /goto {"index": 3}
	http.HandleFunc("/goto", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Index int `json:"index"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		p.Goto(req.Index)
		json.NewEncoder(w).Encode(map[string]string{"status": "goto"})
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
			"passthrough":   p.IsPassthrough(),
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

	// Get metadata for the current track.
	// GET /metadata
	http.HandleFunc("/metadata", func(w http.ResponseWriter, r *http.Request) {
		metadata, err := p.CurrentTrackMetadata()
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metadata)
	})

	// Get album cover for the current track.
	// GET /cover
	http.HandleFunc("/cover", func(w http.ResponseWriter, r *http.Request) {
		file := p.CurrentFile()
		if file == "" {
			http.Error(w, "no current track", 404)
			return
		}

		coverData, mimeType, err := player.ExtractAlbumCover(file)
		if err != nil {
			http.Error(w, err.Error(), 404)
			return
		}

		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(coverData)))
		w.Write(coverData)
	})

	// Get metadata for all tracks in the playlist.
	// GET /playlist-metadata
	http.HandleFunc("/playlist-metadata", func(w http.ResponseWriter, r *http.Request) {
		metadata := p.PlaylistMetadata()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metadata)
	})

	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
