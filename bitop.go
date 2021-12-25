// Bit-level operations

package main

type Bitop struct {
	buffer []byte
	buflen uint32
	bufpos uint32
	bufoff uint32
	iserro bool
}

func createBitop(buffer []byte) Bitop {
	return Bitop{
		buffer: buffer,
		buflen: uint32(len(buffer)),
		bufpos: 0,
		bufoff: 0,
		iserro: false,
	}
}

func (b Bitop) Read(n uint32) uint32 {
	var v uint32
	var d uint32

	v = 0
	d = 0

	for n > 0 {
		if n < 0 || b.bufpos >= b.buflen {
			b.iserro = true
			return 0
		}

		b.iserro = false

		if b.bufoff+n > 8 {
			d = 8 - b.bufoff
		} else {
			d = n
		}

		v <<= d
		v += uint32((b.buffer[b.bufpos] >> byte(8-b.bufoff-d)) & (0xff >> byte(8-d)))

		b.bufoff += d
		n -= d

		if b.bufoff == 8 {
			b.bufpos++
			b.bufoff = 0
		}
	}

	return v
}

func (b Bitop) Look(n uint32) uint32 {
	var p uint32
	var o uint32
	var v uint32

	p = b.bufpos
	o = b.bufoff

	v = b.Read(n)

	b.bufpos = p
	b.bufoff = o

	return v
}

func (b Bitop) ReadGolomb() uint32 {
	var n uint32

	n = 0

	for b.Read(1) == 0 && !b.iserro {
		n++
	}

	return (1 << n) + b.Read(n) - 1
}
