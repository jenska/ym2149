package ym

import (
	"encoding/binary"
	"testing"
)

// buildYM3 assembles a raw (uncompressed) YM3! stream from frame-major rows.
func buildYM3(frames [][14]byte, loop *int) []byte {
	id := "YM3!"
	if loop != nil {
		id = "YM3b"
	}
	nb := len(frames)
	out := append([]byte(nil), id...)
	plane := make([]byte, 14*nb)
	for f, fr := range frames {
		for r := range 14 {
			plane[r*nb+f] = fr[r]
		}
	}
	out = append(out, plane...)
	if loop != nil {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(*loop))
		out = append(out, b[:]...)
	}
	return out
}

// buildYM56 assembles a raw YM5!/YM6! stream.
func buildYM56(id string, frames [][16]byte, drums [][]byte, interleaved bool, drum4 bool, frameHz, clock, loop int) []byte {
	var attr uint32
	if interleaved {
		attr |= 0x01
	}
	if drum4 {
		attr |= 0x04
	}
	out := append([]byte(nil), id...)
	out = append(out, "LeOnArD!"...)
	be32 := func(v uint32) { var b [4]byte; binary.BigEndian.PutUint32(b[:], v); out = append(out, b[:]...) }
	be16 := func(v uint16) { var b [2]byte; binary.BigEndian.PutUint16(b[:], v); out = append(out, b[:]...) }

	be32(uint32(len(frames)))
	be32(attr)
	be16(uint16(len(drums)))
	be32(uint32(clock))
	be16(uint16(frameHz))
	be32(uint32(loop))
	be16(0) // extra header size

	for _, d := range drums {
		be32(uint32(len(d)))
		out = append(out, d...)
	}
	out = append(out, "Title\x00Author\x00Comment\x00"...)

	if interleaved {
		plane := make([]byte, 16*len(frames))
		for f, fr := range frames {
			for r := range 16 {
				plane[r*len(frames)+f] = fr[r]
			}
		}
		out = append(out, plane...)
	} else {
		for _, fr := range frames {
			out = append(out, fr[:]...)
		}
	}
	return out
}

func TestParseYM3(t *testing.T) {
	frames := [][14]byte{
		{0x11, 0x01, 0x22, 0x02, 0x33, 0x03, 0x04, 0x38, 0x0f, 0x00, 0x00, 0x00, 0x00, 0x00},
		{0xaa, 0x00, 0xbb, 0x00, 0xcc, 0x00, 0x1f, 0x07, 0x0a, 0x0b, 0x0c, 0x00, 0x00, 0x00},
	}
	s, err := Parse(buildYM3(frames, nil))
	if err != nil {
		t.Fatal(err)
	}
	if s.Version != 3 || s.FrameHz != 50 || s.ClockHz != atariSTClock {
		t.Fatalf("metadata: %+v", s)
	}
	if len(s.Frames) != 2 {
		t.Fatalf("frames = %d", len(s.Frames))
	}
	for f := range frames {
		for r := range 14 {
			if s.Frames[f][r] != frames[f][r] {
				t.Fatalf("frame %d reg %d = %#x, want %#x", f, r, s.Frames[f][r], frames[f][r])
			}
		}
	}
}

func TestParseYM3bLoop(t *testing.T) {
	frames := make([][14]byte, 8)
	for i := range frames {
		frames[i][0] = byte(i)
	}
	loop := 3
	s, err := Parse(buildYM3(frames, &loop))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frames) != 8 {
		t.Fatalf("frames = %d", len(s.Frames))
	}
	if s.LoopFrame != 3 {
		t.Fatalf("loop = %d", s.LoopFrame)
	}
	for i := range frames {
		if s.Frames[i][0] != byte(i) {
			t.Fatalf("frame %d reg0 = %d", i, s.Frames[i][0])
		}
	}
}

func TestParseYM56BothLayouts(t *testing.T) {
	frames := [][16]byte{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		{15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
		{100, 101, 102, 103, 104, 105, 106, 107, 108, 109, 110, 111, 112, 113, 114, 115},
	}
	drums := [][]byte{{1, 2, 3, 4}, {9, 8, 7}}

	for _, interleaved := range []bool{false, true} {
		raw := buildYM56("YM6!", frames, drums, interleaved, true, 50, atariSTClock, 1)
		s, err := Parse(raw)
		if err != nil {
			t.Fatalf("interleaved=%v: %v", interleaved, err)
		}
		if s.Version != 6 || s.Name != "Title" || s.Author != "Author" || s.Comment != "Comment" {
			t.Fatalf("metadata: %+v", s)
		}
		if len(s.Drums) != 2 || len(s.Drums[0]) != 4 || s.Drums[1][0] != 9 {
			t.Fatalf("drums: %v", s.Drums)
		}
		if !s.drum4bit || s.LoopFrame != 1 {
			t.Fatalf("attrs: drum4=%v loop=%d", s.drum4bit, s.LoopFrame)
		}
		for f := range frames {
			if s.Frames[f] != frames[f] {
				t.Fatalf("interleaved=%v frame %d = %v, want %v", interleaved, f, s.Frames[f], frames[f])
			}
		}
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("XX")); err == nil {
		t.Fatal("want error for short input")
	}
	if _, err := Parse([]byte("ZZZZ....")); err == nil {
		t.Fatal("want error for unknown magic")
	}
	if _, err := Parse(append([]byte("YM4!"), make([]byte, 20)...)); err == nil {
		t.Fatal("want error for YM4!")
	}
	if _, err := Parse([]byte("YM5!LeOnArD!short")); err == nil {
		t.Fatal("want error for truncated YM5")
	}
}

func TestParseCompressedYM3(t *testing.T) {
	frames := make([][14]byte, 20)
	for i := range frames {
		frames[i][7] = 0x38
		frames[i][8] = 0x0c
	}
	raw := buildYM3(frames, nil)
	wrapped := wrapLHArc("-lh5-", encodeLiterals(raw, 13), len(raw))

	s, err := Parse(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frames) != 20 || s.Frames[5][7] != 0x38 {
		t.Fatalf("decoded song wrong: %d frames", len(s.Frames))
	}
}
