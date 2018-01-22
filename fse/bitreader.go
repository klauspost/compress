package fse

import (
	"fmt"
	"io"
)

// bitReader reads a bitstream in reverse.
type bitReader struct {
	in       []byte
	off      uint // next byte to read is at in[off - 1]
	value    uint64
	bitsRead uint8
}

// init initializes and resets the bitreader.
//
func (b *bitReader) init(in []byte) {
	b.in = in
	b.off = uint(len(in))
	b.bitsRead = 64
	b.value = 0
	b.fill()
	b.fill()
	fmt.Println("first bytes: ", uint8(b.value<<56), uint8(b.value<<48))
}

// getBits will return n bits.
func (b *bitReader) getBits(n uint8) uint16 {
	const regMask = 64 - 1
	// attempt using Go built-in shift check.... Probably slower.
	// return uint16((b.value << (b.bitsRead & regMask)) >> (regMask - n))
	v := uint16(((b.value << (b.bitsRead & regMask)) >> 1) >> ((regMask - n) & regMask))
	b.bitsRead += n
	return v
}

// getBitsFast requires that at least one bit is requested every time.
func (b *bitReader) getBitsFast(n uint8) uint16 {
	const regMask = 64 - 1
	v := uint16((b.value << (b.bitsRead & regMask)) >> (((regMask + 1) - n) & regMask))
	//v := uint16((b.value << (b.bitsRead)) >> (((regMask + 1) - n)))
	b.bitsRead += n
	return v
}

// fill() will make sure at least 32 bits are available.
func (b *bitReader) fill() {
	if b.bitsRead < 32 {
		return
	}
	if b.off > 4 {
		b.value = (b.value << 32) | (uint64(b.in[b.off-1]) << 24) | (uint64(b.in[b.off-2]) << 16) | (uint64(b.in[b.off-3]) << 8) | (uint64(b.in[b.off-4]) << 8)
		b.bitsRead &= 31
		b.off -= 4
		return
	}
	for b.off > 0 {
		b.value = (b.value << 8) | uint64(b.in[b.off-1])
		b.bitsRead -= 8
		b.off--
	}
}

func (b *bitReader) finished() bool {
	return b.off == 0 && b.bitsRead >= 64
}

// close the bitstream and returns an error if out-of-buffer reads occurred.
func (b *bitReader) close() error {
	// Release reference.
	b.in = nil
	if b.bitsRead > 64 {
		return io.ErrUnexpectedEOF
	}
	return nil
}
