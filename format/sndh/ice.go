package sndh

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// iceHeaderSize is the 'ICE!' magic plus the packed and unpacked length longs.
const iceHeaderSize = 12

// IsICEPacked reports whether data starts with an ICE! 2.4 header.
func IsICEPacked(data []byte) bool {
	return len(data) >= iceHeaderSize && string(data[:4]) == "ICE!"
}

var errICECorrupt = errors.New("sndh: corrupt ICE! data")

// iceDepacker mirrors the register usage of the original 68000 ICE! 2.4
// decruncher: the packed stream and the output are both consumed back to front.
type iceDepacker struct {
	src  []byte
	sp   int // a5: read position, predecremented
	out  []byte
	dp   int // a6: write position, predecremented
	bits byte
	err  error
}

// DepackICE decompresses an ICE! 2.4 packed buffer.
func DepackICE(data []byte) ([]byte, error) {
	if !IsICEPacked(data) {
		return nil, fmt.Errorf("sndh: not ICE! packed")
	}
	packed := int(binary.BigEndian.Uint32(data[4:8]))
	unpacked := int(binary.BigEndian.Uint32(data[8:12]))
	if packed <= iceHeaderSize || packed > len(data) {
		return nil, fmt.Errorf("sndh: ICE! packed length %d out of range (file is %d bytes)", packed, len(data))
	}
	if unpacked <= 0 || unpacked > 16<<20 {
		return nil, fmt.Errorf("sndh: ICE! unpacked length %d out of range", unpacked)
	}

	d := &iceDepacker{
		src: data[:packed],
		sp:  packed,
		out: make([]byte, unpacked),
		dp:  unpacked,
	}
	d.bits = d.readByte()
	d.run()
	if d.err != nil {
		return nil, d.err
	}
	if d.dp != 0 {
		return nil, errICECorrupt
	}
	if d.bit() == 1 && unpacked >= 32000 {
		icePicture(d.out[unpacked-32000:])
	}
	return d.out, nil
}

func (d *iceDepacker) readByte() byte {
	if d.sp <= iceHeaderSize {
		d.err = errICECorrupt
		return 0
	}
	d.sp--
	return d.src[d.sp]
}

func (d *iceDepacker) writeByte(b byte) {
	if d.dp <= 0 {
		d.err = errICECorrupt
		return
	}
	d.dp--
	d.out[d.dp] = b
}

// bit returns the next bit. The bit buffer keeps a trailing marker 1 so an
// empty buffer shifts out to zero, exactly like add.b d7,d7 / addx.b d7,d7.
func (d *iceDepacker) bit() int {
	carry := d.bits >> 7
	d.bits <<= 1
	if d.bits != 0 {
		return int(carry)
	}
	b := d.readByte()
	d.bits = b<<1 | carry
	return int(b >> 7)
}

// bitsN reads n bits, most significant first.
func (d *iceDepacker) bitsN(n int) int {
	v := 0
	for range n {
		v = v<<1 | d.bit()
	}
	return v
}

// Literal run length classes, tried from the shortest: read width bits, stop
// unless all ones, and add base.
var iceLiteralClasses = [...]struct{ width, allOnes, base int }{
	{2, 3, 1}, {2, 3, 4}, {3, 7, 7}, {8, 255, 14}, {15, 0x7fff, 269},
}

func (d *iceDepacker) run() {
	for d.err == nil {
		// Literal bytes.
		if d.bit() == 1 {
			n := 0
			if d.bit() == 1 {
				for _, c := range iceLiteralClasses {
					n = d.bitsN(c.width)
					if n != c.allOnes {
						n += c.base
						break
					}
					n += c.base
				}
			}
			for range n + 1 {
				d.writeByte(d.readByte())
			}
		}
		if d.dp <= 0 || d.err != nil {
			return
		}
		d.copyString()
	}
}

func (d *iceDepacker) copyString() {
	// String length minus two: 0, 1, 2+1 bit, 4+2 bits or 8+10 bits.
	cls := 0
	for cls < 4 && d.bit() == 1 {
		cls++
	}
	var length int
	switch cls {
	case 0:
		length = 0
	case 1:
		length = 1
	case 2:
		length = 2 + d.bitsN(1)
	case 3:
		length = 4 + d.bitsN(2)
	default:
		length = 8 + d.bitsN(10)
	}

	var offset int
	if length == 0 {
		if d.bit() == 1 {
			offset = d.bitsN(9) + 0x3f
		} else {
			offset = d.bitsN(6) - 1
		}
	} else {
		switch {
		case d.bit() == 0:
			offset = d.bitsN(8) + 0x1f
		case d.bit() == 0:
			offset = d.bitsN(5) - 1
		default:
			offset = d.bitsN(12) + 0x11f
		}
		if offset < 0 {
			offset -= length
		}
	}

	src := d.dp + length + offset + 2
	for range length + 2 {
		src--
		if src < 0 || src >= len(d.out) {
			d.err = errICECorrupt
			return
		}
		d.writeByte(d.out[src])
	}
}

// icePicture undoes ICE!'s optional bitplane transform of a 32000-byte
// low-resolution screen.
func icePicture(buf []byte) {
	p := len(buf)
	for range 4000 {
		var planes [4]uint16
		for range 4 {
			p -= 2
			w := binary.BigEndian.Uint16(buf[p:])
			for range 4 {
				for i := range planes {
					planes[i] = planes[i]<<1 | w>>15
					w <<= 1
				}
			}
		}
		for i, v := range planes {
			binary.BigEndian.PutUint16(buf[p+2*i:], v)
		}
	}
}
