// Package sndh loads and replays SNDH music files: Atari ST music drivers
// (68000 machine code) wrapped in a small tagged header, as collected by the
// SNDH archive (https://sndh.atari.org). ICE! 2.4 packed files are depacked
// transparently.
//
// Replay runs the driver on an emulated 68000 (github.com/jenska/m68kemu)
// inside a minimal Atari ST: 4 MB of RAM, the YM2149 PSG, an MC68901 MFP with
// its four timers, the VBL interrupt and just enough of TOS (system variables,
// GEMDOS/BIOS/XBIOS traps) for drivers to set themselves up.
package sndh

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"time"
)

// TimerKind identifies the interrupt that calls a tune's play routine.
type TimerKind byte

const (
	TimerC   TimerKind = 'C' // default: the OS 200 Hz Timer C tick, divided down
	TimerA   TimerKind = 'A'
	TimerB   TimerKind = 'B'
	TimerD   TimerKind = 'D'
	TimerVBL TimerKind = 'V' // vertical blank, at the machine's VBL rate
)

func (k TimerKind) String() string {
	if k == TimerVBL {
		return "VBL"
	}
	return "Timer " + string(rune(k))
}

// Replay is the play-routine call rate requested by the header.
type Replay struct {
	Timer TimerKind
	Hz    int
}

// File is a parsed SNDH file.
type File struct {
	Title     string
	Composer  string
	Ripper    string
	Converter string
	Year      string

	// Subtunes is the number of subtunes (at least 1); DefaultSubtune is
	// 1-based.
	Subtunes       int
	DefaultSubtune int
	// SubtuneNames holds one name per subtune when the file has a #!SN tag.
	SubtuneNames []string

	Replay Replay

	// Frames holds per-subtune lengths in play-routine calls (FRMS tag, or
	// TIME converted at the replay rate). 0 means "loops forever" or unknown.
	Frames []uint32

	// Flags is the raw FLAG tag payload, e.g. "y" (YM2149) or "ye" (+STe).
	Flags string

	// Packed reports whether the file was ICE! packed.
	Packed bool

	// Data is the depacked image, starting with the INIT/EXIT/PLAY branches.
	Data []byte
}

// Duration reports the length of a 1-based subtune, or 0 when unknown.
func (f *File) Duration(subtune int) time.Duration {
	if subtune < 1 || subtune > len(f.Frames) || f.Frames[subtune-1] == 0 || f.Replay.Hz <= 0 {
		return 0
	}
	return time.Duration(f.Frames[subtune-1]) * time.Second / time.Duration(f.Replay.Hz)
}

// HasFlag reports whether the FLAG tag lists c (e.g. 'e' for STe hardware).
func (f *File) HasFlag(c byte) bool {
	return bytes.IndexByte([]byte(f.Flags), c) >= 0
}

// Parse depacks (if needed) and parses an SNDH file.
func Parse(data []byte) (*File, error) {
	f := &File{}
	if IsICEPacked(data) {
		var err error
		data, err = DepackICE(data)
		if err != nil {
			return nil, err
		}
		f.Packed = true
	}
	if len(data) < 16 || string(data[12:16]) != "SNDH" {
		return nil, fmt.Errorf("sndh: missing SNDH header")
	}
	f.Data = data
	f.parseTags(headerEnd(data))

	if f.Subtunes < 1 {
		f.Subtunes = 1
	}
	if f.DefaultSubtune < 1 || f.DefaultSubtune > f.Subtunes {
		f.DefaultSubtune = 1
	}
	if f.Replay.Timer == 0 {
		f.Replay.Timer = TimerC
	}
	if f.Replay.Hz <= 0 {
		f.Replay.Hz = 50
	}
	if len(f.SubtuneNames) != 0 && len(f.SubtuneNames) != f.Subtunes {
		f.SubtuneNames = nil
	}
	return f, nil
}

// headerEnd bounds the tag scan: the header lies before the code the three
// entry branches jump to.
func headerEnd(data []byte) int {
	end := min(len(data), 4096)
	for off := 0; off < 12; off += 4 {
		op := binary.BigEndian.Uint16(data[off:])
		var target int
		switch {
		case op == 0x6000 || op == 0x4efa: // bra.w / jmp d16(pc)
			target = off + 2 + int(int16(binary.BigEndian.Uint16(data[off+2:])))
		case op&0xff00 == 0x6000: // bra.s
			target = off + 2 + int(int8(op))
		default:
			continue
		}
		if target > 16 && target < end {
			end = target
		}
	}
	return end
}

func (f *File) parseTags(end int) {
	d := f.Data
	var subtuneTime []uint16
	var frms []uint32
	p := 16

	cstring := func(from int) (string, int) {
		i := from
		for i < end && d[i] != 0 {
			i++
		}
		return string(bytes.TrimSpace(d[from:i])), min(i+1, end)
	}
	digits := func(from int) (int, int) {
		i := from
		for i < end && d[i] >= '0' && d[i] <= '9' {
			i++
		}
		n, _ := strconv.Atoi(string(d[from:i]))
		return n, i
	}
	has := func(tag string) bool {
		return p+len(tag) <= end && string(d[p:p+len(tag)]) == tag
	}

	for p < end {
		switch {
		case has("HDNS"):
			p = end
		case has("TITL"):
			f.Title, p = cstring(p + 4)
		case has("COMM"):
			f.Composer, p = cstring(p + 4)
		case has("RIPP"):
			f.Ripper, p = cstring(p + 4)
		case has("CONV"):
			f.Converter, p = cstring(p + 4)
		case has("YEAR"):
			f.Year, p = cstring(p + 4)
		case has("FLAG"):
			p += 4
			if p < end && d[p] == '~' {
				p++
			}
			f.Flags, p = cstring(p)
		case has("#!SN"):
			p = f.parseSubtuneNames(p+4, end)
		case has("##"):
			f.Subtunes, p = digits(p + 2)
		case has("#!"):
			f.DefaultSubtune, p = digits(p + 2)
		case has("!V"):
			f.Replay.Timer = TimerVBL
			f.Replay.Hz, p = digits(p + 2)
		case has("TA"), has("TB"), has("TC"), has("TD"):
			if p+2 < end && d[p+2] >= '0' && d[p+2] <= '9' {
				f.Replay.Timer = TimerKind(d[p+1])
				f.Replay.Hz, p = digits(p + 2)
			} else {
				p++
			}
		case has("FRMS"):
			p += 4
			for range max(f.Subtunes, 1) {
				if p+4 > end {
					break
				}
				frms = append(frms, binary.BigEndian.Uint32(d[p:]))
				p += 4
			}
		case has("TIME"):
			p += 4
			for range max(f.Subtunes, 1) {
				if p+2 > end {
					break
				}
				subtuneTime = append(subtuneTime, binary.BigEndian.Uint16(d[p:]))
				p += 2
			}
		default:
			p++ // padding, or a tag this parser does not know
		}
	}

	switch {
	case len(frms) != 0:
		f.Frames = frms
	case len(subtuneTime) != 0:
		hz := max(f.Replay.Hz, 0)
		if hz == 0 {
			hz = 50
		}
		for _, s := range subtuneTime {
			f.Frames = append(f.Frames, uint32(s)*uint32(hz))
		}
	}
}

// parseSubtuneNames reads the #!SN word-offset table, whose offsets are
// relative to the first table entry, and returns the scan position after the
// last name.
func (f *File) parseSubtuneNames(base, end int) int {
	n := max(f.Subtunes, 1)
	d := f.Data
	if base+2*n > end {
		return base
	}
	offs := make([]int, n)
	for i := range offs {
		offs[i] = int(binary.BigEndian.Uint16(d[base+2*i:]))
	}
	// Real files use offsets from the table start, but the format document's
	// example writes each entry relative to the previous name. Absolute
	// offsets put every later name right after the previous one's NUL.
	for i := 1; i < n; i++ {
		if s := base + offs[i]; s <= base || s >= end || d[s-1] != 0 {
			for j := 1; j < n; j++ {
				offs[j] += offs[j-1]
			}
			break
		}
	}

	names := make([]string, 0, n)
	last := base + 2*n
	for _, o := range offs {
		s := base + o
		if s < base+2*n || s >= end {
			return last
		}
		i := s
		for i < end && d[i] != 0 {
			i++
		}
		names = append(names, string(bytes.TrimSpace(d[s:i])))
		last = max(last, min(i+1, end))
	}
	f.SubtuneNames = names
	return last
}
