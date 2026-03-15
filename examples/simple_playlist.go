package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.sr.ht/~uid/pwplay/pipewire"
	"git.sr.ht/~uid/pwplay/player"
)

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
		log.Fatalf("Failed to initialize PipeWire: %v", err)
	}
	defer pipewire.Deinit()

	log.Printf("Playlist: %d tracks", len(files))

	// Create player (not paused - starts playing immediately)
	p, err := player.NewPlayer(files, false)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}
	defer p.Close()

	log.Println("Playing... Press Ctrl+C to stop")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			log.Println("\nInterrupted, stopping playback...")
			return
		case <-ticker.C:
			if p.IsEOF() {
				log.Println("Playlist finished")
				time.Sleep(200 * time.Millisecond)
				return
			}
		}
	}
}
