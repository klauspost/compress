package zstd

import "io"

type byteBuf []byte

func (b *byteBuf) readN(n int) []byte {
	bb := *b
	if len(bb) < n {
		return nil
	}
	r := bb[:n]
	*b = bb[n:]
	return r
}
func (b *byteBuf) remain() []byte {
	return *b
}

func (b *byteBuf) readByte() (byte, error) {
	bb := *b
	if len(bb) < 1 {
		return 0, nil
	}
	r := bb[0]
	*b = bb[1:]
	return r, nil
}

func (b *byteBuf) skipN(n int) error {
	bb := *b
	if len(bb) < n {
		return io.ErrUnexpectedEOF
	}
	*b = bb[n:]
	return nil
}
