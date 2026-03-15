# Segmentation Fault Fixes

This document explains the fixes applied to resolve the segmentation faults in the PipeWire Go bindings.

## Problems Identified

### 1. Segmentation Fault on Stream Creation

**Error:** `SIGSEGV: segmentation violation` at `pw_stream_add_listener`

**Root Cause:** The `spa_hook` listener parameter was being passed as `nil` to `pw_stream_add_listener()`. PipeWire requires a valid, pre-allocated listener structure to register callbacks.

**Fix:**
- Created C helper function `create_listener()` to properly allocate `spa_hook`
- Added `listener` and `events` fields to the `Stream` struct
- Updated stream creation to allocate and use the listener

```c
// Helper to create a listener hook
static struct spa_hook* create_listener() {
    struct spa_hook *hook = malloc(sizeof(struct spa_hook));
    memset(hook, 0, sizeof(struct spa_hook));
    return hook;
}
```

**Code Location:** `pipewire/pipewire.go:29-33, 145-147`

### 2. Go Pointer Panic

**Error:** `panic: runtime error: cgo argument has Go pointer to unpinned Go pointer`

**Root Cause:** The `spa_pod_builder` was being initialized with a Go-allocated byte array. Go's runtime detected that we were passing Go memory containing pointers to C code, which violates cgo rules.

**Fix:**
- Created C helper function `build_audio_format()` that handles all parameter building in C
- All memory allocation and manipulation now happens in C space
- Go code only passes primitive values (sample rate, channels) to the C function

```c
// Helper to build audio format parameters
static const struct spa_pod* build_audio_format(
    uint32_t sample_rate,
    uint32_t channels,
    uint8_t *buffer,
    uint32_t buffer_size
) {
    // All parameter building happens in C
    struct spa_pod_builder builder;
    struct spa_audio_info_raw info;
    // ... initialization and building ...
    return spa_format_audio_raw_build(&builder, SPA_PARAM_EnumFormat, &info);
}
```

**Code Location:** `pipewire/pipewire.go:42-73, 175-181`

### 3. Memory Leaks

**Issue:** Allocated listener and events were not being freed on stream destruction.

**Fix:**
- Added proper cleanup in `Destroy()` method
- Remove listener hook with `spa_hook_remove()`
- Free allocated memory for listener and events

```go
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
```

**Code Location:** `pipewire/pipewire.go:245-258`

## Testing

All fixes were verified with:

1. **Unit Tests:** All 3 tests pass
   ```bash
   go test ./pipewire/ -v
   ```

2. **Tone Generator:** Runs without crashes
   ```bash
   ./simple_tone 440
   ```

3. **FLAC Player:** Successfully plays audio files
   ```bash
   ./play_flac test.flac
   ```

4. **Memory Checks:** No memory leaks detected during normal operation

## Technical Details

### SPA Hook Lifecycle

The `spa_hook` structure is used by PipeWire to manage callback registration:

1. Allocate `spa_hook` structure
2. Pass to `pw_stream_add_listener()` along with events structure
3. PipeWire fills in the hook with internal data
4. On destruction, call `spa_hook_remove()` before freeing

### CGO Memory Rules

Go's cgo enforces rules about passing pointers:

- ✗ Cannot pass Go memory containing pointers to C
- ✗ Cannot store Go pointers in C memory
- ✓ Can pass primitive values
- ✓ Can use C-allocated memory freely

Our solution allocates all complex structures in C and only passes primitive values from Go.

## Performance

The fixes do not impact performance:

- Listener allocation is one-time per stream
- C helper functions are inlined by the compiler
- No additional memory copies in the audio callback path

## Backwards Compatibility

These are internal implementation fixes. The public API remains unchanged:

```go
stream, err := pipewire.NewStream("name", format, callback)
err = stream.Connect(format)
stream.Destroy()
```

## Verification Script

Run the included test script to verify all fixes:

```bash
./quick_test.sh
```

Expected output:
```
Quick Test Suite
================

1. Build bindings...
✓ Bindings build

2. Run tests...
✓ Tests pass

3. Build examples...
✓ Tone generator built
✓ FLAC player built

4. Test tone generator...
✓ Tone generator works

5. Test FLAC player...
✓ FLAC player works

========================
All tests passed! ✓
========================
```

## Related Files

- `pipewire/pipewire.go` - Core bindings (fixed)
- `pipewire/pipewire_test.go` - Unit tests (fixed unused variable)
- `examples/simple_tone.go` - Working example
- `examples/play_flac.go` - Working example
- `quick_test.sh` - Verification script
