package ym

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"testing"
)

// bitWriter packs bits MSB-first, matching the reader in lzh.go.
type bitWriter struct {
	buf []byte
	acc uint32
	n   uint
}

func (w *bitWriter) put(nbits uint, val uint32) {
	w.acc = w.acc<<nbits | (val & (1<<nbits - 1))
	w.n += nbits
	for w.n >= 8 {
		w.n -= 8
		w.buf = append(w.buf, byte(w.acc>>w.n))
	}
}

func (w *bitWriter) bytes() []byte {
	if w.n > 0 {
		w.buf = append(w.buf, byte(w.acc<<(8-w.n)))
		w.n = 0
	}
	return append(w.buf, 0, 0) // trailing slack for lookahead peeks
}

// encodeLiterals emits a valid single-block LZH stream that codes every byte of
// data as an 8-bit canonical literal. The code-length tables use the reference
// decoder's degenerate ("n == 0") paths, so no per-symbol length bits are
// needed: the 256 c_len entries all resolve to 8 via a constant pt_table.
func encodeLiterals(data []byte, dicbit int) []byte {
	pbit := uint(4)
	if dicbit >= 15 {
		pbit = 5
	}
	w := &bitWriter{}
	w.put(16, uint32(len(data))) // blocksize
	w.put(lzhTBIT, 0)            // read_pt_len(NT): n == 0 ...
	w.put(lzhTBIT, 10)           // ... constant symbol 10 -> c_len entries become 10-2 = 8
	w.put(lzhCBIT, 256)          // read_c_len: 256 explicit entries
	w.put(pbit, 0)               // read_pt_len(NP): n == 0 ...
	w.put(pbit, 0)               // ... constant symbol 0
	for _, b := range data {
		w.put(8, uint32(b))
	}
	return w.bytes()
}

func TestLZHLiteralRoundTrip(t *testing.T) {
	for _, size := range []int{0 + 1, 7, 255, 256, 4096, 4097, 10000, 40000} {
		data := make([]byte, size)
		rng := rand.New(rand.NewSource(int64(size)))
		rng.Read(data)

		for _, dicbit := range []int{12, 13, 15, 16} {
			out, err := lzhDecode(encodeLiterals(data, dicbit), dicbit, len(data))
			if err != nil {
				t.Fatalf("size=%d dicbit=%d: %v", size, dicbit, err)
			}
			if !bytes.Equal(out, data) {
				t.Fatalf("size=%d dicbit=%d: round trip mismatch", size, dicbit)
			}
		}
	}
}

// TestLZHDegenerateBlock exercises the fully degenerate path where read_c_len
// also takes its n == 0 branch: every symbol decodes to one literal byte.
func TestLZHDegenerateBlock(t *testing.T) {
	const n, fill = 5000, byte(0xA7)
	w := &bitWriter{}
	w.put(16, n)                 // blocksize
	w.put(lzhTBIT, 0)            // NT lengths: n == 0
	w.put(lzhTBIT, 0)            //   constant symbol 0
	w.put(lzhCBIT, 0)            // C lengths: n == 0
	w.put(lzhCBIT, uint32(fill)) //   every code -> literal 0xA7
	w.put(4, 0)                  // NP lengths: n == 0
	w.put(4, 0)

	out, err := lzhDecode(w.bytes(), 13, n)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != n {
		t.Fatalf("len = %d, want %d", len(out), n)
	}
	for i, b := range out {
		if b != fill {
			t.Fatalf("byte %d = %#x, want %#x", i, b, fill)
		}
	}
}

func TestLZHRejectsGarbage(t *testing.T) {
	if _, err := lzhDecode(bytes.Repeat([]byte{0xff}, 64), 13, 4096); err == nil {
		t.Fatal("expected error decoding garbage")
	}
	if _, err := lzhDecode(nil, 13, 0); err == nil {
		t.Fatal("expected error for zero output size")
	}
}

// wrapLHArc prepends a level-0 LHArc header around a compressed payload.
func wrapLHArc(method string, payload []byte, origSize int) []byte {
	name := []byte("song.ym")
	h := make([]byte, 22+len(name)+2)
	h[0] = byte(22 + len(name)) // data starts at h[0]+2 == len(h)
	copy(h[2:7], method)
	binary.LittleEndian.PutUint32(h[7:11], uint32(len(payload)))
	binary.LittleEndian.PutUint32(h[11:15], uint32(origSize))
	h[20] = 0 // header level 0
	h[21] = byte(len(name))
	copy(h[22:], name)
	return append(h, payload...)
}

func TestMaybeDepack(t *testing.T) {
	payload := []byte("YM3!not really but distinctive enough for a test")

	// Uncompressed input passes straight through.
	if got, err := maybeDepack(payload); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("passthrough: %v / %q", err, got)
	}

	// -lh5- container is transparently expanded.
	wrapped := wrapLHArc("-lh5-", encodeLiterals(payload, 13), len(payload))
	got, err := maybeDepack(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("depack mismatch: %q", got)
	}

	// -lh0- stored container.
	stored := wrapLHArc("-lh0-", payload, len(payload))
	if got, err = maybeDepack(stored); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("stored: %v / %q", err, got)
	}
}
