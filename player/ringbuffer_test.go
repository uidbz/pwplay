package player

import (
	"sync"
	"testing"
)

func BenchmarkRingBufferWrite(b *testing.B) {
	rb := NewRingBuffer(8192)
	samples := make([]float32, 512)
	for i := range samples {
		samples[i] = float32(i) * 0.01
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Write(samples)
		// Simulate consumption
		if i%16 == 0 {
			rb.read = rb.write / 2
		}
	}
}

func BenchmarkRingBufferRead(b *testing.B) {
	rb := NewRingBuffer(8192)
	samples := make([]float32, 512)

	// Pre-fill buffer
	for i := 0; i < 8; i++ {
		rb.Write(samples)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rb.Read(samples)
		// Simulate production
		if i%16 == 0 {
			rb.write = rb.read + 4096
		}
	}
}

func BenchmarkRingBufferConcurrent(b *testing.B) {
	rb := NewRingBuffer(8192)
	writeSamples := make([]float32, 256)
	readSamples := make([]float32, 256)

	for i := range writeSamples {
		writeSamples[i] = float32(i) * 0.01
	}

	b.ResetTimer()

	var wg sync.WaitGroup
	wg.Add(2)

	// Producer
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			rb.Write(writeSamples)
		}
	}()

	// Consumer
	go func() {
		defer wg.Done()
		for i := 0; i < b.N; i++ {
			rb.Read(readSamples)
		}
	}()

	wg.Wait()
}

func TestRingBufferBasic(t *testing.T) {
	rb := NewRingBuffer(100)

	// Test write
	samples := []float32{1.0, 2.0, 3.0, 4.0, 5.0}
	written := rb.Write(samples)
	if written != 5 {
		t.Errorf("Expected to write 5 samples, wrote %d", written)
	}

	// Test available
	avail := rb.Available()
	if avail != 5 {
		t.Errorf("Expected 5 available samples, got %d", avail)
	}

	// Test read
	output := make([]float32, 3)
	read := rb.Read(output)
	if read != 3 {
		t.Errorf("Expected to read 3 samples, read %d", read)
	}

	// Verify data
	for i := 0; i < 3; i++ {
		if output[i] != samples[i] {
			t.Errorf("Sample %d: expected %f, got %f", i, samples[i], output[i])
		}
	}

	// Test remaining available
	avail = rb.Available()
	if avail != 2 {
		t.Errorf("Expected 2 remaining samples, got %d", avail)
	}
}

func TestRingBufferWrap(t *testing.T) {
	rb := NewRingBuffer(10)

	// Fill buffer almost to capacity
	samples := make([]float32, 8)
	for i := range samples {
		samples[i] = float32(i)
	}
	rb.Write(samples)

	// Read some to create space
	output := make([]float32, 5)
	rb.Read(output)

	// Write more to cause wrap-around
	more := []float32{10.0, 11.0, 12.0, 13.0, 14.0}
	written := rb.Write(more)
	if written != 5 {
		t.Errorf("Expected to write 5 samples after wrap, wrote %d", written)
	}

	// Verify we can read all data correctly
	result := make([]float32, 8)
	read := rb.Read(result)
	if read != 8 {
		t.Errorf("Expected to read 8 samples, read %d", read)
	}
}

func TestRingBufferFull(t *testing.T) {
	rb := NewRingBuffer(5)

	// Try to overfill (remember: 1 slot always kept empty)
	samples := make([]float32, 10)
	written := rb.Write(samples)

	// Should only write size-1 samples
	if written >= 5 {
		t.Errorf("Buffer should not accept all samples, wrote %d", written)
	}

	// Verify buffer is now effectively full
	oneSample := []float32{1.0}
	written = rb.Write(oneSample)
	if written != 0 {
		t.Errorf("Full buffer should not accept more samples, wrote %d", written)
	}
}

func TestRingBufferEmpty(t *testing.T) {
	rb := NewRingBuffer(10)

	// Try to read from empty buffer
	samples := make([]float32, 5)
	read := rb.Read(samples)
	if read != 0 {
		t.Errorf("Empty buffer should return 0 samples, got %d", read)
	}

	// Available should be 0
	avail := rb.Available()
	if avail != 0 {
		t.Errorf("Empty buffer should show 0 available, got %d", avail)
	}
}

func TestRingBufferClear(t *testing.T) {
	rb := NewRingBuffer(10)

	// Write some data
	samples := []float32{1.0, 2.0, 3.0}
	rb.Write(samples)

	// Clear buffer
	rb.Clear()

	// Verify empty
	avail := rb.Available()
	if avail != 0 {
		t.Errorf("Cleared buffer should have 0 available, got %d", avail)
	}

	// Verify we can write again
	written := rb.Write(samples)
	if written != 3 {
		t.Errorf("Expected to write 3 samples after clear, wrote %d", written)
	}
}
