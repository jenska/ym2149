// Package stereo pans the YM2149's three tone channels into a stereo image.
//
// The chip core is mono at heart, but its per-channel taps (enable
// emulation.Config.ChannelTaps) expose each tone channel's isolated
// contribution. Splitter mixes those three taps down to a left/right pair
// using a Panning matrix; the common retro layouts are provided as presets:
//
//   - ABC:  A hard left, B centre, C hard right (Atari ST / Amstrad CPC)
//   - ACB:  A left, C centre, B right (some ZX Spectrum 128 setups)
//   - Mono: every channel centred
//
// Splitter.Left and Splitter.Right are ordinary mono sources, so each side can
// run through its own renderer/bandlimited and renderer/atarist chain before
// being interleaved for playback.
package stereo

// MonoSource is the per-side interface shared with renderer/bandlimited and
// renderer/atarist.
type MonoSource interface {
	DrainMonoF32([]float32) int
	OutputSampleRate() int
}

// ChannelSource provides isolated per-channel PCM. *emulation.Chip satisfies it
// when created with Config.ChannelTaps set.
type ChannelSource interface {
	DrainChannelF32(ch int, dst []float32) int
	OutputSampleRate() int
}

// Panning holds per-channel output gains: Panning[ch][0] is the gain into the
// left output, Panning[ch][1] into the right, for ch 0=A, 1=B, 2=C.
type Panning [3][2]float32

// Mono centres every channel (L == R).
func Mono() Panning { return ABCWidth(0) }

// ABC is the Atari ST / Amstrad CPC layout: A left, B centre, C right.
func ABC() Panning { return ABCWidth(1) }

// ACB places A left, C centre, B right.
func ACB() Panning { return ACBWidth(1) }

// ABCWidth is ABC with an adjustable stereo width in [0,1]: 0 collapses to
// mono, 1 hard-pans A and C. Left+right gain per channel is held at 1 so the
// overall level does not change with width.
func ABCWidth(width float32) Panning { return widthPan(width, 0, 1, 2) }

// ACBWidth is ACB with an adjustable stereo width in [0,1].
func ACBWidth(width float32) Panning { return widthPan(width, 0, 2, 1) }

func widthPan(w float32, left, centre, right int) Panning {
	if w < 0 {
		w = 0
	}
	if w > 1 {
		w = 1
	}
	var p Panning
	p[left] = [2]float32{0.5 + 0.5*w, 0.5 - 0.5*w}
	p[centre] = [2]float32{0.5, 0.5}
	p[right] = [2]float32{0.5 - 0.5*w, 0.5 + 0.5*w}
	return p
}

// Splitter mixes a ChannelSource into a stereo pair. Left and Right are
// independent mono sources that stay sample-aligned: draining either one pulls
// a block from the underlying channel source and buffers the other side.
//
// A Splitter is not safe for concurrent use; drain Left and Right from the
// same goroutine (or guard them).
type Splitter struct {
	src ChannelSource
	pan Panning

	scratch [3][]float32
	left    []float32
	right   []float32
}

// NewSplitter builds a Splitter over src using the given panning.
func NewSplitter(src ChannelSource, pan Panning) *Splitter {
	return &Splitter{src: src, pan: pan}
}

// SetPanning swaps the panning matrix. Samples already buffered keep their old
// panning; the change takes effect on the next block pulled from the source.
func (s *Splitter) SetPanning(p Panning) { s.pan = p }

// OutputSampleRate reports the wrapped source rate.
func (s *Splitter) OutputSampleRate() int { return s.src.OutputSampleRate() }

// Left returns the left channel as a mono source.
func (s *Splitter) Left() MonoSource { return side{s, false} }

// Right returns the right channel as a mono source.
func (s *Splitter) Right() MonoSource { return side{s, true} }

func (s *Splitter) fill(want int) {
	if want < 1 {
		want = 1
	}
	if len(s.scratch[0]) < want {
		for ch := range s.scratch {
			s.scratch[ch] = make([]float32, want)
		}
	}

	n := want
	for ch := range 3 {
		got := s.src.DrainChannelF32(ch, s.scratch[ch][:want])
		if got < n {
			n = got
		}
	}

	for i := range n {
		var l, r float32
		for ch := range 3 {
			v := s.scratch[ch][i]
			l += v * s.pan[ch][0]
			r += v * s.pan[ch][1]
		}
		s.left = append(s.left, l)
		s.right = append(s.right, r)
	}
}

func (s *Splitter) drain(right bool, dst []float32) int {
	n := 0
	for n < len(dst) {
		buf := &s.left
		if right {
			buf = &s.right
		}
		if len(*buf) == 0 {
			s.fill(len(dst) - n)
			if len(*buf) == 0 {
				break
			}
		}
		c := copy(dst[n:], *buf)
		*buf = (*buf)[c:]
		n += c
	}
	return n
}

type side struct {
	s     *Splitter
	right bool
}

func (sd side) OutputSampleRate() int          { return sd.s.OutputSampleRate() }
func (sd side) DrainMonoF32(dst []float32) int { return sd.s.drain(sd.right, dst) }

// Interleaved presents a left/right pair as one interleaved stereo source
// (L, R, L, R, ...). It satisfies audiostream.StereoSource.
type Interleaved struct {
	left, right MonoSource
	lb, rb      []float32
}

// Interleave joins two mono sources into an interleaved stereo source. They
// must share a sample rate; left's rate is reported.
func Interleave(left, right MonoSource) *Interleaved {
	return &Interleaved{left: left, right: right}
}

// OutputSampleRate reports the left source's rate.
func (x *Interleaved) OutputSampleRate() int { return x.left.OutputSampleRate() }

// DrainStereoF32 fills dst with interleaved stereo samples and returns the
// number of float32 values written (always even).
func (x *Interleaved) DrainStereoF32(dst []float32) int {
	frames := len(dst) / 2
	if frames == 0 {
		return 0
	}
	if cap(x.lb) < frames {
		x.lb = make([]float32, frames)
		x.rb = make([]float32, frames)
	}
	nl := x.left.DrainMonoF32(x.lb[:frames])
	nr := x.right.DrainMonoF32(x.rb[:frames])
	n := min(nl, nr)
	for i := range n {
		dst[2*i] = x.lb[i]
		dst[2*i+1] = x.rb[i]
	}
	return 2 * n
}
