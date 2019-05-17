package tozstd

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/klauspost/compress/huff0"
)

type blockType uint8

const (
	blockTypeRaw blockType = iota
	blockTypeRLE
	blockTypeCompressed
	blockTypeReserved
)

type literalsBlockType uint8

const (
	literalsBlockRaw literalsBlockType = iota
	literalsBlockRLE
	literalsBlockCompressed
	literalsBlockTreeless
)

type block struct {
	size      int
	literals  []byte
	sequences []seq
	seqCodes  seqCodes
	litEnc    huff0.Scratch

	extraLits int
	last      bool

	output        []byte
	recentOffsets [3]uint32
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

func (b *block) initOffsets() {
	b.recentOffsets = [3]uint32{1, 4, 8}
}

func (b *block) reset() {
	b.extraLits = 0
	b.literals = b.literals[:0]
	b.size = 0
	b.sequences = b.sequences[:0]
	b.seqCodes.matchLen = b.seqCodes.matchLen[:0]
	b.seqCodes.offset = b.seqCodes.offset[:0]
	b.seqCodes.litLen = b.seqCodes.litLen[:0]
	b.output = b.output[:0]
	b.litEnc.Reuse = huff0.ReusePolicyNone
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
	const mask = 7
	*h = (*h)&mask | blockHeader(v<<3)
}

func (h *blockHeader) setType(t blockType) {
	const mask = 1 | (((1 << 24) - 1) ^ 7)
	*h = (*h & mask) | blockHeader(t<<1)
}

func (h blockHeader) appendTo(b []byte) []byte {
	return append(b, uint8(h), uint8(h>>8), uint8(h>>16))
}

func (h blockHeader) String() string {
	return fmt.Sprintf("Type: %d, Size: %d, Last:%t", (h>>1)&3, h>>3, h&1 == 1)
}

type literalsHeader uint64

func (h *literalsHeader) setType(t literalsBlockType) {
	const mask = math.MaxUint64 - 3
	*h = (*h & mask) | literalsHeader(t)
}

func (h *literalsHeader) setSize(regenLen int) {
	inBits := highBit(uint32(regenLen))
	// Only retain 2 bits
	const mask = 3
	lh := uint64(*h & mask)
	switch {
	case inBits < 5:
		lh |= (uint64(regenLen) << 3) | (1 << 60)
		if debug {
			got := int(lh>>3) & 0xff
			if got != regenLen {
				panic(fmt.Sprint("litRegenSize = ", regenLen, "(want) != ", got, "(got)"))
			}
		}
	case inBits < 12:
		lh |= (1 << 2) | (uint64(regenLen) << 4) | (2 << 60)
	case inBits < 20:
		lh |= (3 << 2) | (uint64(regenLen) << 4) | (3 << 60)
	default:
		panic("internal error: block too big")
	}
	*h = literalsHeader(lh)
}

func (h *literalsHeader) setSizes(compLen, inLen int) {
	compBits, inBits := bits.Len32(uint32(compLen)), bits.Len32(uint32(inLen))
	// Only retain 2 bits
	const mask = 3
	lh := uint64(*h & mask)
	switch {
	case compBits <= 10 && inBits <= 10:
		lh |= (1 << 2) | (uint64(inLen) << 4) | (uint64(compLen) << (10 + 4)) | (3 << 60)
		if debug {
			const mmask = (1 << 24) - 1
			n := (lh >> 4) & mmask
			if int(n&1023) != inLen {
				panic(fmt.Sprint("regensize:", int(n&1023), "!=", inLen, inBits))
			}
			if int(n>>10) != compLen {
				panic(fmt.Sprint("compsize:", int(n>>10), "!=", compLen, compBits))
			}
		}
	case compBits <= 14 && inBits <= 14:
		lh |= (2 << 2) | (uint64(inLen) << 4) | (uint64(compLen) << (14 + 4)) | (4 << 60)
	case compBits <= 18 && inBits <= 18:
		lh |= (3 << 2) | (uint64(inLen) << 4) | (uint64(compLen) << (18 + 4)) | (5 << 60)
	default:
		panic("internal error: block too big")
	}
	*h = literalsHeader(lh)
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
		panic(fmt.Errorf("internal error: literalsHeader has invalid size (%d)", size))
	}
	return b
}

// size returns the output size with currently set values.
func (h literalsHeader) size() int {
	return int(h >> 60)
}

func (h literalsHeader) String() string {
	return fmt.Sprintf("Type: %d, SizeFormat: %d, Size: 0x%d, Bytes:%d", literalsBlockType(h&3), (h>>2)&3, h&((1<<60)-1)>>4, h>>60)
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
	// Bail out of compression is too little.
	if len(out) > (len(b.literals) - len(b.literals)>>4) {
		err = huff0.ErrIncompressible
	}
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
	var lh literalsHeader
	if reUsed {
		lh.setType(literalsBlockTreeless)
	} else {
		lh.setType(literalsBlockCompressed)
	}
	// Set sizes
	lh.setSizes(len(out), len(b.literals))
	bh.setSize(uint32(len(out) + lh.size() + 1))

	// Write block headers.
	b.output = bh.appendTo(b.output)
	b.output = lh.appendTo(b.output)
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
	b.output = bh.appendTo(b.output)

	var (
		out    []byte
		reUsed bool
		err    error
	)
	if len(b.literals) > 32 {
		// TODO: Switch to 1X on small blocks.
		out, reUsed, err = huff0.Compress4X(b.literals, &b.litEnc)
		if len(out) > len(b.literals)-len(b.literals)>>4 {
			err = huff0.ErrIncompressible
		}
	} else {
		err = huff0.ErrIncompressible
	}
	switch err {
	case huff0.ErrIncompressible:
		lh.setType(literalsBlockRaw)
		lh.setSize(len(b.literals))
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, b.literals...)
		println("Adding literals RAW, length", len(b.literals))
	case huff0.ErrUseRLE:
		lh.setType(literalsBlockRLE)
		lh.setSize(len(b.literals))
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, b.literals[0])
		println("Adding literals RLE")
	default:
		println("Adding literals ERROR:", err)
		return err
	case nil:
		// Compressed literals...
		if reUsed {
			println("reused tree")
			lh.setType(literalsBlockTreeless)
		} else {
			println("new tree, size:", len(b.litEnc.OutTable))
			lh.setType(literalsBlockCompressed)
			if debug {
				_, _, err := huff0.ReadTable(out, nil)
				if err != nil {
					panic(err)
				}
			}
		}
		lh.setSizes(len(out), len(b.literals))
		if debug {
			printf("Compressed %d literals to %d bytes", len(b.literals), len(out))
			println("Adding literal header:", lh)
		}
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, out...)
		b.litEnc.Reuse = huff0.ReusePolicyAllow
		println("Adding literals compressed")
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
	println("Encoding", len(b.sequences), "sequences")

	b.genCodes()
	llEnc := b.seqCodes.llEnc
	ofEnc := b.seqCodes.ofEnc
	mlEnc := b.seqCodes.mlEnc
	llIn, ofIn, mlIn := b.seqCodes.litLen, b.seqCodes.offset, b.seqCodes.matchLen
	err = llEnc.normalizeCount(llIn)
	if err != nil {
		return err
	}
	err = ofEnc.normalizeCount(ofIn)
	if err != nil {
		return err
	}
	err = mlEnc.normalizeCount(mlIn)
	if err != nil {
		return err
	}

	// Write compression mode
	var mode uint8
	if llEnc.useRLE {
		mode |= uint8(compModeRLE) << 6
		llEnc.setRLE(llIn[0])
		println("llEnc.useRLE")
	} else {
		mode |= uint8(compModeFSE) << 6
	}
	if ofEnc.useRLE {
		mode |= uint8(compModeRLE) << 4
		ofEnc.setRLE(ofIn[0])
		println("ofEnc.useRLE")
	} else {
		mode |= uint8(compModeFSE) << 4
	}
	if mlEnc.useRLE {
		mode |= uint8(compModeRLE) << 2
		mlEnc.setRLE(mlIn[0])
		println("mlEnc.useRLE")
		fmt.Println()
	} else {
		mode |= uint8(compModeFSE) << 2
	}
	b.output = append(b.output, mode)
	printf("Compression modes: 0b%b", mode)

	b.output, err = llEnc.writeCount(b.output)
	if err != nil {
		return err
	}
	start := len(b.output)
	b.output, err = ofEnc.writeCount(b.output)
	if err != nil {
		return err
	}
	if false && len(b.sequences) == 64 {
		println("block:", b.output[start:], "tablelog", ofEnc.actualTableLog, "maxcount:", ofEnc.maxCount)
		fmt.Printf("selected TableLog: %d, Symbol length: %d\n", ofEnc.actualTableLog, ofEnc.symbolLen)
		for i, v := range ofEnc.norm[:ofEnc.symbolLen] {
			fmt.Printf("%3d: %5d -> %4d \n", i, ofEnc.count[i], v)
		}
	}
	b.output, err = mlEnc.writeCount(b.output)
	if err != nil {
		return err
	}

	// Maybe in block?
	var wr bitWriter
	wr.reset(b.output)

	var ll, of, ml cState

	// Current sequence
	seq := len(b.sequences) - 1
	llTT, ofTT, mlTT := llEnc.ct.symbolTT[:256], ofEnc.ct.symbolTT[:256], mlEnc.ct.symbolTT[:256]
	llB, ofB, mlB := llIn[seq], ofIn[seq], mlIn[seq]
	ll.init(&wr, &llEnc.ct, llTT[llB])
	of.init(&wr, &ofEnc.ct, ofTT[ofB])
	wr.flush32()
	ml.init(&wr, &mlEnc.ct, mlTT[mlB])

	s := b.sequences[seq]
	wr.addBits32NC(s.litLen, llBitsTable[llB])
	wr.flush32()
	wr.addBits32NC(s.matchLen, mlBitsTable[mlB])
	wr.flush32()
	wr.addBits32NC(s.offset, ofB)
	seq--
	for seq >= 0 {
		s = b.sequences[seq]
		wr.flush32()
		llB, ofB, mlB := llIn[seq], ofIn[seq], mlIn[seq]
		of.encode(ofTT[ofB])
		ml.encode(mlTT[mlB])
		ll.encode(llTT[llB])
		wr.flush32()
		wr.addBits32NC(s.litLen, llBitsTable[llB])
		wr.flush32()
		wr.addBits32NC(s.matchLen, mlBitsTable[mlB])
		wr.flush32()
		wr.addBits32NC(s.offset, ofB)
		if seq < 10 {
			//println("seq", seq, "offset", s.offset, "bits", ofB)
			//println("seq", seq, "matchlen", s.matchLen, "bits", mlBitsTable[mlB])
		}

		seq--
	}
	ml.flush(mlEnc.actualTableLog)
	of.flush(ofEnc.actualTableLog)
	ll.flush(llEnc.actualTableLog)
	err = wr.close()
	if err != nil {
		return err
	}
	b.output = wr.out

	// Size is output minus block header.
	bh.setSize(uint32(len(b.output)) - 3)
	println("Rewriting block header", bh)
	_ = bh.appendTo(b.output[:0])
	return nil
}

const (
	maxLiteralLengthBits = 35
	maxOffsetBits        = 30
	maxMatchLengthBits   = 52
)

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
		mlH[v]++
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
	if mlMax > maxMatchLengthBits {
		panic(fmt.Errorf("mlMax > maxMatchLengthBits (%d)", mlMax))
	}
	if ofMax > maxOffsetBits {
		panic(fmt.Errorf("ofMax > maxOffsetBits (%d)", ofMax))
	}
	if llMax > maxLiteralLengthBits {
		panic(fmt.Errorf("llMax > maxLiteralLengthBits (%d)", llMax))
	}

	b.seqCodes.litLen = ll
	b.seqCodes.offset = of
	b.seqCodes.matchLen = ml
	b.seqCodes.mlEnc.HistogramFinished(mlMax, maxCount(mlH[:mlMax+1]))
	b.seqCodes.ofEnc.HistogramFinished(ofMax, maxCount(ofH[:ofMax+1]))
	b.seqCodes.llEnc.HistogramFinished(llMax, maxCount(llH[:llMax+1]))
}
