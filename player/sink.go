package player

// Format describes the PCM format a sink must accept: interleaved float32
// samples at SampleRate Hz with Channels channels.
type Format struct {
	SampleRate int
	Channels   int
}

// SinkOptions carries optional sink behavior hints. Passthrough and
// Exclusive are PipeWire-specific; other sinks ignore them.
type SinkOptions struct {
	// Passthrough disables channel remixing and requests the audio device
	// run at the stream's native sample rate (bit-perfect output).
	Passthrough bool
	// Exclusive requests sole access to the audio sink device.
	Exclusive bool
}

// ProcessCallback is called by the sink when it needs more audio. buffer
// holds frames*channels float32 slots; the callback fills it and returns the
// number of frames written. It runs on the sink's realtime audio thread, so
// it must not block or allocate excessively.
type ProcessCallback func(buffer []byte, frames int) int

// Sink is an audio output the player pushes decoded PCM into. A sink is
// created with a callback and started with Connect; the stream format is
// fixed for the sink's lifetime (the player creates the sink lazily on the
// first track load, so the sink adopts that track's format).
type Sink interface {
	// Connect starts the audio stream at the given format.
	Connect(format Format) error
	// Destroy stops the stream and frees all resources.
	Destroy()
}

// SinkFactory builds a sink that will invoke cb when it needs samples.
// PlayerOptions.Sink is nil for the platform default (PipeWire on Linux,
// OpenSL ES on Android).
type SinkFactory func(name string, format Format, cb ProcessCallback, opts SinkOptions) (Sink, error)
