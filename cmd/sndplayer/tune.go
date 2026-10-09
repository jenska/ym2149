package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jenska/ym2149/format/sndh"
	"github.com/jenska/ym2149/format/ym"
)

// tune is a parsed music file of any supported format, reduced to what the
// player shows and needs.
type tune struct {
	Format         string // "SNDH", "YM6!", ...
	Title          string
	Composer       string
	Subtunes       int
	DefaultSubtune int
	SubtuneNames   []string

	// Info rows for the song panel: four on the left, three on the right
	// (the UI adds the subtune row there).
	left, right []field

	durations []time.Duration
	open      func(subtune, sampleRate int) (tunePlayer, error)
}

type field struct{ label, value string }

// Duration reports the length of a 1-based subtune, 0 when unknown.
func (t *tune) Duration(subtune int) time.Duration {
	if subtune < 1 || subtune > len(t.durations) {
		return 0
	}
	return t.durations[subtune-1]
}

// tunePlayer is a per-channel PCM source that can report and change its
// position. The UI decides when a tune ends, so players always loop.
type tunePlayer interface {
	DrainChannelF32(ch int, dst []float32) int
	OutputSampleRate() int
	Position() time.Duration
	Duration() time.Duration
	Seek(time.Duration) error
	Finished() bool
	Err() error
}

func isTuneName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".sndh", ".snd", ".ym":
		return true
	}
	return false
}

// parseTune recognises SNDH (plain or ICE! packed) and YM (plain or LHA
// packed) files by content; the extension only picks which error to report.
func parseTune(name string, data []byte) (*tune, error) {
	f, serr := sndh.Parse(data)
	if serr == nil {
		return sndhTune(f), nil
	}
	s, yerr := ym.Parse(data)
	if yerr == nil {
		return ymTune(s), nil
	}
	if strings.EqualFold(filepath.Ext(name), ".ym") {
		return nil, yerr
	}
	return nil, serr
}

func sndhTune(f *sndh.File) *tune {
	flags := f.Flags
	if f.Packed {
		flags = strings.TrimSpace(flags + " (ICE! packed)")
	}
	t := &tune{
		Format:         "SNDH",
		Title:          f.Title,
		Composer:       f.Composer,
		Subtunes:       f.Subtunes,
		DefaultSubtune: f.DefaultSubtune,
		SubtuneNames:   f.SubtuneNames,
		left: []field{
			{"Title", f.Title}, {"Composer", f.Composer},
			{"Ripper", f.Ripper}, {"Converter", f.Converter},
		},
		right: []field{
			{"Year", f.Year},
			{"Replay", fmt.Sprintf("%v at %d Hz", f.Replay.Timer, f.Replay.Hz)},
			{"Flags", flags},
		},
		open: func(subtune, rate int) (tunePlayer, error) {
			return sndh.NewPlayer(f, sndh.PlayerConfig{
				SampleRate:  rate,
				Subtune:     subtune,
				Loop:        true,
				ChannelTaps: true,
			})
		},
	}
	for s := 1; s <= f.Subtunes; s++ {
		t.durations = append(t.durations, f.Duration(s))
	}
	return t
}

func ymTune(s *ym.Song) *tune {
	loop := "none"
	if s.LoopFrame > 0 {
		loop = "from " + fmtDur(time.Duration(s.LoopFrame)*time.Second/time.Duration(max(s.FrameHz, 1)))
	}
	t := &tune{
		Format:         fmt.Sprintf("YM%d", s.Version),
		Title:          s.Name,
		Composer:       s.Author,
		Subtunes:       1,
		DefaultSubtune: 1,
		left: []field{
			{"Title", s.Name}, {"Composer", s.Author},
			{"Comment", s.Comment},
			{"Format", fmt.Sprintf("YM%d register dump, %d frames", s.Version, len(s.Frames))},
		},
		right: []field{
			{"Rate", fmt.Sprintf("%d Hz, PSG at %.3f MHz", s.FrameHz, float64(s.ClockHz)/1e6)},
			{"Loop", loop},
			{"Drums", fmt.Sprintf("%d digidrum(s)", len(s.Drums))},
		},
		durations: []time.Duration{time.Duration(s.Duration() * float64(time.Second))},
		open: func(_, rate int) (tunePlayer, error) {
			p, err := ym.NewPlayer(s, ym.PlayerConfig{SampleRate: rate, Loop: true, ChannelTaps: true})
			if err != nil {
				return nil, err
			}
			return ymPlayer{p}, nil
		},
	}
	return t
}

// ymPlayer adapts ym.Player, which cannot fail, to tunePlayer.
type ymPlayer struct{ *ym.Player }

func (p ymPlayer) Seek(d time.Duration) error { p.Player.Seek(d); return nil }
func (p ymPlayer) Err() error                 { return nil }
