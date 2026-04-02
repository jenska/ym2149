package audiostream

import (
	"encoding/binary"
	"math"
	"testing"
)

type fakeSource struct {
	samples []float32
}

func (f *fakeSource) DrainMonoF32(dst []float32) int {
	n := len(dst)
	if n > len(f.samples) {
		n = len(f.samples)
	}
	copy(dst, f.samples[:n])
	f.samples = f.samples[n:]
	return n
}

func (f *fakeSource) OutputSampleRate() int {
	return 48_000
}

func TestReaderDuplicatesMonoToStereo(t *testing.T) {
	src := &fakeSource{samples: []float32{0.25, -0.5}}
	reader := NewReader(src, 2)
	buf := make([]byte, 16)

	n, err := reader.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if n != len(buf) {
		t.Fatalf("Read bytes = %d, want %d", n, len(buf))
	}

	left0 := math.Float32frombits(binary.LittleEndian.Uint32(buf[0:4]))
	right0 := math.Float32frombits(binary.LittleEndian.Uint32(buf[4:8]))
	left1 := math.Float32frombits(binary.LittleEndian.Uint32(buf[8:12]))
	right1 := math.Float32frombits(binary.LittleEndian.Uint32(buf[12:16]))

	if left0 != 0.25 || right0 != 0.25 || left1 != -0.5 || right1 != -0.5 {
		t.Fatalf("unexpected stereo frames: %f %f %f %f", left0, right0, left1, right1)
	}
}

func TestReaderFillsSilenceOnUnderrun(t *testing.T) {
	src := &fakeSource{samples: []float32{0.5}}
	reader := NewReader(src, 2)
	buf := make([]byte, 16)

	if _, err := reader.Read(buf); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := reader.Underruns(); got != 1 {
		t.Fatalf("Underruns = %d, want 1", got)
	}

	lastLeft := math.Float32frombits(binary.LittleEndian.Uint32(buf[8:12]))
	lastRight := math.Float32frombits(binary.LittleEndian.Uint32(buf[12:16]))
	if lastLeft != 0 || lastRight != 0 {
		t.Fatalf("expected silent underrun frame, got %f %f", lastLeft, lastRight)
	}
}
