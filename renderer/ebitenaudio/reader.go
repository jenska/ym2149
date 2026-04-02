package ebitenaudio

import (
	"fmt"
	"time"

	"github.com/jenska/ym2149/renderer/audiostream"
	"github.com/hajimehoshi/ebiten/v2/audio"
)

type MonoSource = audiostream.MonoSource
type Reader = audiostream.Reader

// NewReader creates a backend-neutral stereo PCM reader suitable for Ebiten.
func NewReader(source MonoSource, framesPerRead int) *Reader {
	return audiostream.NewReader(source, framesPerRead)
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
