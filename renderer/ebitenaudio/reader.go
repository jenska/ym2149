package ebitenaudio

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2/audio"
)

// MonoSource is the minimum interface the Ebiten adapter needs from the core.
type MonoSource interface {
	DrainMonoF32([]float32) int
	OutputSampleRate() int
}

// Reader converts mono float32 samples into stereo float32 PCM bytes for Ebiten.
type Reader struct {
	source     MonoSource
	monoBuffer []float32
	pending    [8]byte
	pendingN   int
	underruns  uint64
}

// NewReader creates a stereo PCM adapter around the YM2149 core.
func NewReader(source MonoSource, framesPerRead int) *Reader {
	if framesPerRead <= 0 {
		framesPerRead = 1024
	}
	return &Reader{
		source:     source,
		monoBuffer: make([]float32, framesPerRead),
	}
}

// Read implements io.Reader for Ebiten's NewPlayerF32 API.
func (r *Reader) Read(p []byte) (int, error) {
	written := 0
	if r.pendingN > 0 {
		n := copy(p, r.pending[:r.pendingN])
		copy(r.pending[:], r.pending[n:r.pendingN])
		r.pendingN -= n
		written += n
	}

	fullFrames := (len(p) - written) / 8
	if fullFrames > 0 {
		n, err := r.readFramesInto(p[written:], fullFrames)
		written += n
		if err != nil {
			return written, err
		}
	}

	if rem := len(p) - written; rem > 0 {
		var frame [8]byte
		if _, err := r.readFramesInto(frame[:], 1); err != nil {
			return written, err
		}
		copy(p[written:], frame[:rem])
		copy(r.pending[:], frame[rem:])
		r.pendingN = 8 - rem
		written += rem
	}

	return written, nil
}

// Underruns reports how many silent frames were injected because the core had no PCM ready.
func (r *Reader) Underruns() uint64 {
	return r.underruns
}

// EnsureContext reuses an existing audio context when compatible, or creates one.
func EnsureContext(sampleRate int) (*audio.Context, error) {
	if ctx := audio.CurrentContext(); ctx != nil {
		if ctx.SampleRate() != sampleRate {
			return nil, fmt.Errorf("existing Ebiten audio context uses sample rate %d, want %d", ctx.SampleRate(), sampleRate)
		}
		return ctx, nil
	}
	return audio.NewContext(sampleRate), nil
}

// NewPlayer wires a MonoSource into an Ebiten F32 player.
func NewPlayer(source MonoSource, buffer time.Duration) (*audio.Player, *Reader, error) {
	ctx, err := EnsureContext(source.OutputSampleRate())
	if err != nil {
		return nil, nil, err
	}
	reader := NewReader(source, 1024)
	player, err := ctx.NewPlayerF32(reader)
	if err != nil {
		return nil, nil, err
	}
	if buffer > 0 {
		player.SetBufferSize(buffer)
	}
	return player, reader, nil
}

func (r *Reader) readFramesInto(dst []byte, frames int) (int, error) {
	if len(dst) < frames*8 {
		return 0, io.ErrShortBuffer
	}
	written := frames * 8

	for frames > 0 {
		chunk := frames
		if chunk > len(r.monoBuffer) {
			chunk = len(r.monoBuffer)
		}
		read := r.source.DrainMonoF32(r.monoBuffer[:chunk])
		if read < chunk {
			for i := read; i < chunk; i++ {
				r.monoBuffer[i] = 0
			}
			r.underruns += uint64(chunk - read)
		}

		for i := 0; i < chunk; i++ {
			sample := clamp(r.monoBuffer[i], -1, 1)
			bits := math.Float32bits(sample)
			base := i * 8
			binary.LittleEndian.PutUint32(dst[base:], bits)
			binary.LittleEndian.PutUint32(dst[base+4:], bits)
		}

		dst = dst[chunk*8:]
		frames -= chunk
	}

	return written, nil
}

func clamp[T ~float32 | ~float64](v, lo, hi T) T {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
