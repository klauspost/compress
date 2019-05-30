// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"io"

	"github.com/cespare/xxhash"
)

// Encoder provides encoding to Zstandard
type Encoder struct {
	o     encoderOptions
	enc   fastEncoder
	block *blockEnc
	crc   *xxhash.Digest
	tmp   [8]byte
}

// NewWriter will create a new Zstandard encoder.
// If the encoder will be used for encoding blocks a nil writer can be used.
func NewWriter(w io.Writer, opts ...EOption) (*Encoder, error) {
	var e Encoder
	e.o.setDefault()
	for _, o := range opts {
		err := o(&e.o)
		if err != nil {
			return nil, err
		}
	}

	return &e, nil
}

// EncodeAll will encode all input in src and append it to dst.
// If empty input is given, nothing is returned.
func (e *Encoder) EncodeAll(src, dst []byte) []byte {
	if len(src) == 0 {
		return dst
	}
	if e.block == nil {
		e.block = &blockEnc{}
		e.block.init()
	}
	e.block.initNewEncode()
	e.enc.Reset()
	fh := frameHeader{
		ContentSize:   uint64(len(src)),
		WindowSize:    maxStoreBlockSize * 2,
		SingleSegment: false,
		Checksum:      e.o.crc,
		DictID:        0,
	}
	dst, err := fh.appendTo(dst)
	if err != nil {
		panic(err)
	}
	if e.crc == nil {
		e.crc = xxhash.New()
	} else {
		e.crc.Reset()
	}
	for len(src) > 0 {
		todo := src
		if len(todo) > maxStoreBlockSize {
			todo = todo[:maxStoreBlockSize]
		}
		src = src[len(todo):]
		if e.o.crc {
			_, _ = e.crc.Write(todo)
		}
		e.block.reset()
		e.block.pushOffsets()
		e.enc.Encode(e.block, todo)
		if len(src) == 0 {
			e.block.last = true
		}
		err := e.block.encode()
		switch err {
		case errIncompressible:
			if debug {
				println("Storing uncompressible block as raw")
			}
			e.block.encodeRaw(todo)
			e.block.popOffsets()
		case nil:
		default:
			panic(err)
		}
		dst = append(dst, e.block.output...)
	}
	if e.o.crc {
		crc := e.crc.Sum(e.tmp[:0])
		dst = append(dst, crc[7], crc[6], crc[5], crc[4])
	}
	e.enc.Reset()
	return dst
}
