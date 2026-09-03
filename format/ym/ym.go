// Package ym decodes Atari ST "YM" register-dump music files (YM2!/YM3!/YM3b
// and the extended YM5!/YM6! formats, LHArc-compressed or raw) and replays them
// through the cycle-accurate PSG core in package emulation.
//
// A decoded Song is a table of 50 Hz register frames plus optional digidrum
// samples. Player turns a Song into a bandlimited.MonoSource that drives a Chip,
// reproducing the YM5/YM6 timer effects (digidrum, timer-synth "SID" voice,
// sync-buzzer) by scheduling sub-frame register writes.
package ym

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const atariSTClock = 2_000_000

// ErrUnsupported is returned for well-formed files this package cannot replay
// (currently only the obsolete YM4! format).
var ErrUnsupported = errors.New("ym: unsupported file format")

// Song is a decoded YM register-dump.
type Song struct {
	Version   int    // 2, 3, 5 or 6
	Name      string // song title (YM5/YM6 only)
	Author    string
	Comment   string
	FrameHz   int        // replay rate of Frames, in Hz (usually 50)
	ClockHz   int        // PSG master clock the dump was recorded at
	LoopFrame int        // frame index to resume from after the last frame
	Frames    [][16]byte // per-frame R0..R15 snapshots (R14/R15 carry effect data)
	Drums     [][]byte   // digidrum samples (YM5/YM6)

	drum4bit bool
}

// Duration returns the un-looped playing time in seconds.
func (s *Song) Duration() float64 {
	if s.FrameHz <= 0 {
		return 0
	}
	return float64(len(s.Frames)) / float64(s.FrameHz)
}

// Parse decodes a YM file, decompressing an LHArc container if present.
func Parse(data []byte) (*Song, error) {
	raw, err := maybeDepack(data)
	if err != nil {
		return nil, err
	}
	if len(raw) < 4 {
		return nil, fmt.Errorf("ym: file too small (%d bytes)", len(raw))
	}

	switch id := string(raw[:4]); id {
	case "YM2!", "YM3!":
		return parseYM3(raw, id, 0)
	case "YM3b":
		if len(raw) < 4+14+4 {
			return nil, fmt.Errorf("ym: truncated YM3b file")
		}
		loop := int(int32(binary.LittleEndian.Uint32(raw[len(raw)-4:])))
		return parseYM3(raw[:len(raw)-4], id, loop)
	case "YM4!":
		return nil, fmt.Errorf("%w: YM4!", ErrUnsupported)
	case "YM5!", "YM6!":
		return parseYM56(raw, id)
	default:
		return nil, fmt.Errorf("ym: unknown format %q", id)
	}
}

// parseYM3 handles YM2!/YM3!/YM3b: 14 register planes stored consecutively
// (register-major), 50 Hz, 2 MHz clock.
func parseYM3(raw []byte, id string, loop int) (*Song, error) {
	body := raw[4:]
	nb := len(body) / 14
	if nb == 0 {
		return nil, fmt.Errorf("ym: %s file has no frames", id)
	}

	s := &Song{
		Version:   3,
		Name:      "Unknown",
		FrameHz:   50,
		ClockHz:   atariSTClock,
		LoopFrame: loop,
		Frames:    make([][16]byte, nb),
	}
	if id == "YM2!" {
		s.Version = 2
	}
	for f := range nb {
		for r := range 14 {
			s.Frames[f][r] = body[r*nb+f]
		}
	}
	if s.LoopFrame < 0 || s.LoopFrame >= nb {
		s.LoopFrame = 0
	}
	return s, nil
}

// parseYM56 handles the extended "LeOnArD!" YM5!/YM6! layout.
func parseYM56(raw []byte, id string) (*Song, error) {
	const headerLen = 34
	if len(raw) < headerLen || string(raw[4:12]) != "LeOnArD!" {
		return nil, fmt.Errorf("ym: %s missing LeOnArD! signature", id)
	}

	p := 12
	u32 := func() uint32 { v := binary.BigEndian.Uint32(raw[p:]); p += 4; return v }
	u16 := func() uint16 { v := binary.BigEndian.Uint16(raw[p:]); p += 2; return v }

	nbFrame := int(u32())
	attrs := u32()
	nbDrum := int(u16())
	clock := int(u32())
	frameHz := int(u16())
	loop := int(u32())
	skip := int(u16())

	if nbFrame <= 0 || nbFrame > maxDepackedSize/16 {
		return nil, fmt.Errorf("ym: %s implausible frame count %d", id, nbFrame)
	}
	if p+skip > len(raw) {
		return nil, fmt.Errorf("ym: %s truncated extra header", id)
	}
	p += skip

	interleaved := attrs&0x01 != 0
	s := &Song{
		Version:   5,
		FrameHz:   frameHz,
		ClockHz:   clock,
		LoopFrame: loop,
		Frames:    make([][16]byte, nbFrame),
		drum4bit:  attrs&0x04 != 0,
	}
	if id == "YM6!" {
		s.Version = 6
	}
	if s.FrameHz <= 0 {
		s.FrameHz = 50
	}
	if s.ClockHz <= 0 {
		s.ClockHz = atariSTClock
	}

	for range nbDrum {
		if p+4 > len(raw) {
			return nil, fmt.Errorf("ym: %s truncated digidrum table", id)
		}
		sz := int(binary.BigEndian.Uint32(raw[p:]))
		p += 4
		if sz < 0 || p+sz > len(raw) {
			return nil, fmt.Errorf("ym: %s truncated digidrum sample", id)
		}
		d := make([]byte, sz)
		copy(d, raw[p:p+sz])
		p += sz
		s.Drums = append(s.Drums, d)
	}

	s.Name, p = readCString(raw, p)
	s.Author, p = readCString(raw, p)
	s.Comment, p = readCString(raw, p)

	need := nbFrame * 16
	if p+need > len(raw) {
		return nil, fmt.Errorf("ym: %s truncated frame data (want %d, have %d)", id, need, len(raw)-p)
	}
	fd := raw[p : p+need]
	if interleaved {
		for f := range nbFrame {
			for r := range 16 {
				s.Frames[f][r] = fd[r*nbFrame+f]
			}
		}
	} else {
		for f := range nbFrame {
			copy(s.Frames[f][:], fd[f*16:f*16+16])
		}
	}
	if s.LoopFrame < 0 || s.LoopFrame >= nbFrame {
		s.LoopFrame = 0
	}
	return s, nil
}

func readCString(b []byte, p int) (string, int) {
	if p < 0 || p >= len(b) {
		return "", len(b)
	}
	end := p
	for end < len(b) && b[end] != 0 {
		end++
	}
	s := string(b[p:end])
	if end < len(b) {
		end++
	}
	return s, end
}
