package audiostream

import (
	"encoding/binary"
	"math"
	"testing"
)

type fakeStereo struct {
	samples []float32 // interleaved L,R
	rate    int
}

func (f *fakeStereo) OutputSampleRate() int { return f.rate }

func (f *fakeStereo) DrainStereoF32(dst []float32) int {
	n := min(len(dst)-len(dst)%2, len(f.samples))
	copy(dst, f.samples[:n])
	f.samples = f.samples[n:]
	return n
}

func TestStereoReaderEmitsInterleavedPCM(t *testing.T) {
	src := &fakeStereo{
		rate:    48_000,
		samples: []float32{0.25, -0.5, 1, -1, 0.75, 0.1},
	}
	r := NewStereoReader(src, 4)
	if r.OutputSampleRate() != 48_000 {
		t.Fatalf("rate = %d", r.OutputSampleRate())
	}

	buf := make([]byte, 3*8)
	n, err := r.Read(buf)
	if err != nil || n != 24 {
		t.Fatalf("Read = %d, %v", n, err)
	}

	want := []float32{0.25, -0.5, 1, -1, 0.75, 0.1}
	for i := range want {
		got := math.Float32frombits(binary.LittleEndian.Uint32(buf[i*4:]))
		if got != want[i] {
			t.Fatalf("value %d = %v, want %v", i, got, want[i])
		}
	}
}

func TestStereoReaderClampsAndCountsUnderruns(t *testing.T) {
	src := &fakeStereo{rate: 44_100, samples: []float32{2, -2}} // one frame, out of range
	r := NewStereoReader(src, 8)

	buf := make([]byte, 3*8) // ask for 3 frames, only 1 available
	if _, err := r.Read(buf); err != nil {
		t.Fatal(err)
	}
	l := math.Float32frombits(binary.LittleEndian.Uint32(buf[0:]))
	rr := math.Float32frombits(binary.LittleEndian.Uint32(buf[4:]))
	if l != 1 || rr != -1 {
		t.Fatalf("clamp failed: %v / %v", l, rr)
	}
	if r.Underruns() != 2 {
		t.Fatalf("underruns = %d, want 2", r.Underruns())
	}
}
