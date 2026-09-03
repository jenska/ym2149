package ym

import "errors"

// LZH (LHarc) depacker for the -lh4-/-lh5-/-lh6-/-lh7- methods used to compress
// YM register-dump files.
//
// This is a straight port of the public-domain LZH decoder by Haruhiko Okumura
// (1991) and Kerwin F. Medina (1996), as adapted for ST-Sound by Arnaud Carré.
// The four methods differ only in the sliding-dictionary size (and, for the
// larger windows, the width of the position-length prefix code); the entropy
// coding is identical.

var errCorruptLZH = errors.New("ym: corrupt LZH stream")

const (
	lzhNC   = 510 // UCHAR_MAX + MAXMATCH + 2 - THRESHOLD
	lzhNT   = 19  // CODE_BIT + 3
	lzhCBIT = 9
	lzhTBIT = 5

	lzhThreshold = 3
)

type lzhReader struct {
	src  []byte
	spos int

	bitbuf   uint32 // 16-bit lookahead window (C "ushort")
	subbuf   uint32 // current partially consumed source byte
	bitcount int
}

func (r *lzhReader) dataIn() (byte, bool) {
	if r.spos >= len(r.src) {
		return 0, false
	}
	b := r.src[r.spos]
	r.spos++
	return b, true
}

// fillbuf shifts the window left by n bits and refills the low end from the
// source, mirroring the reference io.c routine (including its harmless n==0
// no-op behaviour).
func (r *lzhReader) fillbuf(n int) {
	r.bitbuf = (r.bitbuf << uint(n)) & 0xffff
	for n > r.bitcount {
		n -= r.bitcount
		r.bitbuf = (r.bitbuf | (r.subbuf << uint(n))) & 0xffff
		if b, ok := r.dataIn(); ok {
			r.subbuf = uint32(b)
		} else {
			r.subbuf = 0
		}
		r.bitcount = 8
	}
	r.bitcount -= n
	r.bitbuf = (r.bitbuf | (r.subbuf >> uint(r.bitcount))) & 0xffff
}

func (r *lzhReader) getbits(n int) uint32 {
	x := r.bitbuf >> uint(16-n)
	r.fillbuf(n)
	return x
}

func (r *lzhReader) initGetbits() {
	r.bitbuf = 0
	r.subbuf = 0
	r.bitcount = 0
	r.fillbuf(16)
}

type lzhDepacker struct {
	r lzhReader

	dicbit int
	dicsiz int
	np     int
	pbit   int

	outbuf []byte

	left  [2 * lzhNC]uint16
	right [2 * lzhNC]uint16

	cLen  [lzhNC]byte
	ptLen [lzhNT + 8]byte

	cTable  [4096]uint16
	ptTable [256]uint16

	blocksize int

	decodeI int
	decodeJ int

	err error
}

func (d *lzhDepacker) fail() {
	if d.err == nil {
		d.err = errCorruptLZH
	}
	panic(d.err)
}

func (d *lzhDepacker) makeTable(nchar int, bitlen []byte, tablebits int, table []uint16) {
	var count [17]uint32
	var weight [17]uint32
	var start [18]uint32

	for i := range nchar {
		count[bitlen[i]]++
	}
	for i := 1; i <= 16; i++ {
		start[i+1] = (start[i] + (count[i] << uint(16-i))) & 0xffff
	}
	if start[17] != 0 {
		d.fail()
	}

	jutbits := uint(16 - tablebits)
	for i := 1; i <= tablebits; i++ {
		start[i] >>= jutbits
		weight[i] = 1 << uint(tablebits-i)
	}
	for i := tablebits + 1; i <= 16; i++ {
		weight[i] = 1 << uint(16-i)
	}

	// Zero the slots that no short code reaches; long codes populate them with
	// internal-node indices below.
	for i := start[tablebits+1] >> jutbits; int(i) < (1 << uint(tablebits)); i++ {
		table[i] = 0
	}

	avail := nchar
	mask := uint32(1) << uint(15-tablebits)
	for ch := range nchar {
		length := int(bitlen[ch])
		if length == 0 {
			continue
		}
		nextcode := start[length] + weight[length]
		if length <= tablebits {
			hi := min(nextcode, uint32(len(table)))
			for i := start[length]; i < hi; i++ {
				table[i] = uint16(ch)
			}
		} else {
			k := start[length]
			arr, pos := 0, int(k>>jutbits) // 0: table, 1: left, 2: right
			for i := length - tablebits; i != 0; i-- {
				cur := d.node(arr, pos, table)
				if cur == 0 {
					d.right[avail] = 0
					d.left[avail] = 0
					d.setNode(arr, pos, table, uint16(avail))
					cur = uint16(avail)
					avail++
					if avail >= len(d.left) {
						d.fail()
					}
				}
				if k&mask != 0 {
					arr, pos = 2, int(cur)
				} else {
					arr, pos = 1, int(cur)
				}
				k <<= 1
			}
			d.setNode(arr, pos, table, uint16(ch))
		}
		start[length] = (nextcode) & 0xffff
	}
}

func (d *lzhDepacker) node(arr, pos int, table []uint16) uint16 {
	switch arr {
	case 0:
		if pos < 0 || pos >= len(table) {
			d.fail()
		}
		return table[pos]
	case 1:
		return d.left[pos]
	default:
		return d.right[pos]
	}
}

func (d *lzhDepacker) setNode(arr, pos int, table []uint16, v uint16) {
	switch arr {
	case 0:
		if pos < 0 || pos >= len(table) {
			d.fail()
		}
		table[pos] = v
	case 1:
		d.left[pos] = v
	default:
		d.right[pos] = v
	}
}

func (d *lzhDepacker) readPtLen(nn, nbit, ispecial int) {
	n := int(d.r.getbits(nbit))
	if n == 0 {
		c := uint16(d.r.getbits(nbit))
		for i := range nn {
			d.ptLen[i] = 0
		}
		for i := range d.ptTable {
			d.ptTable[i] = c
		}
		return
	}

	i := 0
	for i < n {
		c := int(d.r.bitbuf >> (16 - 3))
		if c == 7 {
			mask := uint32(1) << (16 - 1 - 3)
			for mask&d.r.bitbuf != 0 {
				mask >>= 1
				c++
			}
		}
		if c < 7 {
			d.r.fillbuf(3)
		} else {
			d.r.fillbuf(c - 3)
		}
		if i >= len(d.ptLen) {
			d.fail()
		}
		d.ptLen[i] = byte(c)
		i++
		if i == ispecial {
			z := int(d.r.getbits(2))
			for z > 0 {
				if i >= len(d.ptLen) {
					d.fail()
				}
				d.ptLen[i] = 0
				i++
				z--
			}
		}
	}
	for i < nn {
		if i >= len(d.ptLen) {
			d.fail()
		}
		d.ptLen[i] = 0
		i++
	}
	d.makeTable(nn, d.ptLen[:], 8, d.ptTable[:])
}

func (d *lzhDepacker) readCLen() {
	n := int(d.r.getbits(lzhCBIT))
	if n == 0 {
		c := uint16(d.r.getbits(lzhCBIT))
		for i := range d.cLen {
			d.cLen[i] = 0
		}
		for i := range d.cTable {
			d.cTable[i] = c
		}
		return
	}

	i := 0
	for i < n {
		c := int(d.ptTable[d.r.bitbuf>>(16-8)])
		if c >= lzhNT {
			mask := uint32(1) << (16 - 1 - 8)
			for {
				if d.r.bitbuf&mask != 0 {
					c = int(d.right[c])
				} else {
					c = int(d.left[c])
				}
				mask >>= 1
				if c < lzhNT || mask == 0 {
					break
				}
			}
		}
		d.r.fillbuf(int(d.ptLen[c]))
		if c <= 2 {
			switch c {
			case 0:
				c = 1
			case 1:
				c = int(d.r.getbits(4)) + 3
			default:
				c = int(d.r.getbits(lzhCBIT)) + 20
			}
			for c > 0 {
				if i >= lzhNC {
					d.fail()
				}
				d.cLen[i] = 0
				i++
				c--
			}
		} else {
			if i >= lzhNC {
				d.fail()
			}
			d.cLen[i] = byte(c - 2)
			i++
		}
	}
	for i < lzhNC {
		d.cLen[i] = 0
		i++
	}
	d.makeTable(lzhNC, d.cLen[:], 12, d.cTable[:])
}

func (d *lzhDepacker) decodeC() uint16 {
	if d.blocksize == 0 {
		d.blocksize = int(d.r.getbits(16))
		d.readPtLen(lzhNT, lzhTBIT, 3)
		d.readCLen()
		d.readPtLen(d.np, d.pbit, -1)
	}
	d.blocksize--

	j := d.cTable[d.r.bitbuf>>(16-12)]
	if int(j) >= lzhNC {
		mask := uint32(1) << (16 - 1 - 12)
		for {
			if d.r.bitbuf&mask != 0 {
				j = d.right[j]
			} else {
				j = d.left[j]
			}
			mask >>= 1
			if int(j) < lzhNC || mask == 0 {
				break
			}
		}
	}
	d.r.fillbuf(int(d.cLen[j]))
	return j
}

func (d *lzhDepacker) decodeP() uint16 {
	j := d.ptTable[d.r.bitbuf>>(16-8)]
	if int(j) >= d.np {
		mask := uint32(1) << (16 - 1 - 8)
		for {
			if d.r.bitbuf&mask != 0 {
				j = d.right[j]
			} else {
				j = d.left[j]
			}
			mask >>= 1
			if int(j) < d.np || mask == 0 {
				break
			}
		}
	}
	d.r.fillbuf(int(d.ptLen[j]))
	if j != 0 {
		v := uint32(1)<<(uint(j)-1) + d.r.getbits(int(j)-1)
		j = uint16(v)
	}
	return j
}

func (d *lzhDepacker) decode(count int) {
	buf := d.outbuf
	mask := d.dicsiz - 1
	r := 0

	for {
		d.decodeJ--
		if d.decodeJ < 0 {
			break
		}
		buf[r] = buf[d.decodeI]
		d.decodeI = (d.decodeI + 1) & mask
		r++
		if r == count {
			return
		}
	}

	for {
		c := int(d.decodeC())
		if c <= 255 {
			buf[r] = byte(c)
			r++
			if r == count {
				return
			}
			continue
		}
		d.decodeJ = c - (256 - lzhThreshold)
		d.decodeI = (r - int(d.decodeP()) - 1) & mask
		for {
			d.decodeJ--
			if d.decodeJ < 0 {
				break
			}
			buf[r] = buf[d.decodeI]
			d.decodeI = (d.decodeI + 1) & mask
			r++
			if r == count {
				return
			}
		}
	}
}

// lzhDecode expands src (a raw LZH stream, no container header) into exactly
// outSize bytes. dicbit is the sliding-dictionary width: 12 (-lh4-), 13
// (-lh5-), 15 (-lh6-) or 16 (-lh7-).
func lzhDecode(src []byte, dicbit, outSize int) (out []byte, err error) {
	if outSize <= 0 {
		return nil, errCorruptLZH
	}
	d := &lzhDepacker{
		dicbit: dicbit,
		dicsiz: 1 << uint(dicbit),
		np:     dicbit + 1,
		pbit:   4,
	}
	if dicbit >= 15 {
		d.pbit = 5
	}
	d.outbuf = make([]byte, d.dicsiz)
	d.r.src = src

	defer func() {
		if rec := recover(); rec != nil {
			out, err = nil, errCorruptLZH
		}
	}()

	d.r.initGetbits()

	dst := make([]byte, outSize)
	for done := 0; done < outSize; {
		n := min(outSize-done, d.dicsiz)
		d.decode(n)
		copy(dst[done:done+n], d.outbuf[:n])
		done += n
	}
	return dst, nil
}
