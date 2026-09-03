package audiostream

import (
	"encoding/binary"
	"io"
	"math"
)

// StereoSource emits interleaved stereo float32 samples (L, R, L, R, ...).
// The return value of DrainStereoF32 counts float32 values written and is
// always even.
type StereoSource interface {
	DrainStereoF32([]float32) int
	OutputSampleRate() int
}

// StereoReader converts an interleaved StereoSource into little-endian stereo
// float32 PCM bytes, the layout Ebiten's NewPlayerF32 expects. Missing samples
// are padded with silence and counted as underruns.
type StereoReader struct {
	source    StereoSource
	buf       []float32
	pending   [8]byte
	pendingN  int
	underruns uint64
}

// NewStereoReader creates a stereo PCM reader. framesPerRead sizes the internal
// pull buffer (defaults to 1024).
func NewStereoReader(source StereoSource, framesPerRead int) *StereoReader {
	if framesPerRead <= 0 {
		framesPerRead = 1024
	}
	return &StereoReader{
		source: source,
		buf:    make([]float32, framesPerRead*2),
	}
}

// OutputSampleRate returns the source sample rate.
func (r *StereoReader) OutputSampleRate() int { return r.source.OutputSampleRate() }

// Underruns reports how many stereo frames were padded with silence.
func (r *StereoReader) Underruns() uint64 { return r.underruns }

// Read implements io.Reader.
func (r *StereoReader) Read(p []byte) (int, error) {
	written := 0
	if r.pendingN > 0 {
		n := copy(p, r.pending[:r.pendingN])
		copy(r.pending[:], r.pending[n:r.pendingN])
		r.pendingN -= n
		written += n
	}

	if full := (len(p) - written) / 8; full > 0 {
		n := r.readFrames(p[written:], full)
		written += n
	}

	if rem := len(p) - written; rem > 0 {
		var frame [8]byte
		r.readFrames(frame[:], 1)
		copy(p[written:], frame[:rem])
		copy(r.pending[:], frame[rem:])
		r.pendingN = 8 - rem
		written += rem
	}

	return written, nil
}

func (r *StereoReader) readFrames(dst []byte, frames int) int {
	if len(dst) < frames*8 {
		return 0
	}
	total := frames * 8

	for frames > 0 {
		chunk := min(frames, len(r.buf)/2)
		got := r.source.DrainStereoF32(r.buf[:chunk*2])
		if got < chunk*2 {
			for i := got; i < chunk*2; i++ {
				r.buf[i] = 0
			}
			r.underruns += uint64((chunk*2 - got) / 2)
		}

		for i := range chunk {
			l := clamp(r.buf[2*i], -1, 1)
			rr := clamp(r.buf[2*i+1], -1, 1)
			binary.LittleEndian.PutUint32(dst[i*8:], math.Float32bits(l))
			binary.LittleEndian.PutUint32(dst[i*8+4:], math.Float32bits(rr))
		}

		dst = dst[chunk*8:]
		frames -= chunk
	}

	return total
}

var _ io.Reader = (*StereoReader)(nil)
