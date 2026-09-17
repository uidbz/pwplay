//go:build !linux && !android

package player

import "fmt"

// platformSink has no implementation on platforms without a sink: pwplay
// targets Linux (PipeWire) and Android (OpenSL ES). The player still works
// for playlist management; the first track load fails with this error.
func platformSink(name string, format Format, cb ProcessCallback, opts SinkOptions) (Sink, error) {
	return nil, fmt.Errorf("no audio sink on this platform (supported: linux, android)")
}
