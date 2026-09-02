package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/uidbz/pwplay/pipewire"
	"github.com/uidbz/pwplay/player"
)

func printHelp() {
	fmt.Println("\nControls:")
	fmt.Println("  space    - Play/Pause")
	fmt.Println("  n        - Next track")
	fmt.Println("  p        - Previous track")
	fmt.Println("  f / +    - Seek forward 10 seconds")
	fmt.Println("  b / -    - Seek backward 10 seconds")
	fmt.Println("  v / V    - Volume down / up (5%)")
	fmt.Println("  m        - Mute / unmute")
	fmt.Println("  s        - Stop")
	fmt.Println("  i        - Track info")
	fmt.Println("  h/?      - Help")
	fmt.Println("  q        - Quit")
}

func formatTime(seconds float64) string {
	if seconds < 0 {
		return "??:??"
	}
	min := int(seconds) / 60
	sec := int(seconds) % 60
	return fmt.Sprintf("%d:%02d", min, sec)
}

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
		log.Fatalf("Failed to initialize PipeWire: %v", err)
	}
	defer pipewire.Deinit()

	log.Printf("Playlist: %d tracks", len(files))
	for i, f := range files {
		log.Printf("  [%d] %s", i+1, filepath.Base(f))
	}

	opts := player.PlayerOptions{
		StartPaused: true,
		Passthrough: *passthrough,
		Exclusive:   *exclusive,
	}
	p, err := player.NewPlayerWithOptions(files, opts)
	if err != nil {
		log.Fatalf("Failed to create player: %v", err)
	}
	defer p.Close()

	if *passthrough {
		log.Println("Passthrough mode: no software volume, native sample rate, no channel remix")
	}
	if *exclusive {
		log.Println("Exclusive mode: sole access to audio device")
	}

	log.Printf("Ready. Press 'space' to start playback")
	printHelp()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}

		cmd := strings.TrimSpace(strings.ToLower(scanner.Text()))

		switch cmd {
		case " ", "":
			if p.IsPaused() {
				p.Play()
				log.Println("Playing")
			} else {
				p.Pause()
				log.Println("Paused")
			}
		case "n", "next":
			p.Next()
			log.Println("Next track")
		case "p", "prev", "previous":
			p.Previous()
			log.Println("Previous track")
		case "f", "+":
			p.SeekRelative(10)
			log.Printf("Seek +10s -> %s", formatTime(p.Position()))
		case "b", "-":
			p.SeekRelative(-10)
			log.Printf("Seek -10s -> %s", formatTime(p.Position()))
		case "v":
			v := p.Volume() - 0.05
			if v < 0 {
				v = 0
			}
			p.SetVolume(v)
			log.Printf("Volume %d%%", int(v*100+0.5))
		case "V":
			v := p.Volume() + 0.05
			if v > 2 {
				v = 2
			}
			p.SetVolume(v)
			log.Printf("Volume %d%%", int(v*100+0.5))
		case "m":
			if p.Volume() > 0 {
				p.SetVolume(0)
				log.Println("Muted")
			} else {
				p.SetVolume(1.0)
				log.Println("Volume 100%")
			}
		case "s", "stop":
			p.Stop()
			log.Println("Stopped")
		case "i", "info":
			idx := p.CurrentTrack()
			file := p.CurrentFile()
			pos := p.Position()
			dur := p.TrackDuration()
			vol := int(p.Volume()*100 + 0.5)
			fmt.Printf("[%d/%d] %s  %s / %s  vol %d%%\n",
				idx+1, len(files), filepath.Base(file),
				formatTime(pos), formatTime(dur), vol)
		case "h", "?", "help":
			printHelp()
		case "q", "quit", "exit":
			log.Println("Bye!")
			return
		default:
			fmt.Println("Unknown command. Type 'h' for help")
		}

		if p.IsStopped() && p.IsEOF() {
			log.Println("Playback stopped. Press 'q' to quit")
		}
	}
}
