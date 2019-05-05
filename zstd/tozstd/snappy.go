package tozstd

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"math"

	"github.com/klauspost/compress/huff0"
)

const (
	tagLiteral = 0x00
	tagCopy1   = 0x01
	tagCopy2   = 0x02
	tagCopy4   = 0x03
)

type blockType uint8

const (
	blockTypeRaw blockType = iota
	blockTypeRLE
	blockTypeCompressed
	blockTypeReserved
)

const (
	checksumSize    = 4
	chunkHeaderSize = 4
	magicChunk      = "\xff\x06\x00\x00" + magicBody
	magicBody       = "sNaPpY"

	// maxBlockSize is the maximum size of the input to encodeBlock. It is not
	// part of the wire format per se, but some parts of the encoder assume
	// that an offset fits into a uint16.
	//
	// Also, for the framing format (Writer type instead of Encode function),
	// https://github.com/google/snappy/blob/master/framing_format.txt says
	// that "the uncompressed data in a chunk must be no longer than 65536
	// bytes".
	maxBlockSize = 65536

	// maxEncodedLenOfMaxBlockSize equals MaxEncodedLen(maxBlockSize), but is
	// hard coded to be a const instead of a variable, so that obufLen can also
	// be a const. Their equivalence is confirmed by
	// TestMaxEncodedLenOfMaxBlockSize.
	maxEncodedLenOfMaxBlockSize = 76490

	obufHeaderLen = len(magicChunk) + checksumSize + chunkHeaderSize
	obufLen       = obufHeaderLen + maxEncodedLenOfMaxBlockSize
)

const (
	chunkTypeCompressedData   = 0x00
	chunkTypeUncompressedData = 0x01
	chunkTypePadding          = 0xfe
	chunkTypeStreamIdentifier = 0xff
)

var (
	// ErrCorrupt reports that the input is invalid.
	ErrCorrupt = errors.New("snappy: corrupt input")
	// ErrTooLarge reports that the uncompressed length is too large.
	ErrTooLarge = errors.New("snappy: decoded block is too large")
	// ErrUnsupported reports that the input isn't supported.
	ErrUnsupported = errors.New("snappy: unsupported input")

	errUnsupportedLiteralLength = errors.New("snappy: unsupported literal length")
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

// crc implements the checksum specified in section 3 of
// https://github.com/google/snappy/blob/master/framing_format.txt
func crc(b []byte) uint32 {
	c := crc32.Update(0, crcTable, b)
	return uint32(c>>15|c<<17) + 0xa282ead8
}

// DecodedLen returns the length of the decoded block.
func DecodedLen(src []byte) (int, error) {
	v, _, err := decodedLen(src)
	return v, err
}

// decodedLen returns the length of the decoded block and the number of bytes
// that the length header occupied.
func decodedLen(src []byte) (blockLen, headerLen int, err error) {
	v, n := binary.Uvarint(src)
	if n <= 0 || v > 0xffffffff {
		return 0, 0, ErrCorrupt
	}

	const wordSize = 32 << (^uint(0) >> 32 & 1)
	if wordSize == 32 && v > 0x7fffffff {
		return 0, 0, ErrTooLarge
	}
	return int(v), n, nil
}

const (
	decodeErrCodeCorrupt                  = 1
	decodeErrCodeUnsupportedLiteralLength = 2
)

/*
// Decode returns the decoded form of src. The returned slice may be a sub-
// slice of dst if dst was large enough to hold the entire decoded block.
// Otherwise, a newly allocated slice will be returned.
//
// The dst and src must not overlap. It is valid to pass a nil dst.
func Decode(dst, src []byte) ([]byte, error) {
	dLen, s, err := decodedLen(src)
	if err != nil {
		return nil, err
	}
	if dLen <= len(dst) {
		dst = dst[:dLen]
	} else {
		dst = make([]byte, dLen)
	}
	switch decode(dst, src[s:]) {
	case 0:
		return dst, nil
	case decodeErrCodeUnsupportedLiteralLength:
		return nil, errUnsupportedLiteralLength
	}
	return nil, ErrCorrupt
}
*/

// Snappy can read Snappy-compressed streams and convert them to zstd.
type Snappy struct {
	r   io.Reader
	err error
	//decoded []byte
	buf []byte
	// decoded[i:j] contains decoded bytes that have not yet been passed on.
	//i, j       int
	//readHeader bool

	//compress chan *block
	//write    chan *block
	block *block
}

type seq struct {
	litLen   uint32
	offset   uint32
	matchLen uint32
}

type seqCodes struct {
	litLen   []uint8
	offset   []uint8
	matchLen []uint8

	llEnc, ofEnc, mlEnc    *fseEncoder
	llPrev, ofPrev, mlPrev *fseEncoder

	// maximum values
	//llMax uint8
	//ofMax uint8
	//mlMax uint8
}

func (s *seqCodes) initSize(sequences int) {
	// maybe move stuff here
}

type block struct {
	size      int
	literals  []byte
	sequences []seq
	//done      chan struct{}
	seqCodes seqCodes
	litEnc   huff0.Scratch

	extraLits int
	last      bool

	output []byte
}

func (b *block) init() {
	if cap(b.literals) < maxBlockSize {
		b.literals = make([]byte, 0, maxBlockSize)
	}
	const defSeqs = 200
	b.literals = b.literals[:0]
	if cap(b.sequences) < defSeqs {
		b.sequences = make([]seq, 0, defSeqs)
	}
	if cap(b.output) < maxBlockSize+16 {
		b.output = make([]byte, 0, maxBlockSize+16)
	}
	fn := func(b []uint8) []uint8 {
		if cap(b) < defSeqs {
			return make([]uint8, 0, defSeqs)
		}
		return b
	}
	b.seqCodes.matchLen = fn(b.seqCodes.matchLen)
	b.seqCodes.offset = fn(b.seqCodes.offset)
	b.seqCodes.litLen = fn(b.seqCodes.litLen)
	if b.seqCodes.mlEnc == nil {
		b.seqCodes.mlEnc = &fseEncoder{}
		b.seqCodes.ofEnc = &fseEncoder{}
		b.seqCodes.llEnc = &fseEncoder{}
	}

	b.reset()
}

func (b *block) reset() {
	b.extraLits = 0
	b.literals = b.literals[:0]
	b.sequences = b.sequences[:0]
	b.seqCodes.matchLen = b.seqCodes.matchLen[:0]
	b.seqCodes.offset = b.seqCodes.offset[:0]
	b.seqCodes.litLen = b.seqCodes.litLen[:0]
	b.output = b.output[:0]
	b.extraLits = 0
}

type blockHeader uint32

func (h *blockHeader) setLast(b bool) {
	if b {
		*h = *h | 1
	} else {
		const mask = (1 << 24) - 2
		*h = *h & mask
	}
}
func (h *blockHeader) setSize(v uint32) {
	const mask = (1 << 24) - (1 << 3)
	*h = (*h)&mask | blockHeader(v<<3)
}

func (h *blockHeader) setType(t blockType) {
	const mask = (1 << 24) - (1 << 3) + 1
	*h = (*h & mask) | blockHeader(t<<1)
}

func (h blockHeader) appendTo(b []byte) []byte {
	return append(b, uint8(h), uint8(h>>8), uint8(h>>16))
}

type literalsHeader uint32

type literalsBlockType uint8

const (
	literalsBlockRaw literalsBlockType = iota
	literalsBlockRLE
	literalsBlockCompressed
	literalsBlockTreeless
)

func (h *literalsHeader) setType(t literalsBlockType) {
	const mask = math.MaxUint64 - 3
	*h = (*h & mask) | literalsHeader(t<<1)
}

func (h *literalsHeader) setSize(regenLen int) {
	inBits := highBit(uint32(regenLen))
	// Only retain 2 bits
	const mask = 3
	lh := uint64(*h & mask)
	switch {
	case inBits <= 5:
		lh |= (uint64(regenLen) << 3) | (1 << 60)
	case inBits <= 12:
		lh |= (1 << 2) | (uint64(regenLen) << 4) | (2 << 60)
	case inBits <= 20:
		lh |= (3 << 2) | (uint64(regenLen) << 4) | (3 << 60)
	default:
		panic("internal error: block too big")
	}
}

func (h *literalsHeader) setSizes(compLen, inLen int) {
	compBits, inBits := highBit(uint32(compLen)), highBit(uint32(inLen))
	// Only retain 2 bits
	const mask = 3
	lh := uint64(*h & mask)
	switch {
	case compBits <= 10 && inBits <= 10:
		lh |= (1 << 2) | (uint64(inLen) << 4) | (uint64(compLen) << (10 + 4)) | (3 << 60)
	case compBits <= 14 && inBits <= 14:
		lh |= (2 << 2) | (uint64(inLen) << 4) | (uint64(compLen) << (14 + 4)) | (4 << 60)
	case compBits <= 18 && inBits <= 18:
		lh |= (3 << 2) | (uint64(inLen) << 4) | (uint64(compLen) << (18 + 4)) | (5 << 60)
	default:
		panic("internal error: block too big")
	}
}

func (h literalsHeader) appendTo(b []byte) []byte {
	size := uint8(h >> 60)
	switch size {
	case 1:
		b = append(b, uint8(h))
	case 2:
		b = append(b, uint8(h), uint8(h>>8))
	case 3:
		b = append(b, uint8(h), uint8(h>>8), uint8(h>>16))
	case 4:
		b = append(b, uint8(h), uint8(h>>8), uint8(h>>16), uint8(h>>24))
	case 5:
		b = append(b, uint8(h), uint8(h>>8), uint8(h>>16), uint8(h>>24), uint8(h>>32))
	default:
		panic("internal error: literalsHeader has invalid size")
	}
	return b
}

// encodeLits can be used if the block is only literals.
func (b *block) encodeLits() error {
	var bh blockHeader
	bh.setLast(b.last)
	bh.setSize(uint32(len(b.literals)))

	// Don't compress extremely small blocks
	if len(b.literals) < 32 {
		bh.setType(blockTypeRaw)
		b.output = bh.appendTo(b.output)
		b.output = append(b.output, b.literals...)
		return nil
	}

	// TODO: Switch to 1X when less than 32 bytes.
	out, reUsed, err := huff0.Compress4X(b.literals, &b.litEnc)
	switch err {
	case huff0.ErrIncompressible:
		bh.setType(blockTypeRaw)
		b.output = bh.appendTo(b.output)
		b.output = append(b.output, b.literals...)
		return nil
	case huff0.ErrUseRLE:
		bh.setType(blockTypeRLE)
		b.output = bh.appendTo(b.output)
		b.output = append(b.output, b.literals[0])
		return nil
	default:
		return err
	case nil:
	}
	// Compressed...
	// Now, allow reuse
	b.litEnc.Reuse = huff0.ReusePolicyAllow
	bh.setType(blockTypeCompressed)
	bh.setSize(uint32(len(out)))
	b.output = bh.appendTo(b.output)

	var lh uint64
	if reUsed {
		lh |= uint64(literalsBlockTreeless)
	} else {
		lh |= uint64(literalsBlockCompressed)
	}
	compLen, inLen := uint64(len(out)), uint64(len(b.literals))
	compBits, inBits := highBit(uint32(compLen)), highBit(uint32(inLen))
	switch {
	case compBits <= 10 && inBits <= 10:
		lh |= (1 << 2) | (inLen << 4) | (compLen << (10 + 4))
		b.output = append(b.output, uint8(lh), uint8(lh>>8), uint8(lh>>16))
	case compBits <= 14 && inBits <= 14:
		lh |= (2 << 2) | (inLen << 4) | (compLen << (14 + 4))
		b.output = append(b.output, uint8(lh), uint8(lh>>8), uint8(lh>>16), uint8(lh>>24))
	case compBits <= 18 && inBits <= 18:
		lh |= (3 << 2) | (inLen << 4) | (compLen << (18 + 4))
		b.output = append(b.output, uint8(lh), uint8(lh>>8), uint8(lh>>16), uint8(lh>>24), uint8(lh>>32))
	default:
		panic("internal error: block too big")
	}
	// Add compressed data.
	b.output = append(b.output, out...)
	// No sequences.
	b.output = append(b.output, 0)
	return nil
}

type seqCompMode uint8

const (
	compModePredefined seqCompMode = iota
	compModeRLE
	compModeFSE
	compModeRepeat
)

// encodeLits can be used if the block is only literals.
func (b *block) encode() error {
	if len(b.sequences) == 0 {
		return b.encodeLits()
	}

	var bh blockHeader
	var lh literalsHeader
	bh.setLast(b.last)
	bh.setType(blockTypeCompressed)

	// TODO: We must set size when we are finished.
	b.output = bh.appendTo(b.output)

	// TODO: Switch to 1X when less than 32 bytes.
	out, reUsed, err := huff0.Compress4X(b.literals, &b.litEnc)
	switch err {
	case huff0.ErrIncompressible:
		lh.setType(literalsBlockRaw)
		lh.setSize(len(b.literals))
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, b.literals...)

	case huff0.ErrUseRLE:
		lh.setType(literalsBlockRLE)
		lh.setSize(len(b.literals))
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, b.literals[0])
	default:
		return err
	case nil:
		if reUsed {
			lh.setType(literalsBlockTreeless)
		} else {
			lh.setType(literalsBlockCompressed)
		}
		lh.setSizes(len(out), len(b.literals))
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, b.literals[0])
		b.litEnc.Reuse = huff0.ReusePolicyAllow
	}
	// Sequence compression

	// Write the number of sequences
	switch {
	case len(b.sequences) < 128:
		b.output = append(b.output, uint8(len(b.sequences)))
	case len(b.sequences) < 0x7f00: // TODO: this could be wrong
		n := len(b.sequences)
		b.output = append(b.output, 128+uint8(n>>8), uint8(n))
	default:
		n := len(b.sequences) - 0x7f00
		b.output = append(b.output, 255, uint8(n), uint8(n>>8))
	}

	b.genCodes()
	llEnc := b.seqCodes.llEnc
	ofEnc := b.seqCodes.ofEnc
	mlEnc := b.seqCodes.mlEnc
	err = llEnc.normalizeCount(b.seqCodes.litLen)
	if err != nil {
		return err
	}
	err = ofEnc.normalizeCount(b.seqCodes.offset)
	if err != nil {
		return err
	}
	err = mlEnc.normalizeCount(b.seqCodes.matchLen)
	if err != nil {
		return err
	}

	// Write compression mode
	var mode uint8
	if llEnc.useRLE {
		mode |= uint8(compModeRLE) << 6
	} else {
		mode |= uint8(compModeFSE) << 6
	}
	if ofEnc.useRLE {
		mode |= uint8(compModeRLE) << 4
	} else {
		mode |= uint8(compModeFSE) << 4
	}
	if mlEnc.useRLE {
		mode |= uint8(compModeRLE) << 2
	} else {
		mode |= uint8(compModeFSE) << 2
	}

	b.output, err = llEnc.writeCount(b.output)
	if err != nil {
		return err
	}
	b.output, err = ofEnc.writeCount(b.output)
	if err != nil {
		return err
	}
	b.output, err = mlEnc.writeCount(b.output)
	if err != nil {
		return err
	}

	// Size is output minus block header.
	bh.setSize(uint32(len(b.output)) - 3)
	_ = bh.appendTo(b.output[:0])
	return nil
}

func (b *block) genCodes() {
	if len(b.sequences) == 0 {
		// nothing to do
		return
	}

	if len(b.sequences) > math.MaxUint16 {
		panic("can only encode up to 64K sequences")
	}
	if cap(b.seqCodes.litLen) < len(b.sequences) {
		b.seqCodes.litLen = make([]byte, len(b.sequences)*2)
	}
	if cap(b.seqCodes.offset) < len(b.sequences) {
		b.seqCodes.offset = make([]byte, len(b.sequences)*2)
	}
	if cap(b.seqCodes.matchLen) < len(b.sequences) {
		b.seqCodes.matchLen = make([]byte, len(b.sequences)*2)
	}
	ll := b.seqCodes.litLen[:len(b.sequences)]
	of := b.seqCodes.offset[:len(b.sequences)]
	ml := b.seqCodes.matchLen[:len(b.sequences)]

	// No bounds checks after here:
	llH := b.seqCodes.llEnc.Histogram()[:256]
	ofH := b.seqCodes.ofEnc.Histogram()[:256]
	mlH := b.seqCodes.mlEnc.Histogram()[:256]
	for i := range llH {
		llH[i] = 0
	}
	for i := range ofH {
		ofH[i] = 0
	}
	for i := range mlH {
		mlH[i] = 0
	}

	var llMax, ofMax, mlMax uint8
	for i, seq := range b.sequences {
		v := llCode(seq.litLen)
		ll[i] = v
		llH[v]++
		if v > llMax {
			llMax = v
		}

		v = ofCode(seq.offset)
		of[i] = v
		ofH[v]++
		if v > ofMax {
			ofMax = v
		}

		v = mlCode(seq.matchLen)
		ml[i] = v
		mlH[i]++
		if v > mlMax {
			mlMax = v
		}
	}
	maxCount := func(a []uint32) int {
		var max uint32
		for _, v := range a {
			if v > max {
				max = v
			}
		}
		return int(max)
	}

	b.seqCodes.litLen = ll
	b.seqCodes.offset = of
	b.seqCodes.matchLen = ml
	b.seqCodes.ofEnc.HistogramFinished(ofMax, maxCount(mlH[:ofMax]))
	b.seqCodes.llEnc.HistogramFinished(llMax, maxCount(llH[:llMax]))
	b.seqCodes.mlEnc.HistogramFinished(mlMax, maxCount(mlH[:mlMax]))
}

func (r *Snappy) readFull(p []byte, allowEOF bool) (ok bool) {
	if _, r.err = io.ReadFull(r.r, p); r.err != nil {
		if r.err == io.ErrUnexpectedEOF || (r.err == io.EOF && !allowEOF) {
			r.err = ErrCorrupt
		}
		return false
	}
	return true
}

func (r *Snappy) Convert(in io.Reader, w io.Writer) (int64, error) {
	r.err = nil
	r.r = in
	if r.block == nil {
		r.block = &block{}
		r.block.init()
	}
	if cap(r.buf) != maxEncodedLenOfMaxBlockSize+checksumSize {
		r.buf = make([]byte, maxEncodedLenOfMaxBlockSize+checksumSize)
	}
	r.block.litEnc.Reuse = huff0.ReusePolicyNone
	var written int64
	var readHeader bool

	for {
		if !r.readFull(r.buf[:4], true) {
			return written, r.err
		}
		chunkType := r.buf[0]
		if !readHeader {
			if chunkType != chunkTypeStreamIdentifier {
				r.err = ErrCorrupt
				return written, r.err
			}
			readHeader = true
		}
		chunkLen := int(r.buf[1]) | int(r.buf[2])<<8 | int(r.buf[3])<<16
		if chunkLen > len(r.buf) {
			r.err = ErrUnsupported
			return written, r.err
		}

		// The chunk types are specified at
		// https://github.com/google/snappy/blob/master/framing_format.txt
		switch chunkType {
		case chunkTypeCompressedData:
			// Section 4.2. Compressed data (chunk type 0x00).
			if chunkLen < checksumSize {
				r.err = ErrCorrupt
				return written, r.err
			}
			buf := r.buf[:chunkLen]
			if !r.readFull(buf, false) {
				return written, r.err
			}
			//checksum := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
			buf = buf[checksumSize:]

			n, err := DecodedLen(buf)
			if err != nil {
				r.err = err
				return written, r.err
			}
			if n > maxBlockSize {
				r.err = ErrCorrupt
				return written, r.err
			}
			r.block.reset()
			if err := decode(r.block, buf); err != nil {
				r.err = err
				return written, r.err
			}
			err = r.block.encode()
			if err != nil {
				return written, err
			}
			n, r.err = w.Write(r.block.output)
			if r.err != nil {
				return written, err
			}
			written += int64(n)

			/*
				if crc(r.decoded[:n]) != checksum {
					r.err = ErrCorrupt
					return 0, r.err
				}
				r.i, r.j = 0, n
			*/
			continue

		case chunkTypeUncompressedData:
			// Section 4.3. Uncompressed data (chunk type 0x01).
			if chunkLen < checksumSize {
				r.err = ErrCorrupt
				return written, r.err
			}
			r.block.reset()
			buf := r.buf[:checksumSize]
			if !r.readFull(buf, false) {
				return written, r.err
			}
			checksum := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
			// Read directly into r.decoded instead of via r.buf.
			n := chunkLen - checksumSize
			if n > maxBlockSize {
				r.err = ErrCorrupt
				return written, r.err
			}
			r.block.literals = r.block.literals[:n]
			if !r.readFull(r.block.literals, false) {
				return written, r.err
			}
			if crc(r.block.literals) != checksum {
				r.err = ErrCorrupt
				return written, r.err
			}
			err := r.block.encodeLits()
			if err != nil {
				return written, err
			}
			n, r.err = w.Write(r.block.output)
			if r.err != nil {
				return written, err
			}
			written += int64(n)
			continue

		case chunkTypeStreamIdentifier:
			// Section 4.1. Stream identifier (chunk type 0xff).
			if chunkLen != len(magicBody) {
				r.err = ErrCorrupt
				return written, r.err
			}
			if !r.readFull(r.buf[:len(magicBody)], false) {
				return written, r.err
			}
			for i := 0; i < len(magicBody); i++ {
				if r.buf[i] != magicBody[i] {
					r.err = ErrCorrupt
					return written, r.err
				}
			}
			continue
		}

		if chunkType <= 0x7f {
			// Section 4.5. Reserved unskippable chunks (chunk types 0x02-0x7f).
			r.err = ErrUnsupported
			return written, r.err
		}
		// Section 4.4 Padding (chunk type 0xfe).
		// Section 4.6. Reserved skippable chunks (chunk types 0x80-0xfd).
		if !r.readFull(r.buf[:chunkLen], false) {
			return written, r.err
		}
	}
}

// decode writes the decoding of src to dst. It assumes that the varint-encoded
// length of the decompressed bytes has already been read, and that len(dst)
// equals that length.
//
// It returns 0 on success or a decodeErrCodeXxx error code on failure.
func decode(dst *block, src []byte) error {
	var d, s, length int
	lits := dst.extraLits
	var offset uint32
	for s < len(src) {
		switch src[s] & 0x03 {
		case tagLiteral:
			x := uint32(src[s] >> 2)
			switch {
			case x < 60:
				s++
			case x == 60:
				s += 2
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return ErrCorrupt
				}
				x = uint32(src[s-1])
			case x == 61:
				s += 3
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return ErrCorrupt
				}
				x = uint32(src[s-2]) | uint32(src[s-1])<<8
			case x == 62:
				s += 4
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return ErrCorrupt
				}
				x = uint32(src[s-3]) | uint32(src[s-2])<<8 | uint32(src[s-1])<<16
			case x == 63:
				s += 5
				if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
					return ErrCorrupt
				}
				x = uint32(src[s-4]) | uint32(src[s-3])<<8 | uint32(src[s-2])<<16 | uint32(src[s-1])<<24
			}
			if x > maxBlockSize {
				return ErrCorrupt
			}
			length = int(x) + 1
			if length <= 0 {
				return errUnsupportedLiteralLength
			}
			//if length > maxBlockSize-d || uint32(length) > len(src)-s {
			//	return ErrCorrupt
			//}

			dst.literals = append(dst.literals, src[s:s+length]...)
			lits += length
			s += length
			continue

		case tagCopy1:
			s += 2
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return ErrCorrupt
			}
			length = 4 + int(src[s-2])>>2&0x7
			offset = uint32(src[s-2])&0xe0<<3 | uint32(src[s-1])

		case tagCopy2:
			s += 3
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return ErrCorrupt
			}
			length = 1 + int(src[s-3])>>2
			offset = uint32(src[s-2]) | uint32(src[s-1])<<8

		case tagCopy4:
			s += 5
			if uint(s) > uint(len(src)) { // The uint conversions catch overflow from the previous line.
				return ErrCorrupt
			}
			length = 1 + int(src[s-5])>>2
			offset = uint32(src[s-4]) | uint32(src[s-3])<<8 | uint32(src[s-2])<<16 | uint32(src[s-1])<<24
		}

		if offset <= 0 || d < int(offset) /*|| length > len(dst)-d */ {
			return ErrCorrupt
		}
		// Copy from an earlier sub-slice of dst to a later sub-slice. Unlike
		// the built-in copy function, this byte-by-byte copy always runs
		// forwards, even if the slices overlap. Conceptually, this is:
		//
		// d += forwardCopy(dst[d:d+length], dst[d-offset:])
		//for end := d + length; d != end; d++ {
		//	dst[d] = dst[d-offset]
		//}
		dst.sequences = append(dst.sequences, seq{
			litLen:   uint32(lits),
			offset:   offset,
			matchLen: uint32(length),
		})
		lits = 0
		dst.size += length + lits
	}
	dst.extraLits = lits
	//if d != len(dst) {
	//	return ErrCorrupt
	//}
	return nil
}
