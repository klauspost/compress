package fse

// bitReader reads a bitstream in reverse.
type bitReader struct {
	in       []byte
	off      uint
	value    uint64
	bitsRead uint8
}

func (b *bitReader) getBits(n uint8) uint32 {
	const regMask = 64 - 1
	return uint32(((b.value << (b.bitsRead & regMask)) >> 1) >> ((regMask - n) & regMask))
}
