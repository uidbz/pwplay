//go:build android

package player

/*
#cgo LDFLAGS: -lOpenSLES -llog

#include <SLES/OpenSLES.h>
#include <SLES/OpenSLES_Android.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <android/log.h>

#define LOG_ERR(...) __android_log_print(ANDROID_LOG_ERROR, "pwplay", __VA_ARGS__)

extern void goOpenslBufferDone(uintptr_t handle);

// opensl_engine bundles every OpenSL ES object the sink needs, so the Go
// side holds a single pointer.
typedef struct {
	SLObjectItf engineObj;
	SLEngineItf engineItf;
	SLObjectItf mixObj;
	SLObjectItf playerObj;
	SLPlayItf playItf;
	SLAndroidSimpleBufferQueueItf bqItf;
} opensl_engine;

static void opensl_bq_callback(SLAndroidSimpleBufferQueueItf caller, void *pContext) {
	(void)caller;
	goOpenslBufferDone((uintptr_t)pContext);
}

static void opensl_destroy(opensl_engine *e);

// opensl_create builds the whole player graph (engine, output mix, buffer
// queue player) but does not start playback. Returns NULL on failure and
// stores the failing SL result code in *err.
static opensl_engine *opensl_create(SLuint32 rateMilliHz, SLint32 channels, SLuint32 numBuffers, uintptr_t context, SLresult *err) {
	opensl_engine *e = calloc(1, sizeof(opensl_engine));
	if (e == NULL) {
		*err = SL_RESULT_MEMORY_FAILURE;
		return NULL;
	}

	*err = slCreateEngine(&e->engineObj, 0, NULL, 0, NULL, NULL);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }
	*err = (*e->engineObj)->Realize(e->engineObj, SL_BOOLEAN_FALSE);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }
	*err = (*e->engineObj)->GetInterface(e->engineObj, SL_IID_ENGINE, &e->engineItf);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }

	*err = (*e->engineItf)->CreateOutputMix(e->engineItf, &e->mixObj, 0, NULL, NULL);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }
	*err = (*e->mixObj)->Realize(e->mixObj, SL_BOOLEAN_FALSE);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }

	SLDataLocator_AndroidSimpleBufferQueue locBq = {SL_DATALOCATOR_ANDROIDSIMPLEBUFFERQUEUE, numBuffers};
	SLAndroidDataFormat_PCM_EX fmt;
	memset(&fmt, 0, sizeof(fmt));
	fmt.formatType = SL_ANDROID_DATAFORMAT_PCM_EX;
	fmt.numChannels = channels;
	fmt.sampleRate = rateMilliHz;
	fmt.bitsPerSample = SL_PCMSAMPLEFORMAT_FIXED_32;
	fmt.containerSize = SL_PCMSAMPLEFORMAT_FIXED_32;
	if (channels == 1) {
		fmt.channelMask = SL_SPEAKER_FRONT_CENTER;
	} else if (channels == 2) {
		fmt.channelMask = SL_SPEAKER_FRONT_LEFT | SL_SPEAKER_FRONT_RIGHT;
	} else {
		fmt.channelMask = 0; // let OpenSL pick a default layout
	}
	fmt.endianness = SL_BYTEORDER_LITTLEENDIAN;
	fmt.representation = SL_ANDROID_PCM_REPRESENTATION_FLOAT;

	SLDataSource src = {&locBq, &fmt};
	SLDataLocator_OutputMix locOut = {SL_DATALOCATOR_OUTPUTMIX, e->mixObj};
	SLDataSink snk = {&locOut, NULL};

	SLInterfaceID ids[3] = {SL_IID_PLAY, SL_IID_ANDROIDSIMPLEBUFFERQUEUE, SL_IID_ANDROIDCONFIGURATION};
	SLboolean req[3] = {SL_BOOLEAN_TRUE, SL_BOOLEAN_TRUE, SL_BOOLEAN_TRUE};
	*err = (*e->engineItf)->CreateAudioPlayer(e->engineItf, &e->playerObj, &src, &snk, 3, ids, req);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }

	// Route to the media stream (the default, but be explicit) before Realize.
	SLAndroidConfigurationItf config;
	*err = (*e->playerObj)->GetInterface(e->playerObj, SL_IID_ANDROIDCONFIGURATION, &config);
	if (*err == SL_RESULT_SUCCESS) {
		SLint32 streamType = SL_ANDROID_STREAM_MEDIA;
		(*config)->SetConfiguration(config, SL_ANDROID_KEY_STREAM_TYPE, &streamType, sizeof(SLint32));
	}

	*err = (*e->playerObj)->Realize(e->playerObj, SL_BOOLEAN_FALSE);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }
	*err = (*e->playerObj)->GetInterface(e->playerObj, SL_IID_PLAY, &e->playItf);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }
	*err = (*e->playerObj)->GetInterface(e->playerObj, SL_IID_ANDROIDSIMPLEBUFFERQUEUE, &e->bqItf);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }
	*err = (*e->bqItf)->RegisterCallback(e->bqItf, opensl_bq_callback, (void *)context);
	if (*err != SL_RESULT_SUCCESS) { opensl_destroy(e); return NULL; }

	return e;
}

static SLresult opensl_enqueue(opensl_engine *e, const void *buf, SLuint32 bytes) {
	return (*e->bqItf)->Enqueue(e->bqItf, buf, bytes);
}

// opensl_set_playing starts or stops the player.
static SLresult opensl_set_playing(opensl_engine *e, int playing) {
	return (*e->playItf)->SetPlayState(e->playItf, playing ? SL_PLAYSTATE_PLAYING : SL_PLAYSTATE_STOPPED);
}

// opensl_destroy stops playback and destroys every object. Destroying the
// player blocks until any in-flight buffer-queue callback has returned, so
// the Go side can free its buffers once this returns.
static void opensl_destroy(opensl_engine *e) {
	if (e == NULL) {
		return;
	}
	if (e->playItf != NULL) {
		(*e->playItf)->SetPlayState(e->playItf, SL_PLAYSTATE_STOPPED);
	}
	if (e->bqItf != NULL) {
		(*e->bqItf)->Clear(e->bqItf);
	}
	if (e->playerObj != NULL) {
		(*e->playerObj)->Destroy(e->playerObj);
	}
	if (e->mixObj != NULL) {
		(*e->mixObj)->Destroy(e->mixObj);
	}
	if (e->engineObj != NULL) {
		(*e->engineObj)->Destroy(e->engineObj);
	}
	free(e);
}
*/
import "C"

import (
	"fmt"
	"sync"
	"unsafe"

	"runtime/cgo"
)

// openslNumBuffers and openslFramesPerBuffer size the buffer queue. The
// engine's ring buffer holds ~3 s ahead of the sink, so the callback only
// copies from the ring (no decoding); 4 x 1024 frames (~93 ms at 44.1 kHz)
// keeps pause/seek responsive without risking underruns.
const (
	openslNumBuffers      = 4
	openslFramesPerBuffer = 1024
)

// openslSlot is one queued buffer: C-allocated so it stays valid while
// OpenSL holds it, with its enqueue size precomputed.
type openslSlot struct {
	buf   unsafe.Pointer
	bytes C.SLuint32
}

// openslSink plays float32 PCM through OpenSL ES (Android).
type openslSink struct {
	cb     ProcessCallback
	format Format
	eng    *C.opensl_engine
	handle cgo.Handle

	mu     sync.Mutex
	closed bool
	slots  []*openslSlot // FIFO: the front slot is the one OpenSL just finished
}

// platformSink is the Android default sink factory: an OpenSL ES player.
// The whole graph is created here (at the track's native format); Connect
// starts playback.
func platformSink(name string, format Format, cb ProcessCallback, opts SinkOptions) (Sink, error) {
	if format.SampleRate <= 0 || format.Channels <= 0 {
		return nil, fmt.Errorf("opensl: invalid format %+v", format)
	}
	s := &openslSink{cb: cb, format: format}
	for i := 0; i < openslNumBuffers; i++ {
		slot := &openslSlot{bytes: C.SLuint32(openslFramesPerBuffer * format.Channels * 4)}
		slot.buf = C.malloc(C.size_t(slot.bytes))
		if slot.buf == nil {
			for _, prev := range s.slots {
				C.free(prev.buf)
			}
			return nil, fmt.Errorf("opensl: out of memory allocating buffers")
		}
		s.slots = append(s.slots, slot)
	}
	s.handle = cgo.NewHandle(s)

	var res C.SLresult
	eng := C.opensl_create(C.SLuint32(format.SampleRate*1000), C.SLint32(format.Channels),
		C.SLuint32(openslNumBuffers), C.uintptr_t(s.handle), &res)
	if eng == nil {
		s.handle.Delete()
		for _, slot := range s.slots {
			C.free(slot.buf)
		}
		return nil, fmt.Errorf("opensl: player creation failed (result %d)", int(res))
	}
	s.eng = eng
	return s, nil
}

// Connect primes the buffer queue and starts playback.
func (s *openslSink) Connect(format Format) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("opensl: Connect after Destroy")
	}
	// Prime every slot with real audio (the callback is a plain ring-buffer
	// copy, so this is cheap) before starting the play state.
	for _, slot := range s.slots {
		s.fill(slot)
		if res := C.opensl_enqueue(s.eng, slot.buf, slot.bytes); res != C.SL_RESULT_SUCCESS {
			return fmt.Errorf("opensl: initial enqueue failed (result %d)", int(res))
		}
	}
	if res := C.opensl_set_playing(s.eng, 1); res != C.SL_RESULT_SUCCESS {
		return fmt.Errorf("opensl: SetPlayState failed (result %d)", int(res))
	}
	return nil
}

// fill runs the player's process callback into the slot's C buffer.
func (s *openslSink) fill(slot *openslSlot) {
	n := int(slot.bytes)
	s.cb(unsafe.Slice((*byte)(slot.buf), n), openslFramesPerBuffer)
}

// bufferDone is called (serialized by OpenSL) each time the player finishes
// a buffer: refill it with fresh audio and re-enqueue it.
func (s *openslSink) bufferDone() {
	s.mu.Lock()
	if s.closed || len(s.slots) == 0 {
		s.mu.Unlock()
		return
	}
	slot := s.slots[0]
	s.slots = s.slots[1:]
	s.mu.Unlock()

	s.fill(slot)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if res := C.opensl_enqueue(s.eng, slot.buf, slot.bytes); res != C.SL_RESULT_SUCCESS {
		s.closed = true
		return
	}
	s.slots = append(s.slots, slot)
}

// Destroy stops playback and frees every resource. Object destruction blocks
// until any in-flight callback has returned, so buffers are freed safely.
func (s *openslSink) Destroy() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	C.opensl_destroy(s.eng)
	s.eng = nil

	s.mu.Lock()
	for _, slot := range s.slots {
		C.free(slot.buf)
	}
	s.slots = nil
	s.mu.Unlock()
	s.handle.Delete()
}

//export goOpenslBufferDone
func goOpenslBufferDone(handle C.uintptr_t) {
	cgo.Handle(handle).Value().(*openslSink).bufferDone()
}
