package fse

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/OneOfOne/xxhash"
)

type Reader struct {
	s            Scratch
	br           *bufio.Reader
	out          []byte
	cBuffer      []byte
	read         int
	maxBlockSize uint64
	h            xxhash.XXHash64
	eof          bool
}

func NewReader(rd io.Reader) (*Reader, error) {
	r := Reader{}
	return &r, r.Reset(rd)
}

func (r *Reader) Reset(rd io.Reader) error {
	byter, ok := rd.(*bufio.Reader)
	if !ok {
		if r.br == nil {
			byter = bufio.NewReader(rd)
		} else {
			r.br.Reset(rd)
			byter = r.br
		}
	}
	r.br = byter
	var tmp [4]byte
	var err error
	for i := range tmp[:] {
		tmp[i], err = byter.ReadByte()
		if err != nil {
			return err
		}
	}
	magic := binary.LittleEndian.Uint32(tmp[:])
	if magic != streamMagicNumber {
		return errors.New("magic number mismatch")
	}
	r.maxBlockSize, err = binary.ReadUvarint(r.br)
	if err != nil {
		return err
	}
	if r.maxBlockSize > streamBlockSizeLimit || r.maxBlockSize == 0 {
		return fmt.Errorf("invalid blocksize %d", r.maxBlockSize)
	}
	if cap(r.out) < int(r.maxBlockSize) {
		r.out = make([]byte, 0, r.maxBlockSize)
	}
	r.out = r.out[:0]
	if cap(r.cBuffer) < int(r.maxBlockSize) {
		r.cBuffer = make([]byte, 0, r.maxBlockSize)
	}
	r.cBuffer = r.cBuffer[:0]
	r.h.Reset()
	r.read = 0
	r.eof = false
	return nil
}

func (r *Reader) Read(p []byte) (n int, err error) {
	read := 0
	for read < len(p) {
		if r.read >= len(r.out) {
			if r.eof {
				return 0, io.EOF
			}
			err := r.decodeNext()
			if err != nil {
				if err == io.EOF {
					// Postpone EOF one read.
					r.eof = true
					return read, nil
				}
				return read, err
			}
		}
		n := copy(p[read:], r.out[r.read:])
		r.read += n
		read += n
	}
	return read, nil
}

func (r *Reader) decodeNext() error {
	block, err := r.br.ReadByte()
	if err != nil {
		return err
	}
	r.read = 0
	switch block {
	case blockTypeRaw:
		size, err := binary.ReadUvarint(r.br)
		if err != nil {
			return err
		}
		if size > r.maxBlockSize {
			return fmt.Errorf("invalid block size: %d", size)
		}
		r.out = r.out[:size]
		_, err = io.ReadFull(r.br, r.out)
		r.h.Write(r.out)
		return err
	case blockTypeRLE:
		value, err := r.br.ReadByte()
		if err != nil {
			return err
		}
		size, err := binary.ReadUvarint(r.br)
		if err != nil {
			return err
		}
		if size > r.maxBlockSize {
			return fmt.Errorf("invalid block size: %d", size)
		}
		r.out = r.out[:size]
		for i := range r.out {
			r.out[i] = value
		}
		r.h.Write(r.out)
		return nil
	case blockTypeCompressed:
		size, err := binary.ReadUvarint(r.br)
		if err != nil {
			return err
		}
		if size > r.maxBlockSize {
			return fmt.Errorf("invalid block size: %d", size)
		}
		cSize, err := binary.ReadUvarint(r.br)
		if err != nil {
			return err
		}
		if cSize > size {
			return fmt.Errorf("invalid compressed block size: %d", cSize)
		}
		r.s.Out = r.out[:0]
		r.cBuffer = r.cBuffer[:cSize]
		_, err = io.ReadFull(r.br, r.cBuffer)
		if err != nil {
			return err
		}
		o, err := Decompress(r.cBuffer, &r.s)
		if err != nil {
			return err
		}
		r.out = o
		r.h.Write(r.out)
		return nil
	case blockTypeCRC:
		var tmp [8]byte
		_, err = io.ReadFull(r.br, tmp[:])
		if err != nil {
			return err
		}
		got := r.h.Sum(nil)
		if !bytes.Equal(got, tmp[:]) {
			return fmt.Errorf("CRC mismatch, stream corrupted got: %x != want: %x", got, tmp[:])
		}
		r.out = r.out[:0]
		return nil
	case blockTypeEOS:
		r.out = r.out[:0]
		return io.EOF
	}
	return fmt.Errorf("unknown block type %d", block)
}
