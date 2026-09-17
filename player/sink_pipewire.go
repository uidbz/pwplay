//go:build linux && !android

package player

import (
	"sync"

	"github.com/uidbz/pwplay/pipewire"
)

// pwInitOnce guards pipewire.Init: pw_init may be called repeatedly, but
// pwplay's own binaries already call it explicitly, so do it exactly once
// here for embedders (e.g. tie-audio) that never touch the pipewire package.
var pwInitOnce sync.Once

// platformSink is the Linux default sink factory: a PipeWire stream.
func platformSink(name string, format Format, cb ProcessCallback, opts SinkOptions) (Sink, error) {
	pwInitOnce.Do(func() { _ = pipewire.Init() })
	stream, err := pipewire.NewStreamWithOptions(name,
		pipewire.AudioFormat{SampleRate: format.SampleRate, Channels: format.Channels},
		pipewire.ProcessCallback(cb),
		pipewire.StreamOptions{Passthrough: opts.Passthrough, Exclusive: opts.Exclusive})
	if err != nil {
		return nil, err
	}
	return pipewireStream{stream}, nil
}

// pipewireStream adapts *pipewire.Stream to the Sink interface (Connect takes
// pipewire.AudioFormat rather than the platform-neutral Format).
type pipewireStream struct{ *pipewire.Stream }

func (s pipewireStream) Connect(format Format) error {
	return s.Stream.Connect(pipewire.AudioFormat{SampleRate: format.SampleRate, Channels: format.Channels})
}
