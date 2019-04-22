package zstd

import (
	"fmt"
	"io"
	"io/ioutil"
)

type byteBuffer interface {
	// Read up to 8 bytes.
	// Returns nil if no more input is available.
	readSmall(n int) []byte

	// Read >8 bytes.
	// MAY use the destination slice.
	readBig(n int, dst []byte) ([]byte, error)

	// Read a single byte.
	readByte() (byte, error)

	// Skip n bytes.
	skipN(n int) error
}

// in-memory buffer
type byteBuf []byte

func (b *byteBuf) readSmall(n int) []byte {
	if debug && n > 8 {
		panic(fmt.Errorf("small read > 8 (%d). use readBig", n))
	}
	bb := *b
	if len(bb) < n {
		return nil
	}
	r := bb[:n]
	*b = bb[n:]
	return r
}

func (b *byteBuf) readBig(n int, dst []byte) ([]byte, error) {
	bb := *b
	if len(bb) < n {
		return nil, io.ErrUnexpectedEOF
	}
	r := bb[:n]
	*b = bb[n:]
	return r, nil
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

// wrapper around a reader.
type readerWrapper struct {
	r   io.Reader
	tmp [8]byte
}

func (r *readerWrapper) readSmall(n int) []byte {
	if debug && n > 8 {
		panic(fmt.Errorf("small read > 8 (%d). use readBig", n))
	}
	n2, _ := r.r.Read(r.tmp[:n])
	// We only really care about the actual bytes read.
	if n2 != n {
		return nil
	}
	return r.tmp[:n]
}

func (r *readerWrapper) readBig(n int, dst []byte) ([]byte, error) {
	if cap(dst) < n {
		dst = make([]byte, n)
	}
	n2, err := io.ReadFull(r.r, dst[:n])
	return dst[:n2], err
}

func (r *readerWrapper) readByte() (byte, error) {
	n2, err := r.r.Read(r.tmp[:1])
	if err != nil {
		return 0, err
	}
	if n2 != 1 {
		return 0, io.ErrUnexpectedEOF
	}
	return r.tmp[0], nil
}

func (r *readerWrapper) skipN(n int) error {
	_, err := io.CopyN(ioutil.Discard, r.r, int64(n))
	return err
}
