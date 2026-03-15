package pipewire

/*
#cgo pkg-config: libpipewire-0.3
#include <pipewire/pipewire.h>
#include <spa/param/audio/format-utils.h>
#include <spa/param/props.h>
#include <spa/utils/hook.h>
#include <stdlib.h>

// Callback function that will be called from Go
extern void go_on_process_callback(void *userdata);

// C wrapper for the process callback
static void process_callback(void *userdata) {
    go_on_process_callback(userdata);
}

// Helper to set up the stream with callbacks
static struct pw_stream_events* create_stream_events() {
    struct pw_stream_events *events = malloc(sizeof(struct pw_stream_events));
    memset(events, 0, sizeof(struct pw_stream_events));
    events->version = PW_VERSION_STREAM_EVENTS;
    events->process = process_callback;
    return events;
}

// Helper to create a listener hook
static struct spa_hook* create_listener() {
    struct spa_hook *hook = malloc(sizeof(struct spa_hook));
    memset(hook, 0, sizeof(struct spa_hook));
    return hook;
}

// Helper to create properties (avoids variadic C function issues)
static struct pw_properties* create_stream_properties() {
    struct pw_properties *props = pw_properties_new(
        PW_KEY_MEDIA_TYPE, "Audio",
        PW_KEY_MEDIA_CATEGORY, "Playback",
        PW_KEY_MEDIA_ROLE, "Music",
        NULL
    );
    return props;
}

// Helper to build audio format parameters
static const struct spa_pod* build_audio_format(
    uint32_t sample_rate,
    uint32_t channels,
    uint8_t *buffer,
    uint32_t buffer_size
) {
    struct spa_pod_builder builder;
    struct spa_audio_info_raw info;

    spa_zero(info);
    info.format = SPA_AUDIO_FORMAT_F32;
    info.channels = channels;
    info.rate = sample_rate;

    if (channels == 1) {
        info.position[0] = SPA_AUDIO_CHANNEL_MONO;
    } else if (channels == 2) {
        info.position[0] = SPA_AUDIO_CHANNEL_FL;
        info.position[1] = SPA_AUDIO_CHANNEL_FR;
    }

    builder = SPA_POD_BUILDER_INIT(buffer, buffer_size);
    return spa_format_audio_raw_build(&builder, SPA_PARAM_EnumFormat, &info);
}
*/
import "C"
import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Stream represents a PipeWire stream
type Stream struct {
	pw       *C.struct_pw_stream
	loop     *C.struct_pw_thread_loop
	ctx      *C.struct_pw_context
	listener *C.struct_spa_hook
	events   *C.struct_pw_stream_events
	userData unsafe.Pointer
	callback ProcessCallback
	mu       sync.Mutex
}

// ProcessCallback is called when the stream needs data
type ProcessCallback func(buffer []byte, frames int) int

var (
	streamRegistry = make(map[*C.struct_pw_stream]*Stream)
	registryMu     sync.RWMutex
)

// Init initializes the PipeWire library
func Init() error {
	C.pw_init(nil, nil)
	return nil
}

// Deinit deinitializes the PipeWire library
func Deinit() {
	C.pw_deinit()
}

// AudioFormat represents audio format parameters
type AudioFormat struct {
	SampleRate int
	Channels   int
}

// NewStream creates a new PipeWire stream for audio playback
func NewStream(name string, format AudioFormat, callback ProcessCallback) (*Stream, error) {
	if callback == nil {
		return nil, errors.New("callback cannot be nil")
	}

	// Create thread loop
	loop := C.pw_thread_loop_new(C.CString(name), nil)
	if loop == nil {
		return nil, errors.New("failed to create thread loop")
	}

	// Get the loop's context
	loopPtr := C.pw_thread_loop_get_loop(loop)
	ctx := C.pw_context_new(loopPtr, nil, 0)
	if ctx == nil {
		C.pw_thread_loop_destroy(loop)
		return nil, errors.New("failed to create context")
	}

	// Create stream
	props := C.create_stream_properties()

	// Lock the loop before creating stream
	C.pw_thread_loop_lock(loop)

	streamName := C.CString(name)
	core := C.pw_context_connect(ctx, nil, 0)
	if core == nil {
		C.pw_thread_loop_unlock(loop)
		C.pw_context_destroy(ctx)
		C.pw_thread_loop_destroy(loop)
		C.free(unsafe.Pointer(streamName))
		return nil, errors.New("failed to connect context")
	}

	pwStream := C.pw_stream_new(core, streamName, props)
	C.free(unsafe.Pointer(streamName))

	if pwStream == nil {
		C.pw_thread_loop_unlock(loop)
		C.pw_context_destroy(ctx)
		C.pw_thread_loop_destroy(loop)
		return nil, errors.New("failed to create stream")
	}

	// Create listener and events
	listener := C.create_listener()
	events := C.create_stream_events()

	stream := &Stream{
		pw:       pwStream,
		loop:     loop,
		ctx:      ctx,
		listener: listener,
		events:   events,
		callback: callback,
	}

	// Register the stream in our global registry
	registryMu.Lock()
	streamRegistry[pwStream] = stream
	registryMu.Unlock()

	// Set up process callback
	C.pw_stream_add_listener(pwStream, listener, events, unsafe.Pointer(pwStream))

	C.pw_thread_loop_unlock(loop)

	runtime.SetFinalizer(stream, (*Stream).Destroy)

	return stream, nil
}

// Connect connects the stream to PipeWire
func (s *Stream) Connect(format AudioFormat) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	C.pw_thread_loop_lock(s.loop)

	// Build audio format parameters using C helper
	buffer := (*C.uint8_t)(C.malloc(1024))
	defer C.free(unsafe.Pointer(buffer))

	pod := C.build_audio_format(
		C.uint32_t(format.SampleRate),
		C.uint32_t(format.Channels),
		buffer,
		1024,
	)

	params := [1]*C.struct_spa_pod{(*C.struct_spa_pod)(unsafe.Pointer(pod))}

	// Connect stream
	ret := C.pw_stream_connect(
		s.pw,
		C.PW_DIRECTION_OUTPUT,
		C.PW_ID_ANY,
		C.PW_STREAM_FLAG_AUTOCONNECT|
			C.PW_STREAM_FLAG_MAP_BUFFERS|
			C.PW_STREAM_FLAG_RT_PROCESS,
		&params[0],
		1,
	)

	C.pw_thread_loop_unlock(s.loop)

	if ret < 0 {
		return fmt.Errorf("failed to connect stream: %d", ret)
	}

	// Start the thread loop
	if C.pw_thread_loop_start(s.loop) < 0 {
		return errors.New("failed to start thread loop")
	}

	return nil
}

// Destroy destroys the stream and frees resources
func (s *Stream) Destroy() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pw != nil {
		registryMu.Lock()
		delete(streamRegistry, s.pw)
		registryMu.Unlock()

		C.pw_thread_loop_stop(s.loop)

		// Remove listener
		if s.listener != nil {
			C.spa_hook_remove(s.listener)
			C.free(unsafe.Pointer(s.listener))
			s.listener = nil
		}

		// Free events
		if s.events != nil {
			C.free(unsafe.Pointer(s.events))
			s.events = nil
		}

		C.pw_stream_destroy(s.pw)
		if s.ctx != nil {
			C.pw_context_destroy(s.ctx)
		}
		C.pw_thread_loop_destroy(s.loop)
		s.pw = nil
		s.loop = nil
		s.ctx = nil
	}
}

//export go_on_process_callback
func go_on_process_callback(userdata unsafe.Pointer) {
	pwStream := (*C.struct_pw_stream)(userdata)

	registryMu.RLock()
	stream, ok := streamRegistry[pwStream]
	registryMu.RUnlock()

	if !ok || stream.callback == nil {
		return
	}

	// Get buffer
	buf := C.pw_stream_dequeue_buffer(pwStream)
	if buf == nil {
		return
	}
	defer C.pw_stream_queue_buffer(pwStream, buf)

	// Get the data buffer
	if buf.buffer.n_datas == 0 {
		return
	}

	data := (*C.struct_spa_data)(unsafe.Pointer(uintptr(unsafe.Pointer(buf.buffer.datas))))
	if data.data == nil {
		return
	}

	// Calculate requested frames
	stride := C.int(4 * 2) // 4 bytes per sample (float32) * 2 channels
	maxFrames := C.int(data.maxsize) / stride
	requestedFrames := buf.requested

	if requestedFrames == 0 {
		requestedFrames = C.uint64_t(maxFrames)
	}

	if C.int(requestedFrames) > maxFrames {
		requestedFrames = C.uint64_t(maxFrames)
	}

	// Create Go slice backed by C memory
	bufferSize := int(requestedFrames) * int(stride)
	goBuffer := unsafe.Slice((*byte)(data.data), bufferSize)

	// Call the Go callback
	written := stream.callback(goBuffer, int(requestedFrames))

	// Update buffer metadata
	data.chunk.offset = 0
	data.chunk.stride = stride
	data.chunk.size = C.uint32_t(written * int(stride))
}
