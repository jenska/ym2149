package sndh

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"testing"
)

// iceEncoder is a test-only ICE! 2.4 packer. It emits the decoder's read
// sequence (bits and raw literal bytes) in order and lays the stream out back
// to front, so it exercises every code the depacker understands.
type iceEncoder struct {
	stream    []byte // in decoder read order
	bitSlot   int    // stream index of the byte receiving bits
	bitPos    int    // next bit position in that byte (7..0), -1 = full
	firstBits int    // bits stored in the first byte (at most 7)
}

func (e *iceEncoder) putBit(b int) {
	if e.bitSlot == 0 && e.firstBits == 7 {
		e.bitPos = -1
	}
	if e.bitPos < 0 {
		e.bitSlot = len(e.stream)
		e.stream = append(e.stream, 0)
		e.bitPos = 7
	}
	e.stream[e.bitSlot] |= byte(b) << e.bitPos
	e.bitPos--
	if e.bitSlot == 0 {
		e.firstBits++
	}
}

func (e *iceEncoder) putBits(v, n int) {
	for i := n - 1; i >= 0; i-- {
		e.putBit(v >> i & 1)
	}
}

func (e *iceEncoder) putLiterals(lit []byte) {
	n := len(lit) - 1 // the decoder copies n+1 bytes
	switch {
	case n == 0:
		e.putBits(0b10, 2)
	default:
		e.putBits(0b11, 2)
		for i, c := range iceLiteralClasses {
			if n-c.base < c.allOnes || i == len(iceLiteralClasses)-1 {
				e.putBits(n-c.base, c.width)
				break
			}
			e.putBits(c.allOnes, c.width)
		}
	}
	// Literals are written back to front.
	for i := len(lit) - 1; i >= 0; i-- {
		e.stream = append(e.stream, lit[i])
	}
}

// putString encodes a copy of count bytes from distance dist above.
func (e *iceEncoder) putString(count, dist int) {
	length := count - 2
	switch {
	case length == 0:
		e.putBits(0b0, 1)
	case length == 1:
		e.putBits(0b10, 2)
	case length < 4:
		e.putBits(0b110, 3)
		e.putBits(length-2, 1)
	case length < 8:
		e.putBits(0b1110, 4)
		e.putBits(length-4, 2)
	default:
		e.putBits(0b1111, 4)
		e.putBits(length-8, 10)
	}
	offset := dist - count
	if dist == 1 {
		offset = -1
	}
	if length == 0 {
		if offset >= 0x3f {
			e.putBit(1)
			e.putBits(offset-0x3f, 9)
		} else {
			e.putBit(0)
			e.putBits(offset+1, 6)
		}
		return
	}
	switch {
	case offset >= 0x1f && offset < 0x1f+256:
		e.putBit(0)
		e.putBits(offset-0x1f, 8)
	case offset < 0x1f:
		e.putBits(0b10, 2)
		e.putBits(offset+1, 5)
	default:
		e.putBits(0b11, 2)
		e.putBits(offset-0x11f, 12)
	}
}

// findMatch returns the longest encodable copy for out[pos-count:pos] from
// above pos.
func findMatch(data []byte, pos int) (count, dist int) {
	for d := 1; d <= 2000 && pos+d <= len(data); d++ {
		n := 0
		for n < 1031 && pos-1-n >= 0 && pos-1-n+d < len(data) && data[pos-1-n] == data[pos-1-n+d] {
			n++
		}
		if n < 2 {
			continue
		}
		if d != 1 && d < n { // only a distance of 1 may overlap
			n = d
		}
		maxDist := n + 4382
		if n == 2 {
			maxDist = 2 + 574
		}
		if d != 1 && d > maxDist {
			continue
		}
		if n > count {
			count, dist = n, d
		}
	}
	return count, dist
}

func icePack(data []byte) []byte {
	e := &iceEncoder{stream: []byte{0}, bitPos: 7}

	pos := len(data)
	var lit []byte
	for pos > 0 {
		if count, dist := findMatch(data, pos); count >= 2 {
			if len(lit) > 0 {
				e.putLiterals(lit)
				lit = nil
			} else {
				e.putBit(0) // no literals before this string
			}
			e.putString(count, dist)
			pos -= count
			continue
		}
		pos--
		lit = append([]byte{data[pos]}, lit...)
		if len(lit) > 269+0x7fff {
			panic("test encoder: literal run too long")
		}
	}
	if len(lit) > 0 {
		e.putLiterals(lit)
	} else {
		e.putBit(0) // the decoder reads one more literal flag after a string
	}
	e.putBit(0)                           // no picture transform
	e.stream[0] |= 1 << (7 - e.firstBits) // marker below the first byte's bits

	out := make([]byte, iceHeaderSize+len(e.stream))
	copy(out, "ICE!")
	binary.BigEndian.PutUint32(out[4:], uint32(len(out)))
	binary.BigEndian.PutUint32(out[8:], uint32(len(data)))
	for i, b := range e.stream {
		out[len(out)-1-i] = b
	}
	return out
}

func TestICERoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	cases := map[string][]byte{
		"single":   {0x42},
		"short":    []byte("SNDH SNDH SNDH"),
		"run":      bytes.Repeat([]byte{0}, 5000),
		"text":     bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog. "), 200),
		"random":   make([]byte, 3000),
		"mixed":    nil,
		"longlits": make([]byte, 600),
	}
	for i := range cases["random"] {
		cases["random"][i] = byte(rng.IntN(256))
	}
	for i := range cases["longlits"] {
		cases["longlits"][i] = byte(i * 7)
	}
	var mixed []byte
	for i := range 400 {
		if rng.IntN(3) == 0 {
			mixed = append(mixed, bytes.Repeat([]byte{byte(i)}, rng.IntN(40)+1)...)
		} else {
			start := rng.IntN(max(len(mixed), 1))
			end := min(len(mixed), start+rng.IntN(300))
			mixed = append(mixed, mixed[start:end]...)
			mixed = append(mixed, byte(rng.IntN(256)))
		}
	}
	cases["mixed"] = mixed

	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			packed := icePack(data)
			if !IsICEPacked(packed) {
				t.Fatal("not recognised as ICE!")
			}
			got, err := DepackICE(packed)
			if err != nil {
				t.Fatalf("DepackICE: %v", err)
			}
			if !bytes.Equal(got, data) {
				t.Fatalf("round trip mismatch (%d bytes in, %d out)", len(data), len(got))
			}
		})
	}
}

func TestICERejectsCorruptData(t *testing.T) {
	packed := icePack(bytes.Repeat([]byte("corrupt me "), 100))

	if _, err := DepackICE(packed[:20]); err == nil {
		t.Error("truncated file accepted")
	}

	bad := bytes.Clone(packed)
	binary.BigEndian.PutUint32(bad[8:], 1<<30)
	if _, err := DepackICE(bad); err == nil {
		t.Error("absurd unpacked length accepted")
	}

	bad = bytes.Clone(packed)
	binary.BigEndian.PutUint32(bad[8:], binary.BigEndian.Uint32(packed[8:])+500)
	if _, err := DepackICE(bad); err == nil {
		t.Error("wrong unpacked length accepted")
	}

	if _, err := DepackICE([]byte("not packed at all")); err == nil {
		t.Error("unpacked data accepted")
	}
}

func TestICEPicture(t *testing.T) {
	// Each group of four words is read back to front and its bits are dealt
	// round-robin into four planes, so word 0's top bit ends up as bit 3 of
	// plane 0 and its second bit as bit 3 of plane 1.
	for _, tc := range []struct {
		in        byte
		wantIndex int
	}{{0x80, 0}, {0x40, 2}} {
		buf := make([]byte, 32000)
		buf[len(buf)-8] = tc.in
		icePicture(buf)
		want := make([]byte, 8)
		want[tc.wantIndex+1] = 0x08
		if got := buf[len(buf)-8:]; !bytes.Equal(got, want) {
			t.Errorf("input %#02x: got % x, want % x", tc.in, got, want)
		}
	}
}
