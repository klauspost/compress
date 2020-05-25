package zstd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

type dict struct {
	id uint32
	// llEnc, ofEnc, mlEnc *fseEncoder
	llDec, ofDec, mlDec sequenceDec
	offsets             [3]int
	content             []byte
}

var dictMagic = [4]byte{0x3c, 0x30, 0xa4, 0x37}

// Load a dictionary as described in
// https://github.com/facebook/zstd/blob/master/doc/zstd_compression_format.md#dictionary-format
func loadDict(b []byte) (*dict, error) {
	// Check static field size.
	if len(b) <= 8+(3*4) {
		return nil, io.ErrUnexpectedEOF
	}
	d := dict{}
	if !bytes.Equal(b[:4], dictMagic[:]) {
		return nil, ErrMagicMismatch
	}
	d.id = binary.LittleEndian.Uint32(b[4:8])
	if d.id == 0 {
		return nil, errors.New("dictionaries cannot have ID 0")
	}
	br := byteReader{
		b:   b[8:],
		off: 0,
	}
	if err := d.llDec.fse.readNCount(&br, maxLiteralLengthSymbol); err != nil {
		return nil, err
	}
	if err := d.ofDec.fse.readNCount(&br, maxOffsetLengthSymbol); err != nil {
		return nil, err
	}
	if err := d.mlDec.fse.readNCount(&br, maxMatchLengthSymbol); err != nil {
		return nil, err
	}
	// Set decoders as predefined so they aren't reused.
	d.llDec.fse.preDefined = true
	d.ofDec.fse.preDefined = true
	d.mlDec.fse.preDefined = true

	if br.remain() < 12 {
		return nil, io.ErrUnexpectedEOF
	}
	d.offsets[0] = int(br.Uint32())
	d.offsets[1] = int(br.Uint32())
	d.offsets[2] = int(br.Uint32())
	if d.offsets[0] <= 0 || d.offsets[1] <= 0 || d.offsets[2] <= 0 {
		return nil, errors.New("invalid offset in dictionary")
	}
	d.content = make([]byte, br.remain())
	copy(d.content, br.unread())
	if d.offsets[0] > len(d.content) || d.offsets[1] > len(d.content) || d.offsets[2] > len(d.content) {
		return nil, errors.New("initial offset bigger than dictionary content")
	}

	return &d, nil
}
