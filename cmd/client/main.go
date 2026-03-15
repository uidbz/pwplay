package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"git.sr.ht/~uid/pwplay/client"
)

// --- terminal raw mode (POSIX) ---

type termios struct {
	Iflag  uint32
	Oflag  uint32
	Cflag  uint32
	Lflag  uint32
	Cc     [20]byte
	Ispeed uint32
	Ospeed uint32
}

var origTermios termios

const (
	ioctlTCGETS  = 0x5401
	ioctlTCSETSF = 0x5404
)

func tcget(fd uintptr, t *termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlTCGETS, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}

func tcset(fd uintptr, t *termios) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlTCSETSF, uintptr(unsafe.Pointer(t)))
	if e != 0 {
		return e
	}
	return nil
}

func enableRawMode() error {
	if err := tcget(os.Stdin.Fd(), &origTermios); err != nil {
		return err
	}
	raw := origTermios
	raw.Iflag &^= syscall.ICRNL | syscall.IXON | syscall.BRKINT | syscall.INPCK | syscall.ISTRIP
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	return tcset(os.Stdin.Fd(), &raw)
}

func disableRawMode() {
	tcset(os.Stdin.Fd(), &origTermios)
}

// --- display ---

const (
	clearScreen = "\033[2J\033[H"
	cursorHome  = "\033[H"
	bold        = "\033[1m"
	dim         = "\033[2m"
	reset       = "\033[0m"
	cyan        = "\033[36m"
	green       = "\033[32m"
	yellow      = "\033[33m"
	red         = "\033[31m"
	white       = "\033[37m"
	magenta     = "\033[35m"
	hideCursor  = "\033[?25l"
	showCursor  = "\033[?25h"
	clearLine   = "\033[2K"
)

func formatTime(sec float64) string {
	if sec < 0 {
		return "--:--"
	}
	m := int(sec) / 60
	s := int(sec) % 60
	return fmt.Sprintf("%d:%02d", m, s)
}

func progressBar(pos, dur float64, width int) string {
	if dur <= 0 {
		return strings.Repeat("-", width)
	}
	ratio := pos / dur
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("=", filled)
	if filled < width {
		bar += ">"
		bar += strings.Repeat("-", width-filled-1)
	}
	return bar
}

func stateString(s *client.Status) string {
	if s.Stopped {
		return red + "STOPPED" + reset
	}
	if s.Paused {
		return yellow + "PAUSED" + reset
	}
	if s.Playing {
		return green + "PLAYING" + reset
	}
	return dim + "UNKNOWN" + reset
}

func render(s *client.Status, msg string, showPlaylist bool) {
	var buf strings.Builder

	buf.WriteString(cursorHome)

	// Header
	buf.WriteString(clearLine)
	buf.WriteString(bold + cyan + "  PipeWire Audio Client" + reset + "\n")
	buf.WriteString(clearLine)
	buf.WriteString(dim + "  " + strings.Repeat("-", 50) + reset + "\n")

	// State
	buf.WriteString(clearLine)
	buf.WriteString("  State: " + stateString(s) + "\n")

	// Current track
	buf.WriteString(clearLine)
	buf.WriteString(fmt.Sprintf("  Track: %s[%d/%d]%s %s%s%s\n",
		dim, s.CurrentTrack+1, s.TotalTracks, reset,
		bold+white, s.CurrentFile, reset))

	// Time + progress bar + volume
	pos := formatTime(s.Position)
	dur := formatTime(s.TrackDuration)
	bar := progressBar(s.Position, s.TrackDuration, 36)
	vol := int(s.Volume*100 + 0.5)
	buf.WriteString(clearLine)
	buf.WriteString(fmt.Sprintf("  %s %s[%s]%s %s  %svol %d%%%s\n",
		pos, dim, bar, reset, dur, dim, vol, reset))

	buf.WriteString(clearLine + "\n")

	// Playlist (toggled)
	if showPlaylist {
		buf.WriteString(clearLine)
		buf.WriteString(bold + "  Playlist:" + reset + "\n")
		for i, f := range s.Playlist {
			buf.WriteString(clearLine)
			marker := "   "
			nameColor := dim
			if i == s.CurrentTrack {
				marker = cyan + " > " + reset
				nameColor = bold + white
			}
			buf.WriteString(fmt.Sprintf("%s%s%d. %s%s\n",
				marker, nameColor, i+1, filepath.Base(f), reset))
		}
		buf.WriteString(clearLine + "\n")
	}

	// Message line
	buf.WriteString(clearLine)
	if msg != "" {
		buf.WriteString("  " + magenta + msg + reset + "\n")
	} else {
		buf.WriteString("\n")
	}

	// Controls
	buf.WriteString(clearLine)
	buf.WriteString(dim + "  [space]" + reset + " play/pause  " +
		dim + "[s]" + reset + " stop  " +
		dim + "[n]" + reset + " next  " +
		dim + "[p]" + reset + " prev  " +
		dim + "[f/b]" + reset + " seek +/-10s\n")
	buf.WriteString(clearLine)
	buf.WriteString(dim + "  [F/B]" + reset + " seek +/-30s  " +
		dim + "[1-9]" + reset + " seek to 10-90%  " +
		dim + "[0]" + reset + " seek to start\n")
	buf.WriteString(clearLine)
	buf.WriteString(dim + "  [v/V]" + reset + " vol down/up  " +
		dim + "[m]" + reset + " mute  " +
		dim + "[l]" + reset + " playlist  " +
		dim + "[a]" + reset + " add  " +
		dim + "[r]" + reset + " remove  " +
		dim + "[q]" + reset + " quit\n")

	// Clear any leftover lines below
	for i := 0; i < 20; i++ {
		buf.WriteString(clearLine + "\n")
	}

	fmt.Print(buf.String())
}

// readLine reads a line of input from keyCh, with echo and basic line editing.
// Must be called while the key reader goroutine is active.
func readLine(prompt string, keyCh <-chan byte) (string, bool) {
	disableRawMode()
	fmt.Print(showCursor)
	fmt.Print(prompt)

	buf := make([]byte, 0, 256)
	for {
		b, ok := <-keyCh
		if !ok {
			enableRawMode()
			fmt.Print(hideCursor)
			return "", false
		}
		if b == '\n' || b == '\r' {
			break
		}
		if b == 127 || b == 8 { // backspace
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Print("\b \b")
			}
			continue
		}
		if b == 3 || b == 27 { // ctrl-c or escape -> cancel
			enableRawMode()
			fmt.Print(hideCursor)
			return "", false
		}
		buf = append(buf, b)
		fmt.Print(string(b))
	}
	fmt.Println()
	enableRawMode()
	fmt.Print(hideCursor)
	return string(buf), true
}

func main() {
	host := "localhost:8080"
	if len(os.Args) >= 2 {
		host = os.Args[1]
	}
	var baseURL string
	if !strings.Contains(host, "://") {
		baseURL = "http://" + host
	} else {
		baseURL = host
	}

	c := client.New(baseURL)

	// Check connectivity
	_, err := c.Status()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Cannot connect to webservice at %s: %v\n", baseURL, err)
		fmt.Fprintf(os.Stderr, "Usage: %s [host:port]\n", os.Args[0])
		os.Exit(1)
	}

	if err := enableRawMode(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to set raw terminal mode: %v\n", err)
		os.Exit(1)
	}
	defer disableRawMode()

	// Clean up terminal on exit
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Print(showCursor)
		disableRawMode()
		fmt.Println("\nBye!")
		os.Exit(0)
	}()

	fmt.Print(clearScreen + hideCursor)

	// Key input channel
	keyCh := make(chan byte, 16)
	go func() {
		buf := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				return
			}
			keyCh <- buf[0]
		}
	}()

	msg := "Connected to " + baseURL
	showPlaylist := false
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	// Initial render
	if s, err := c.Status(); err == nil {
		render(s, msg, showPlaylist)
	}

	for {
		select {
		case key := <-keyCh:
			switch key {
			case ' ':
				s, _ := c.Status()
				if s != nil && s.Paused {
					c.Play()
					msg = "Playing"
				} else {
					c.Pause()
					msg = "Paused"
				}
			case 's':
				c.Stop()
				msg = "Stopped"
			case 'n':
				c.Next()
				msg = "Next track"
			case 'p':
				c.Previous()
				msg = "Previous track"
			case 'f', '+':
				c.SeekRelative(10)
				msg = "Seek +10s"
			case 'b', '-':
				c.SeekRelative(-10)
				msg = "Seek -10s"
			case 'F':
				c.SeekRelative(30)
				msg = "Seek +30s"
			case 'B':
				c.SeekRelative(-30)
				msg = "Seek -30s"
			case '0':
				c.Seek(0)
				msg = "Seek to start"
			case '1', '2', '3', '4', '5', '6', '7', '8', '9':
				pct := float64(key-'0') * 10
				s, _ := c.Status()
				if s != nil && s.TrackDuration > 0 {
					target := s.TrackDuration * pct / 100.0
					c.Seek(target)
					msg = fmt.Sprintf("Seek to %d%%", int(pct))
				} else {
					msg = "Cannot seek: unknown duration"
				}
			case 'v':
				s, _ := c.Status()
				if s != nil {
					newVol := s.Volume - 0.05
					if newVol < 0 {
						newVol = 0
					}
					c.SetVolume(newVol)
					msg = fmt.Sprintf("Volume %d%%", int(newVol*100+0.5))
				}
			case 'V':
				s, _ := c.Status()
				if s != nil {
					newVol := s.Volume + 0.05
					if newVol > 2 {
						newVol = 2
					}
					c.SetVolume(newVol)
					msg = fmt.Sprintf("Volume %d%%", int(newVol*100+0.5))
				}
			case 'm':
				s, _ := c.Status()
				if s != nil {
					if s.Volume > 0 {
						c.SetVolume(0)
						msg = "Muted"
					} else {
						c.SetVolume(1.0)
						msg = "Volume 100%"
					}
				}
			case 'l':
				showPlaylist = !showPlaylist
				if showPlaylist {
					fmt.Print(clearScreen)
					msg = "Playlist shown"
				} else {
					fmt.Print(clearScreen)
					msg = "Playlist hidden"
				}
			case 'a':
				fmt.Print(clearScreen + cursorHome)
				if path, ok := readLine("  Add track/directory path: ", keyCh); ok && path != "" {
					if err := c.AddTracks(path); err != nil {
						msg = "Add failed: " + err.Error()
					} else {
						msg = "Added: " + filepath.Base(path)
					}
				} else {
					msg = "Add cancelled"
				}
				fmt.Print(clearScreen)
			case 'r':
				fmt.Print(clearScreen + cursorHome)
				if idxStr, ok := readLine("  Remove track # (1-based): ", keyCh); ok && idxStr != "" {
					if idx, err := strconv.Atoi(idxStr); err == nil {
						c.RemoveTrack(idx - 1)
						msg = fmt.Sprintf("Removed track #%d", idx)
					} else {
						msg = "Invalid track number"
					}
				} else {
					msg = "Remove cancelled"
				}
				fmt.Print(clearScreen)
			case 'q', 3: // q or ctrl-c
				fmt.Print(showCursor + clearScreen + cursorHome)
				fmt.Println("Bye!")
				return
			}

			// Immediate re-render after key
			if s, err := c.Status(); err == nil {
				render(s, msg, showPlaylist)
			}

		case <-ticker.C:
			if s, err := c.Status(); err == nil {
				render(s, msg, showPlaylist)
			} else {
				msg = red + "Connection lost: " + err.Error() + reset
			}
		}
	}
}
