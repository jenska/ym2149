package stereo

import (
	"math"
	"testing"
)

// fakeChannels serves three fixed DC levels as isolated channel PCM.
type fakeChannels struct {
	level [3]float32
	rate  int
	left  int // samples remaining
}

func (f *fakeChannels) OutputSampleRate() int { return f.rate }

func (f *fakeChannels) DrainChannelF32(ch int, dst []float32) int {
	n := min(len(dst), f.left)
	for i := range n {
		dst[i] = f.level[ch]
	}
	if ch == 2 {
		f.left -= n
	}
	return n
}

func approx(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-6 }

func TestPanningPresets(t *testing.T) {
	abc := ABC()
	if !approx(abc[0][0], 1) || !approx(abc[0][1], 0) {
		t.Fatalf("ABC channel A = %v, want {1,0}", abc[0])
	}
	if !approx(abc[1][0], 0.5) || !approx(abc[1][1], 0.5) {
		t.Fatalf("ABC channel B = %v, want {0.5,0.5}", abc[1])
	}
	if !approx(abc[2][0], 0) || !approx(abc[2][1], 1) {
		t.Fatalf("ABC channel C = %v, want {0,1}", abc[2])
	}

	m := Mono()
	for ch := range 3 {
		if !approx(m[ch][0], 0.5) || !approx(m[ch][1], 0.5) {
			t.Fatalf("Mono channel %d = %v, want {0.5,0.5}", ch, m[ch])
		}
	}

	acb := ACB()
	// A left, C centre, B right.
	if !approx(acb[0][0], 1) || !approx(acb[0][1], 0) {
		t.Fatalf("ACB channel A = %v, want {1,0}", acb[0])
	}
	if !approx(acb[2][0], 0.5) || !approx(acb[2][1], 0.5) {
		t.Fatalf("ACB channel C = %v, want {0.5,0.5}", acb[2])
	}
	if !approx(acb[1][0], 0) || !approx(acb[1][1], 1) {
		t.Fatalf("ACB channel B = %v, want {0,1}", acb[1])
	}
}

func TestSplitterPansChannels(t *testing.T) {
	src := &fakeChannels{level: [3]float32{0.8, 0.4, 0.2}, rate: 48_000, left: 4096}
	sp := NewSplitter(src, ABC())
	if sp.OutputSampleRate() != 48_000 {
		t.Fatalf("rate = %d", sp.OutputSampleRate())
	}

	l := make([]float32, 512)
	r := make([]float32, 512)
	nl := sp.Left().DrainMonoF32(l)
	nr := sp.Right().DrainMonoF32(r)
	if nl != 512 || nr != 512 {
		t.Fatalf("drained %d/%d", nl, nr)
	}

	// ABC: L = A*1 + B*0.5 + C*0     = 0.8 + 0.2      = 1.0
	//      R = A*0 + B*0.5 + C*1     = 0.2 + 0.2      = 0.4
	if !approx(l[0], 1.0) {
		t.Fatalf("left = %v, want 1.0", l[0])
	}
	if !approx(r[0], 0.4) {
		t.Fatalf("right = %v, want 0.4", r[0])
	}
}

func TestSplitterMonoPreservesSum(t *testing.T) {
	src := &fakeChannels{level: [3]float32{0.3, 0.3, 0.3}, rate: 48_000, left: 1024}
	sp := NewSplitter(src, Mono())
	l := make([]float32, 256)
	r := make([]float32, 256)
	sp.Left().DrainMonoF32(l)
	sp.Right().DrainMonoF32(r)
	// Each side: (0.3+0.3+0.3)*0.5 = 0.45
	if !approx(l[0], 0.45) || !approx(r[0], 0.45) {
		t.Fatalf("mono split = %v / %v, want 0.45", l[0], r[0])
	}
}

func TestInterleave(t *testing.T) {
	src := &fakeChannels{level: [3]float32{1, 0, 0}, rate: 44_100, left: 2048}
	sp := NewSplitter(src, ABC())
	st := Interleave(sp.Left(), sp.Right())
	if st.OutputSampleRate() != 44_100 {
		t.Fatalf("rate = %d", st.OutputSampleRate())
	}

	out := make([]float32, 200)
	n := st.DrainStereoF32(out)
	if n != 200 {
		t.Fatalf("wrote %d", n)
	}
	for i := 0; i < n; i += 2 {
		if !approx(out[i], 1) || !approx(out[i+1], 0) {
			t.Fatalf("frame %d = {%v,%v}, want {1,0}", i/2, out[i], out[i+1])
		}
	}
}

func TestSplitterHandlesSourceExhaustion(t *testing.T) {
	src := &fakeChannels{level: [3]float32{0.5, 0.5, 0.5}, rate: 48_000, left: 100}
	sp := NewSplitter(src, ABC())
	l := make([]float32, 512)
	got := sp.Left().DrainMonoF32(l)
	if got != 100 {
		t.Fatalf("got %d, want 100", got)
	}
}
