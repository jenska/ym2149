package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/jenska/ym2149/format/sndh"
	"github.com/jenska/ym2149/renderer/stereo"
)

// exportWAV renders length of a subtune to a 16-bit stereo WAV file, through
// the same filter chain as live playback. progress, if set, receives values
// from 0 to 1.
func exportWAV(path string, f *sndh.File, subtune int, length time.Duration, rate int, pan stereo.Panning, progress func(float64)) error {
	p, err := newTunePlayer(f, subtune, rate)
	if err != nil {
		return err
	}
	_, src, err := buildChain(p, pan)
	if err != nil {
		return err
	}

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	w := bufio.NewWriterSize(out, 1<<16)

	total := int(length.Seconds() * float64(rate))
	if err := writeWAVHeader(w, rate, total); err != nil {
		return err
	}

	buf := make([]float32, 2*4096)
	pcm := make([]byte, 2*len(buf))
	frames := 0
	for frames < total {
		want := min(len(buf)/2, total-frames)
		n := src.DrainStereoF32(buf[:2*want]) / 2
		if n == 0 {
			break
		}
		for i, v := range buf[:2*n] {
			s := int16(math.Round(float64(max(-1, min(1, v))) * 32767))
			binary.LittleEndian.PutUint16(pcm[2*i:], uint16(s))
		}
		if _, err := w.Write(pcm[:4*n]); err != nil {
			return err
		}
		frames += n
		if progress != nil {
			progress(float64(frames) / float64(total))
		}
		if p.Err() != nil {
			break
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if frames != total { // the driver stopped early: fix up the sizes
		if _, err := out.Seek(0, 0); err != nil {
			return err
		}
		if err := writeWAVHeader(out, rate, frames); err != nil {
			return err
		}
	}
	if err := p.Err(); err != nil {
		return fmt.Errorf("stopped after %v: %w", time.Duration(frames)*time.Second/time.Duration(rate), err)
	}
	return out.Close()
}

func writeWAVHeader(w interface{ Write([]byte) (int, error) }, rate, frames int) error {
	const channels, bits = 2, 16
	data := uint32(frames * channels * bits / 8)
	h := make([]byte, 0, 44)
	h = append(h, "RIFF"...)
	h = binary.LittleEndian.AppendUint32(h, 36+data)
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1) // PCM
	h = binary.LittleEndian.AppendUint16(h, channels)
	h = binary.LittleEndian.AppendUint32(h, uint32(rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(rate*channels*bits/8))
	h = binary.LittleEndian.AppendUint16(h, channels*bits/8)
	h = binary.LittleEndian.AppendUint16(h, bits)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, data)
	_, err := w.Write(h)
	return err
}
