package fse

import "fmt"

type bitWriter struct {
	bitContainer uint64
	nBits        uint8
	out          []byte
}

var bitMask32 = [32]uint32{
	0, 1, 3, 7, 0xF, 0x1F,
	0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF,
	0xFFF, 0x1FFF, 0x3FFF, 0x7FFF, 0xFFFF, 0x1FFFF,
	0x3FFFF, 0x7FFFF, 0xFFFFF, 0x1FFFFF, 0x3FFFFF, 0x7FFFFF,
	0xFFFFFF, 0x1FFFFFF, 0x3FFFFFF, 0x7FFFFFF, 0xFFFFFFF, 0x1FFFFFFF,
	0x3FFFFFFF, 0x7FFFFFFF}

// bitMask16 is bitmasks. Has extra to avoid bounds check.
var bitMask16 = [32]uint16{
	0, 1, 3, 7, 0xF, 0x1F,
	0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF,
	0xFFF, 0x1FFF, 0x3FFF, 0x7FFF, 0xFFFF, 0xFFFF,
	0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF,
	0xFFFF, 0xFFFF} /* up to 16 bits */

func (b *bitWriter) addBits(value uint32, bits uint8) {
	if b.nBits+bits >= 64 {
		b.flush32()
	}
	b.bitContainer |= uint64(value&bitMask32[bits]) << (b.nBits & 63)
	b.nBits += bits
}

func (b *bitWriter) addBits16NC(value uint16, bits uint8) {
	b.bitContainer |= uint64(value&bitMask16[bits&31]) << (b.nBits & 63)
	b.nBits += bits
}

// flush will
func (b *bitWriter) flush() {
	v := b.nBits >> 3
	switch v {
	case 0:
	case 1:
		b.out = append(b.out,
			byte(b.bitContainer),
		)
	case 2:
		b.out = append(b.out,
			byte(b.bitContainer),
			byte(b.bitContainer>>8),
		)
	case 3:
		b.out = append(b.out,
			byte(b.bitContainer),
			byte(b.bitContainer>>8),
			byte(b.bitContainer>>16),
		)
	case 4:
		b.out = append(b.out,
			byte(b.bitContainer),
			byte(b.bitContainer>>8),
			byte(b.bitContainer>>16),
			byte(b.bitContainer>>24),
		)
	case 5:
		b.out = append(b.out,
			byte(b.bitContainer),
			byte(b.bitContainer>>8),
			byte(b.bitContainer>>16),
			byte(b.bitContainer>>24),
			byte(b.bitContainer>>32),
		)
	case 6:
		b.out = append(b.out,
			byte(b.bitContainer),
			byte(b.bitContainer>>8),
			byte(b.bitContainer>>16),
			byte(b.bitContainer>>24),
			byte(b.bitContainer>>32),
			byte(b.bitContainer>>40),
		)
	case 7:
		b.out = append(b.out,
			byte(b.bitContainer),
			byte(b.bitContainer>>8),
			byte(b.bitContainer>>16),
			byte(b.bitContainer>>24),
			byte(b.bitContainer>>32),
			byte(b.bitContainer>>40),
			byte(b.bitContainer>>48),
		)
	case 8:
		b.out = append(b.out,
			byte(b.bitContainer),
			byte(b.bitContainer>>8),
			byte(b.bitContainer>>16),
			byte(b.bitContainer>>24),
			byte(b.bitContainer>>32),
			byte(b.bitContainer>>40),
			byte(b.bitContainer>>48),
			byte(b.bitContainer>>56),
		)
	default:
		panic(fmt.Errorf("bits (%d) > 64", b.nBits))
	}
	b.bitContainer >>= v << 3
	b.nBits &= 7
}

// flush32 will flush out, so there are at least 32 bits available for writing.
func (b *bitWriter) flush32() {
	if b.nBits < 32 {
		return
	}
	b.out = append(b.out,
		byte(b.bitContainer),
		byte(b.bitContainer>>8),
		byte(b.bitContainer>>16),
		byte(b.bitContainer>>24))
	b.nBits -= 32
	b.bitContainer >>= 32
}

// flushAlign will flush remaining full bytes and align to byte boundary.
// May leave bits.
func (b *bitWriter) flushAlign() {
	nbBytes := b.nBits >> 3
	for i := uint8(0); i < nbBytes; i++ {
		b.out = append(b.out, byte(b.bitContainer))
	}
	b.nBits &= 7
	b.bitContainer >>= nbBytes * 8
}

func (b *bitWriter) close() error {
	// End mark
	b.addBits(1, 1)
	b.flushAlign()
	return nil
}

// reset and continue writing by appending to out.
func (b *bitWriter) reset(out []byte) {
	b.bitContainer = 0
	b.nBits = 0
	b.out = out
}
