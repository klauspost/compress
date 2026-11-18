// Copyright 2019+ Klaus Post. All rights reserved.
// License information can be found in the LICENSE file.
// Based on work by Yann Collet, released under BSD License.

package zstd

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"slices"

	"github.com/klauspost/compress/huff0"
)

type blockEnc struct {
	size       int
	literals   []byte
	sequences  []seq
	coders     seqCoders
	litEnc     *huff0.Scratch
	dictLitEnc *huff0.Scratch
	wr         bitWriter

	extraLits         int
	output            []byte
	recentOffsets     [3]uint32
	prevRecentOffsets [3]uint32

	last   bool
	lowMem bool
	// highRatio spends more time on the entropy stage, and ultra more still.
	// postSplit splits blocks after parsing (see splitParts).
	// The encoder sets them on every block, so they are not swapped.
	highRatio, ultra, postSplit bool

	// startReps are the repeat offsets the encoder assumed at the start of
	// the block, 0 for unknown. With several parts, the decoder may not get
	// all of them, so some repeat codes are written as offsets.
	startReps [3]uint32
	split     *blockSplit
	// testCuts, if not nil, are the sequences that tests cut a split block
	// before, in place of the estimates.
	testCuts []int
}

// blockPart is a part of a block that is written as a block of its own.
// The literals not used by its sequences come last.
type blockPart struct {
	lits, seqs, size int // literals, sequences and source bytes
}

// init should be used once the block has been created.
// If called more than once, the effect is the same as calling reset.
func (b *blockEnc) init() {
	if b.lowMem {
		// 1K literals
		if cap(b.literals) < 1<<10 {
			b.literals = make([]byte, 0, 1<<10)
		}
		const defSeqs = 20
		if cap(b.sequences) < defSeqs {
			b.sequences = make([]seq, 0, defSeqs)
		}
		// 1K
		if cap(b.output) < 1<<10 {
			b.output = make([]byte, 0, 1<<10)
		}
	} else {
		if cap(b.literals) < maxCompressedBlockSize {
			b.literals = make([]byte, 0, maxCompressedBlockSize)
		}
		const defSeqs = 2000
		if cap(b.sequences) < defSeqs {
			b.sequences = make([]seq, 0, defSeqs)
		}
		if cap(b.output) < maxCompressedBlockSize {
			b.output = make([]byte, 0, maxCompressedBlockSize)
		}
	}

	if b.coders.mlEnc == nil {
		b.coders.mlEnc = &fseEncoder{}
		b.coders.mlPrev = &fseEncoder{}
		b.coders.ofEnc = &fseEncoder{}
		b.coders.ofPrev = &fseEncoder{}
		b.coders.llEnc = &fseEncoder{}
		b.coders.llPrev = &fseEncoder{}
	}
	b.litEnc = &huff0.Scratch{WantLogLess: 4}
	b.reset(nil)
}

// initNewEncode can be used to reset offsets and encoders to the initial state.
func (b *blockEnc) initNewEncode() {
	b.recentOffsets = [3]uint32{1, 4, 8}
	b.litEnc.Reuse = huff0.ReusePolicyNone
	b.coders.setPrev(nil, nil, nil)
	b.dictLitEnc = nil
}

// reset will reset the block for a new encode, but in the same stream,
// meaning that state will be carried over, but the block content is reset.
// If a previous block is provided, the recent offsets are carried over.
func (b *blockEnc) reset(prev *blockEnc) {
	b.extraLits = 0
	b.literals = b.literals[:0]
	b.size = 0
	b.sequences = b.sequences[:0]
	b.output = b.output[:0]
	b.last = false
	if prev != nil {
		b.recentOffsets = prev.prevRecentOffsets
	}
	b.dictLitEnc = nil
}

// reset will reset the block for a new encode, but in the same stream,
// meaning that state will be carried over, but the block content is reset.
// If a previous block is provided, the recent offsets are carried over.
func (b *blockEnc) swapEncoders(prev *blockEnc) {
	b.coders.swap(&prev.coders)
	b.litEnc, prev.litEnc = prev.litEnc, b.litEnc
}

// blockHeader contains the information for a block header.
type blockHeader uint32

// setLast sets the 'last' indicator on a block.
func (h *blockHeader) setLast(b bool) {
	if b {
		*h = *h | 1
	} else {
		const mask = (1 << 24) - 2
		*h = *h & mask
	}
}

// setSize will store the compressed size of a block.
func (h *blockHeader) setSize(v uint32) {
	const mask = 7
	*h = (*h)&mask | blockHeader(v<<3)
}

// setType sets the block type.
func (h *blockHeader) setType(t blockType) {
	const mask = 1 | (((1 << 24) - 1) ^ 7)
	*h = (*h & mask) | blockHeader(t<<1)
}

// appendTo will append the block header to a slice.
func (h blockHeader) appendTo(b []byte) []byte {
	return append(b, uint8(h), uint8(h>>8), uint8(h>>16))
}

// String returns a string representation of the block.
func (h blockHeader) String() string {
	return fmt.Sprintf("Type: %d, Size: %d, Last:%t", (h>>1)&3, h>>3, h&1 == 1)
}

// literalsHeader contains literals header information.
type literalsHeader uint64

// setType can be used to set the type of literal block.
func (h *literalsHeader) setType(t literalsBlockType) {
	const mask = math.MaxUint64 - 3
	*h = (*h & mask) | literalsHeader(t)
}

// setSize can be used to set a single size, for uncompressed and RLE content.
func (h *literalsHeader) setSize(regenLen int) {
	inBits := bits.Len32(uint32(regenLen))
	// Only retain 2 bits
	const mask = 3
	lh := uint64(*h & mask)
	switch {
	case inBits < 5:
		lh |= (uint64(regenLen) << 3) | (1 << 60)
		if debugEncoder {
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
		panic(fmt.Errorf("internal error: block too big (%d)", regenLen))
	}
	*h = literalsHeader(lh)
}

// setSizes will set the size of a compressed literals section and the input length.
func (h *literalsHeader) setSizes(compLen, inLen int, single bool) {
	compBits, inBits := bits.Len32(uint32(compLen)), bits.Len32(uint32(inLen))
	// Only retain 2 bits
	const mask = 3
	lh := uint64(*h & mask)
	switch {
	case compBits <= 10 && inBits <= 10:
		if !single {
			lh |= 1 << 2
		}
		lh |= (uint64(inLen) << 4) | (uint64(compLen) << (10 + 4)) | (3 << 60)
		if debugEncoder {
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
		if single {
			panic("single stream used with more than 10 bits length.")
		}
	case compBits <= 18 && inBits <= 18:
		lh |= (3 << 2) | (uint64(inLen) << 4) | (uint64(compLen) << (18 + 4)) | (5 << 60)
		if single {
			panic("single stream used with more than 10 bits length.")
		}
	default:
		panic("internal error: block too big")
	}
	*h = literalsHeader(lh)
}

// appendTo will append the literals header to a byte slice.
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

// pushOffsets will push the recent offsets to the backup store.
func (b *blockEnc) pushOffsets() {
	b.prevRecentOffsets = b.recentOffsets
}

// pushOffsets will push the recent offsets to the backup store.
func (b *blockEnc) popOffsets() {
	b.recentOffsets = b.prevRecentOffsets
}

// matchOffset will adjust recent offsets and return the adjusted one,
// if it matches a previous offset.
func (b *blockEnc) matchOffset(offset, lits uint32) uint32 {
	// Check if offset is one of the recent offsets.
	// Adjusts the output offset accordingly.
	// Gives a tiny bit of compression, typically around 1%.
	if true {
		if lits > 0 {
			switch offset {
			case b.recentOffsets[0]:
				offset = 1
			case b.recentOffsets[1]:
				b.recentOffsets[1] = b.recentOffsets[0]
				b.recentOffsets[0] = offset
				offset = 2
			case b.recentOffsets[2]:
				b.recentOffsets[2] = b.recentOffsets[1]
				b.recentOffsets[1] = b.recentOffsets[0]
				b.recentOffsets[0] = offset
				offset = 3
			default:
				b.recentOffsets[2] = b.recentOffsets[1]
				b.recentOffsets[1] = b.recentOffsets[0]
				b.recentOffsets[0] = offset
				offset += 3
			}
		} else {
			switch offset {
			case b.recentOffsets[1]:
				b.recentOffsets[1] = b.recentOffsets[0]
				b.recentOffsets[0] = offset
				offset = 1
			case b.recentOffsets[2]:
				b.recentOffsets[2] = b.recentOffsets[1]
				b.recentOffsets[1] = b.recentOffsets[0]
				b.recentOffsets[0] = offset
				offset = 2
			case b.recentOffsets[0] - 1:
				b.recentOffsets[2] = b.recentOffsets[1]
				b.recentOffsets[1] = b.recentOffsets[0]
				b.recentOffsets[0] = offset
				offset = 3
			default:
				b.recentOffsets[2] = b.recentOffsets[1]
				b.recentOffsets[1] = b.recentOffsets[0]
				b.recentOffsets[0] = offset
				offset += 3
			}
		}
	} else {
		offset += 3
	}
	return offset
}

// encodeRaw can be used to set the output to a raw representation of supplied bytes.
func (b *blockEnc) encodeRaw(a []byte) {
	var bh blockHeader
	bh.setLast(b.last)
	bh.setSize(uint32(len(a)))
	bh.setType(blockTypeRaw)
	b.output = bh.appendTo(b.output[:0])
	b.output = append(b.output, a...)
	if debugEncoder {
		println("Adding RAW block, length", len(a), "last:", b.last)
	}
}

// encodeRaw can be used to set the output to a raw representation of supplied bytes.
func (b *blockEnc) encodeRawTo(dst, src []byte) []byte {
	var bh blockHeader
	bh.setLast(b.last)
	bh.setSize(uint32(len(src)))
	bh.setType(blockTypeRaw)
	dst = bh.appendTo(dst)
	dst = append(dst, src...)
	if debugEncoder {
		println("Adding RAW block, length", len(src), "last:", b.last)
	}
	return dst
}

// encodeLits can be used if the block is only litLen.
func (b *blockEnc) encodeLits(lits []byte, raw bool) error {
	var bh blockHeader
	bh.setLast(b.last)
	bh.setSize(uint32(len(lits)))
	b.setLitOptions()

	// Don't compress extremely small blocks
	tiny := len(lits) < 8 || (len(lits) < 32 && b.dictLitEnc == nil)
	if b.highRatio {
		tiny = len(lits) < b.minHuffLits()
	}
	if tiny || raw {
		if debugEncoder {
			println("Adding RAW block, length", len(lits), "last:", b.last)
		}
		bh.setType(blockTypeRaw)
		b.output = bh.appendTo(b.output)
		b.output = append(b.output, lits...)
		return nil
	}

	var (
		out            []byte
		reUsed, single bool
		err            error
	)
	if b.dictLitEnc != nil {
		b.litEnc.TransferCTable(b.dictLitEnc)
		b.litEnc.Reuse = huff0.ReusePolicyAllow
		b.dictLitEnc = nil
	}
	if len(lits) >= 1024 {
		// Use 4 Streams.
		out, reUsed, err = huff0.Compress4X(lits, b.litEnc)
	} else if len(lits) >= b.minHuffLits() {
		// Use 1 stream
		single = true
		out, reUsed, err = huff0.Compress1X(lits, b.litEnc)
	} else {
		err = huff0.ErrIncompressible
	}
	if err == nil && len(out)+5 >= len(lits) {
		// If we are close, we may still be worse than raw: the header takes
		// up to 5 bytes, and the block 1 more for the sequence count.
		var lh literalsHeader
		lh.setSizes(len(out), len(lits), single)
		if len(out)+lh.size() >= len(lits) {
			err = huff0.ErrIncompressible
			b.dropUnwrittenTable(reUsed)
		}
	}
	switch err {
	case huff0.ErrIncompressible:
		if debugEncoder {
			println("Adding RAW block, length", len(lits), "last:", b.last)
		}
		bh.setType(blockTypeRaw)
		b.output = bh.appendTo(b.output)
		b.output = append(b.output, lits...)
		return nil
	case huff0.ErrUseRLE:
		if debugEncoder {
			println("Adding RLE block, length", len(lits))
		}
		bh.setType(blockTypeRLE)
		b.output = bh.appendTo(b.output)
		b.output = append(b.output, lits[0])
		return nil
	case nil:
	default:
		return err
	}
	// Compressed...
	// Now, allow reuse
	b.litEnc.Reuse = huff0.ReusePolicyAllow
	bh.setType(blockTypeCompressed)
	var lh literalsHeader
	if reUsed {
		if debugEncoder {
			println("Reused tree, compressed to", len(out))
		}
		lh.setType(literalsBlockTreeless)
	} else {
		if debugEncoder {
			println("New tree, compressed to", len(out), "tree size:", len(b.litEnc.OutTable))
		}
		lh.setType(literalsBlockCompressed)
	}
	// Set sizes
	lh.setSizes(len(out), len(lits), single)
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

// dropUnwrittenTable stops later blocks reusing the literal table after
// compressed literals are written raw: a new table became the previous one,
// but the decoder never gets it. With highRatio, the table the decoder has
// is restored instead.
func (b *blockEnc) dropUnwrittenTable(reUsed bool) {
	switch {
	case reUsed:
	case b.highRatio:
		b.litEnc.RestorePrevTable()
	default:
		b.litEnc.Reuse = huff0.ReusePolicyNone
	}
}

// setLitOptions sets up the literal encoder for the block.
func (b *blockEnc) setLitOptions() {
	if b.highRatio {
		// The exact size checks keep raw literals when they are smaller.
		b.litEnc.WantLogLess = 0
		b.litEnc.OptimalDepth = b.ultra
	}
}

// minHuffLits returns the fewest literals worth Huffman coding.
func (b *blockEnc) minHuffLits() int {
	if b.highRatio {
		return 6
	}
	return 17
}

// encodeRLE will encode an RLE block.
func (b *blockEnc) encodeRLE(val byte, length uint32) {
	var bh blockHeader
	bh.setLast(b.last)
	bh.setSize(length)
	bh.setType(blockTypeRLE)
	b.output = bh.appendTo(b.output)
	b.output = append(b.output, val)
}

// fuzzFseEncoder can be used to fuzz the FSE encoder.
func fuzzFseEncoder(data []byte) int {
	if len(data) > maxSequences || len(data) < 2 {
		return 0
	}
	enc := fseEncoder{}
	hist := enc.Histogram()
	maxSym := uint8(0)
	for i, v := range data {
		v = v & 63
		data[i] = v
		hist[v]++
		if v > maxSym {
			maxSym = v
		}
	}
	if maxSym == 0 {
		// All 0
		return 0
	}
	cnt := int(slices.Max(hist[:maxSym]))
	if cnt == len(data) {
		// RLE
		return 0
	}
	enc.HistogramFinished(maxSym, cnt)
	err := enc.normalizeCount(len(data))
	if err != nil {
		return 0
	}
	_, err = enc.writeCount(nil)
	if err != nil {
		panic(err)
	}
	return 1
}

// encode will encode the block and append the output in b.output.
// Previous offset codes must be pushed if more blocks are expected.
func (b *blockEnc) encode(org []byte, raw, rawAllLits bool) error {
	if !b.postSplit {
		return b.encodeBlock(org, raw, rawAllLits)
	}
	if b.split == nil {
		b.split = newBlockSplit()
	}
	sp := b.split
	parts := b.splitParts(raw)
	if len(parts) == 1 {
		return b.encodeBlock(org, raw, rawAllLits)
	}

	// Write the parts in turn, so each continues the entropy tables of the
	// one before (ZSTD_compressBlock_splitBlock_internal).
	sp.save(b)
	lits, seqs, size, last := b.literals, b.sequences, b.size, b.last
	pushed, parsed := b.prevRecentOffsets, b.recentOffsets
	start := len(b.output)
	// The parse continues cRep over all parts. dRep is what the decoder has.
	cRep, dRep := b.startReps, b.startReps
	var l, s, o int
	for i, q := range parts {
		b.literals, b.sequences, b.size = lits[l:l+q.lits], seqs[s:s+q.seqs], q.size
		b.last = last && i == len(parts)-1
		// encodeBlock pops the offsets if it drops the sequences.
		b.prevRecentOffsets = dRep
		cRep, b.recentOffsets = sp.resolveReps(b.sequences, s, cRep, dRep)
		hdr := len(b.output)
		if err := b.encodeBlock(org[o:o+q.size], raw, rawAllLits); err != nil {
			b.literals, b.sequences = lits, seqs
			return err
		}
		if blockType(b.output[hdr]>>1&3) == blockTypeCompressed {
			dRep = b.recentOffsets
		}
		l, s, o = l+q.lits, s+q.seqs, o+q.size
	}
	b.literals, b.sequences, b.size, b.last = lits, seqs, size, last
	b.prevRecentOffsets, b.recentOffsets = pushed, dRep
	if len(b.output)-start <= len(org)+3 {
		return nil
	}
	// Keep the bound of one block: write it whole.
	b.output = b.output[:start]
	sp.restore(b)
	b.recentOffsets = parsed
	return b.encodeBlock(org, raw, rawAllLits)
}

// encodeBlock encodes the block as one block.
func (b *blockEnc) encodeBlock(org []byte, raw, rawAllLits bool) error {
	if len(b.sequences) == 0 {
		return b.encodeLits(b.literals, rawAllLits)
	}
	if len(b.sequences) == 1 && len(org) > 0 && len(b.literals) <= 1 {
		// Check common RLE cases.
		seq := b.sequences[0]
		if seq.litLen == uint32(len(b.literals)) && seq.offset-3 == 1 {
			// Offset == 1 and 0 or 1 literals.
			b.encodeRLE(org[0], b.sequences[0].matchLen+zstdMinMatch+seq.litLen)
			return nil
		}
	}

	// We want some difference to at least account for the headers.
	saved := b.size - len(b.literals) - (b.size >> 6)
	// With highRatio, the sequences are dropped below only if that is smaller.
	fewMatches := saved < 16
	if fewMatches && !b.highRatio {
		if org == nil {
			return errIncompressible
		}
		b.popOffsets()
		return b.encodeLits(org, rawAllLits)
	}

	var bh blockHeader
	var lh literalsHeader
	bh.setLast(b.last)
	bh.setType(blockTypeCompressed)
	// Store offset of the block header. Needed when we know the size.
	bhOffset := len(b.output)
	b.output = bh.appendTo(b.output)

	var (
		out            []byte
		reUsed, single bool
		err            error
	)
	b.setLitOptions()
	if b.dictLitEnc != nil {
		b.litEnc.TransferCTable(b.dictLitEnc)
		b.litEnc.Reuse = huff0.ReusePolicyAllow
		b.dictLitEnc = nil
	}
	if len(b.literals) >= 1024 && !raw {
		// Use 4 Streams.
		out, reUsed, err = huff0.Compress4X(b.literals, b.litEnc)
	} else if len(b.literals) >= b.minHuffLits() && !raw {
		// Use 1 stream
		single = true
		out, reUsed, err = huff0.Compress1X(b.literals, b.litEnc)
	} else {
		err = huff0.ErrIncompressible
	}
	newTable := err == nil && !reUsed

	if err == nil && len(out)+5 > len(b.literals) {
		// If we are close, we may still be worse or equal to raw.
		var lh literalsHeader
		lh.setSize(len(b.literals))
		szRaw := lh.size()
		lh.setSizes(len(out), len(b.literals), single)
		szComp := lh.size()
		if len(out)+szComp >= len(b.literals)+szRaw {
			err = huff0.ErrIncompressible
			b.dropUnwrittenTable(reUsed)
		}
	}
	switch err {
	case huff0.ErrIncompressible:
		lh.setType(literalsBlockRaw)
		lh.setSize(len(b.literals))
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, b.literals...)
		if debugEncoder {
			println("Adding literals RAW, length", len(b.literals))
		}
	case huff0.ErrUseRLE:
		lh.setType(literalsBlockRLE)
		lh.setSize(len(b.literals))
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, b.literals[0])
		if debugEncoder {
			println("Adding literals RLE")
		}
	case nil:
		// Compressed litLen...
		if reUsed {
			if debugEncoder {
				println("reused tree")
			}
			lh.setType(literalsBlockTreeless)
		} else {
			if debugEncoder {
				println("new tree, size:", len(b.litEnc.OutTable))
			}
			lh.setType(literalsBlockCompressed)
			if debugEncoder {
				_, _, err := huff0.ReadTable(out, nil)
				if err != nil {
					panic(err)
				}
			}
		}
		lh.setSizes(len(out), len(b.literals), single)
		if debugEncoder {
			printf("Compressed %d literals to %d bytes", len(b.literals), len(out))
			println("Adding literal header:", lh)
		}
		b.output = lh.appendTo(b.output)
		b.output = append(b.output, out...)
		b.litEnc.Reuse = huff0.ReusePolicyAllow
		if debugEncoder {
			println("Adding literals compressed")
		}
	default:
		if debugEncoder {
			println("Adding literals ERROR:", err)
		}
		return err
	}
	// Size of the literals section if Huffman coded.
	huffSection := 0
	if err == nil {
		huffSection = len(b.output) - bhOffset - 3
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
	if debugEncoder {
		println("Encoding", len(b.sequences), "sequences")
	}
	b.genCodes()
	// The format allows offset tables up to log 8, the others up to 9.
	llEnc, llMode, _, err := b.chooseComp(len(b.sequences), b.output[len(b.output):], b.coders.llEnc, b.coders.llPrev, &fsePredefEnc[tableLiteralLengths], b.sequences[0].llCode, 9)
	if err != nil {
		return err
	}
	ofEnc, ofMode, _, err := b.chooseComp(len(b.sequences), b.output[len(b.output):], b.coders.ofEnc, b.coders.ofPrev, &fsePredefEnc[tableOffsets], b.sequences[0].ofCode, 8)
	if err != nil {
		return err
	}
	mlEnc, mlMode, _, err := b.chooseComp(len(b.sequences), b.output[len(b.output):], b.coders.mlEnc, b.coders.mlPrev, &fsePredefEnc[tableMatchLengths], b.sequences[0].mlCode, 9)
	if err != nil {
		return err
	}

	// Write compression mode
	mode := uint8(llMode)<<6 | uint8(ofMode)<<4 | uint8(mlMode)<<2
	b.output = append(b.output, mode)
	if debugEncoder {
		printf("Compression modes: 0b%b", mode)
	}
	b.output, err = llEnc.writeCount(b.output)
	if err != nil {
		return err
	}
	start := len(b.output)
	b.output, err = ofEnc.writeCount(b.output)
	if err != nil {
		return err
	}
	if false {
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
	wr := &b.wr
	wr.reset(b.output)

	var ll, of, ml cState

	// Current sequence
	seq := len(b.sequences) - 1
	s := b.sequences[seq]
	llEnc.setBits(llBitsTable[:])
	mlEnc.setBits(mlBitsTable[:])
	ofEnc.setBits(nil)

	llTT, ofTT, mlTT := llEnc.ct.symbolTT[:256], ofEnc.ct.symbolTT[:256], mlEnc.ct.symbolTT[:256]

	// We have 3 bounds checks here (and in the loop).
	// Since we are iterating backwards it is kinda hard to avoid.
	llB, ofB, mlB := llTT[s.llCode], ofTT[s.ofCode], mlTT[s.mlCode]
	ll.init(wr, &llEnc.ct, llB)
	of.init(wr, &ofEnc.ct, ofB)
	wr.flush32()
	ml.init(wr, &mlEnc.ct, mlB)

	// Each of these lookups also generates a bounds check.
	wr.addBits32NC(s.litLen, llB.outBits)
	wr.addBits32NC(s.matchLen, mlB.outBits)
	wr.flush32()
	wr.addBits32NC(s.offset, ofB.outBits)
	if debugSequences {
		println("Encoded seq", seq, s, "codes:", s.llCode, s.mlCode, s.ofCode, "states:", ll.state, ml.state, of.state, "bits:", llB, mlB, ofB)
	}
	seq--
	wr.flush32()

	// Store sequences in reverse, with the bit writer and states in locals.
	// flush stores 8 bytes and advances by whole bytes, so each chunk
	// checks room for seqMaxBytes per sequence plus the store's slack.
	const seqMaxBytes = 12 // 26 state bits + 63 extra bits.
	w := bitState{c: wr.bitContainer, nb: uint(wr.nBits), pos: len(wr.out)}
	buf := wr.out[:cap(wr.out)]
	ofState, mlState, llState := of.state, ml.state, ll.state
	ofTab, mlTab, llTab := of.stateTable, ml.stateTable, ll.stateTable
	seqs := b.sequences
	for seq >= 0 {
		n := (len(buf) - w.pos - 16) / seqMaxBytes
		if n <= 0 {
			buf = slices.Grow(buf[:w.pos], (seq+1)*seqMaxBytes+16)
			buf = buf[:cap(buf)]
			continue
		}
		for stop := max(seq-n, -1); seq > stop; seq-- {
			s := seqs[seq]
			ofB, mlB, llB := ofTT[s.ofCode], mlTT[s.mlCode], llTT[s.llCode]

			// w.nb <= 37 here, and the states add at most 26 bits.
			w, ofState = w.addState(ofState, ofTab, ofB)
			w, mlState = w.addState(mlState, mlTab, mlB)
			w, llState = w.addState(llState, llTab, llB)

			ofBits, mlBits, llBits := ofB.outBits&31, mlB.outBits&31, llB.outBits&31
			extra := uint64(s.offset&bitMask32[ofBits])<<(mlBits+llBits) |
				uint64(s.matchLen&bitMask32[mlBits])<<llBits |
				uint64(s.litLen&bitMask32[llBits])
			extraN := uint(ofBits + mlBits + llBits)
			if w.nb+extraN > 63 {
				w = w.flush(buf)
				if extraN > 56 {
					// Only offsets over 24 bits get here.
					low := uint(mlBits + llBits)
					w = w.add(extra&(1<<low-1), low).flush(buf)
					extra >>= low
					extraN -= low
				}
			}
			w = w.add(extra, extraN).flush(buf)

			if debugSequences {
				println("Encoded seq", seq, s)
			}
		}
	}
	wr.bitContainer, wr.nBits, wr.out = w.c, uint8(w.nb), buf[:w.pos]
	of.state, ml.state, ll.state = ofState, mlState, llState
	ml.flush(mlEnc.actualTableLog)
	of.flush(ofEnc.actualTableLog)
	ll.flush(llEnc.actualTableLog)
	wr.close()
	b.output = wr.out

	// Literals only would code the matched bytes at about the literals' rate.
	// The product fits 32 bits, since there are few matched bytes.
	if fewMatches && huffSection > 0 && org != nil && !rawAllLits &&
		4+huffSection+huffSection*(len(org)-len(b.literals))/len(b.literals) < len(b.output)-bhOffset {
		b.output = b.output[:bhOffset]
		b.popOffsets()
		if newTable {
			b.litEnc.RestorePrevTable()
		}
		return b.encodeLits(org, rawAllLits)
	}

	// Maybe even add a bigger margin.
	if len(b.output)-3-bhOffset >= b.size {
		// Discard and encode as raw block.
		b.output = b.encodeRawTo(b.output[:bhOffset], org)
		b.popOffsets()
		switch {
		case !b.highRatio:
			b.litEnc.Reuse = huff0.ReusePolicyNone
		case newTable:
			b.litEnc.RestorePrevTable()
		}
		return nil
	}

	// Size is output minus block header.
	bh.setSize(uint32(len(b.output)-bhOffset) - 3)
	if debugEncoder {
		println("Rewriting block header", bh)
	}
	_ = bh.appendTo(b.output[bhOffset:bhOffset])
	b.coders.setPrev(llEnc, mlEnc, ofEnc)
	return nil
}

// chooseComp chooses the cheapest of RLE and the new, predefined and previous table
// for the histogram of nSeq codes in cur, and returns its estimated size in bits.
// rle is the code to use if all sequences have the same one.
// The new table is only built if it could be the cheapest. Its header is written
// to the spare capacity of hdr.
// With b.highRatio, the new table can have table log up to maxLog.
func (b *blockEnc) chooseComp(nSeq int, hdr []byte, cur, prev, preDef *fseEncoder, rle, maxLog uint8) (*fseEncoder, seqCompMode, uint32, error) {
	if cur.maxCount == nSeq {
		cur.setRLE(rle)
		if debugEncoder {
			println("Using RLE, code:", rle)
		}
		return cur, compModeRLE, 8, nil
	}
	cur.useRLE = false
	hist := cur.count[:cur.symbolLen]
	predefSize := preDef.approxSize(hist)
	prevSize := prev.approxSize(hist)
	if predefSize != math.MaxUint32 {
		predefSize += uint32(preDef.actualTableLog)
	}
	if prevSize != math.MaxUint32 {
		prevSize += uint32(prev.actualTableLog)
	}
	if min(predefSize, prevSize) <= minNewSize(hist, nSeq) || forcePreDef {
		if predefSize <= prevSize || forcePreDef {
			if debugEncoder {
				println("Using predefined", predefSize>>3, "without a new table")
			}
			return preDef, compModePredefined, predefSize, nil
		}
		if debugEncoder {
			println("Using previous", prevSize>>3, "without a new table")
		}
		return prev, compModeRepeat, prevSize, nil
	}
	if !b.highRatio {
		maxLog = maxEncTableLog
	}
	if err := cur.normalizeCountMax(nSeq, maxLog); err != nil {
		return nil, 0, 0, err
	}
	nSize := cur.approxSize(hist) + cur.maxHeaderSize()

	// Add a small penalty for new encoders.
	// Don't bother with extremely small (<2 byte gains).
	nSize = nSize + (nSize+2*8*16)>>4
	// The previous table may come from a dictionary and fit poorly,
	// so it is compared against the exact header size, without penalty.
	nSizePrev := nSize
	if prevSize != math.MaxUint32 || b.highRatio {
		if hdr, err := cur.writeCount(hdr); err == nil {
			nSizePrev = cur.approxSize(hist) + uint32(len(hdr))*8
		}
	}
	if b.highRatio {
		// No penalty against the predefined table either.
		nSize = nSizePrev
	}
	// Each stream ends with a flush of tableLog bits.
	nSize += uint32(cur.actualTableLog)
	nSizePrev += uint32(cur.actualTableLog)
	switch {
	case predefSize <= prevSize && predefSize <= nSize:
		if debugEncoder {
			println("Using predefined", predefSize>>3, "<=", nSize>>3)
		}
		return preDef, compModePredefined, predefSize, nil
	case prevSize <= nSizePrev:
		if debugEncoder {
			println("Using previous", prevSize>>3, "<=", nSizePrev>>3)
		}
		return prev, compModeRepeat, prevSize, nil
	default:
		if debugEncoder {
			println("Using new, predef", predefSize>>3, ". previous:", prevSize>>3, ">", nSize>>3, "header max:", cur.maxHeaderSize()>>3, "bytes")
			println("tl:", cur.actualTableLog, "symbolLen:", cur.symbolLen, "norm:", cur.norm[:cur.symbolLen], "hist", cur.count[:cur.symbolLen])
		}
		return cur, compModeFSE, nSizePrev, nil
	}
}

var errIncompressible = errors.New("incompressible")

func (b *blockEnc) genCodes() {
	if len(b.sequences) == 0 {
		// nothing to do
		return
	}
	if len(b.sequences) > math.MaxUint16 {
		panic("can only encode up to 64K sequences")
	}
	// No bounds checks after here:
	llH := b.coders.llEnc.Histogram()
	ofH := b.coders.ofEnc.Histogram()
	mlH := b.coders.mlEnc.Histogram()
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
	for i := range b.sequences {
		seq := &b.sequences[i]
		v := llCode(seq.litLen)
		seq.llCode = v
		llH[v]++
		if v > llMax {
			llMax = v
		}

		v = ofCode(seq.offset)
		seq.ofCode = v
		ofH[v]++
		if v > ofMax {
			ofMax = v
		}

		v = mlCode(seq.matchLen)
		seq.mlCode = v
		mlH[v]++
		if v > mlMax {
			mlMax = v
			if debugAsserts && mlMax > maxMatchLengthSymbol {
				panic(fmt.Errorf("mlMax > maxMatchLengthSymbol (%d), matchlen: %d", mlMax, seq.matchLen))
			}
		}
	}
	if debugAsserts && mlMax > maxMatchLengthSymbol {
		panic(fmt.Errorf("mlMax > maxMatchLengthSymbol (%d)", mlMax))
	}
	if debugAsserts && ofMax > maxOffsetBits {
		panic(fmt.Errorf("ofMax > maxOffsetBits (%d)", ofMax))
	}
	if debugAsserts && llMax > maxLiteralLengthSymbol {
		panic(fmt.Errorf("llMax > maxLiteralLengthSymbol (%d)", llMax))
	}

	b.coders.mlEnc.HistogramFinished(mlMax, int(slices.Max(mlH[:mlMax+1])))
	b.coders.ofEnc.HistogramFinished(ofMax, int(slices.Max(ofH[:ofMax+1])))
	b.coders.llEnc.HistogramFinished(llMax, int(slices.Max(llH[:llMax+1])))
}

// The post block splitter (ZSTD_deriveBlockSplits) halves the sequences of a
// block while the halves are estimated smaller. The estimates use the tables
// from the start of the part being split.
const (
	splitMinSeqs = 300 // MIN_SEQUENCES_BLOCK_SPLITTING
	splitMaxCuts = 196 // ZSTD_MAX_NB_BLOCK_SPLITS
)

// blockSplit is scratch space for writing a block as several.
type blockSplit struct {
	parts []blockPart
	cuts  []int // sequences to split before
	litAt []int // literal index of each sequence, and the end

	// Estimates.
	lit        huff0.Scratch
	litReuse   bool
	ll, of, ml fseEncoder
	hdr        []byte

	// The state before the block, to write it as one block after all.
	litPrev                huff0.Scratch
	reuse                  huff0.ReusePolicy
	dictLit                *huff0.Scratch
	llPrev, ofPrev, mlPrev fseEncoder
	offsets                [][2]uint32 // sequence index and repeat code of rewritten offsets
}

func newBlockSplit() *blockSplit {
	sp := &blockSplit{hdr: make([]byte, 0, 256)}
	// Estimates write no output, so it needs no room.
	sp.lit.Out = make([]byte, 0, 1)
	return sp
}

// save keeps the entropy tables from before the block.
func (sp *blockSplit) save(b *blockEnc) {
	sp.litPrev.TransferCTable(b.litEnc)
	sp.reuse, sp.dictLit = b.litEnc.Reuse, b.dictLitEnc
	sp.llPrev.copyFrom(b.coders.llPrev)
	sp.ofPrev.copyFrom(b.coders.ofPrev)
	sp.mlPrev.copyFrom(b.coders.mlPrev)
	sp.offsets = sp.offsets[:0]
}

// restore undoes the parts written since save.
func (sp *blockSplit) restore(b *blockEnc) {
	b.litEnc.TransferCTable(&sp.litPrev)
	b.litEnc.Reuse, b.dictLitEnc = sp.reuse, sp.dictLit
	b.coders.llPrev.copyFrom(&sp.llPrev)
	b.coders.ofPrev.copyFrom(&sp.ofPrev)
	b.coders.mlPrev.copyFrom(&sp.mlPrev)
	for _, o := range sp.offsets {
		b.sequences[o[0]].offset = o[1]
	}
}

// resolveReps rewrites the repeat codes of seqs that the decoder, with repeat
// offsets dRep, would resolve differently from the parse, which had cRep
// (ZSTD_seqStore_resolveOffCodes). first is the index of seqs[0] in the block.
// It returns both repeat offsets after seqs.
func (sp *blockSplit) resolveReps(seqs []seq, first int, cRep, dRep [3]uint32) ([3]uint32, [3]uint32) {
	for i := range seqs {
		s := &seqs[i]
		off, ll0 := s.offset, s.litLen == 0
		if off <= 3 {
			if c := repOffset(cRep, off, ll0); c != repOffset(dRep, off, ll0) {
				sp.offsets = append(sp.offsets, [2]uint32{uint32(first + i), off})
				s.offset = c + 3
			}
		}
		cRep, dRep = ultraNewRep(cRep, off, ll0), ultraNewRep(dRep, s.offset, ll0)
	}
	return cRep, dRep
}

// repOffset returns the offset that repeat code offBase refers to with repeat offsets r.
func repOffset(r [3]uint32, offBase uint32, ll0 bool) uint32 {
	if ll0 {
		offBase++
	}
	if offBase == 4 {
		return r[0] - 1
	}
	return r[offBase-1]
}

// splitParts returns the parts the block is written as. It is cut before the
// sequences where the halves are estimated smaller, or at b.testCuts.
// The last part also takes the literals after the last sequence.
func (b *blockEnc) splitParts(raw bool) []blockPart {
	sp := b.split
	seqs := b.sequences
	p := blockPart{lits: len(b.literals), seqs: len(seqs), size: b.size}
	sp.parts = append(sp.parts[:0], p)
	if p.seqs < splitMinSeqs && b.testCuts == nil {
		return sp.parts
	}
	sp.litAt = sp.litAt[:0]
	l := 0
	for i := range seqs {
		q := &seqs[i]
		sp.litAt = append(sp.litAt, l)
		l += int(q.litLen)
		q.llCode, q.mlCode, q.ofCode = llCode(q.litLen), mlCode(q.matchLen), ofCode(q.offset)
	}
	sp.litAt = append(sp.litAt, p.lits)
	cuts := b.testCuts
	if cuts == nil {
		src := b.dictLitEnc
		sp.litReuse = src != nil || b.litEnc.Reuse != huff0.ReusePolicyNone
		if src == nil {
			src = b.litEnc
		}
		if sp.litReuse {
			sp.lit.TransferCTable(src)
		}
		sp.lit.OptimalDepth = b.ultra
		sp.cuts = sp.cuts[:0]
		b.splitRange(seqs, 0, len(seqs), b.estPart(seqs, 0, len(seqs), raw), raw)
		cuts = sp.cuts
	}
	if len(cuts) == 0 {
		return sp.parts
	}
	sp.parts = sp.parts[:0]
	a, lits, size := 0, 0, 0
	for _, c := range cuts {
		q := blockPart{lits: sp.litAt[c] - sp.litAt[a], seqs: c - a}
		q.size = q.lits + q.seqs*zstdMinMatch
		for _, x := range seqs[a:c] {
			q.size += int(x.matchLen)
		}
		sp.parts = append(sp.parts, q)
		a, lits, size = c, lits+q.lits, size+q.size
	}
	sp.parts = append(sp.parts, blockPart{lits: p.lits - lits, seqs: p.seqs - a, size: p.size - size})
	return sp.parts
}

// splitRange adds cuts that split seqs[a:e], estimated at est bytes, where
// the halves are estimated smaller (ZSTD_deriveBlockSplitsHelper).
func (b *blockEnc) splitRange(seqs []seq, a, e, est int, raw bool) {
	sp := b.split
	if e-a < splitMinSeqs || len(sp.cuts) >= splitMaxCuts {
		return
	}
	m := (a + e) / 2
	est1, est2 := b.estPart(seqs, a, m, raw), b.estPart(seqs, m, e, raw)
	if est1+est2 >= est {
		return
	}
	b.splitRange(seqs, a, m, est1, raw)
	if len(sp.cuts) >= splitMaxCuts {
		return
	}
	sp.cuts = append(sp.cuts, m)
	b.splitRange(seqs, m, e, est2, raw)
}

// estPart estimates the size of seqs[a:e] written as a block (ZSTD_estimateBlockSize).
func (b *blockEnc) estPart(seqs []seq, a, e int, raw bool) int {
	sp := b.split
	return 3 + b.estLits(b.literals[sp.litAt[a]:sp.litAt[e]], raw) + b.estSeqs(seqs[a:e])
}

// estLits estimates the size of the literals section for lits.
func (b *blockEnc) estLits(lits []byte, raw bool) int {
	n := len(lits)
	rawSize := n + 1
	if n >= 32 {
		rawSize++
	}
	if n >= 4096 {
		rawSize++
	}
	if raw || n < b.minHuffLits() {
		return rawSize
	}
	sp := b.split
	tableSz, dataSz, reuseSz, err := huff0.EstimateSizes(lits, &sp.lit)
	switch err {
	case nil:
	case huff0.ErrUseRLE:
		return rawSize - n + 1
	default:
		return rawSize
	}
	// The header, and the jump table of 4 streams.
	hdr := 3
	switch {
	case n >= 16<<10:
		hdr = 5 + 6
	case n >= 1024:
		hdr = 4 + 6
	}
	if sp.litReuse && reuseSz >= 0 && reuseSz < n && (reuseSz <= tableSz+dataSz || tableSz+12 >= n) {
		return hdr + reuseSz
	}
	if tableSz+dataSz >= n {
		return rawSize
	}
	return hdr + tableSz + dataSz
}

// estSeqs estimates the size of the sequences section for seqs, whose codes are set.
func (b *blockEnc) estSeqs(seqs []seq) int {
	sp := b.split
	llH, ofH, mlH := sp.ll.Histogram(), sp.of.Histogram(), sp.ml.Histogram()
	clear(llH[:])
	clear(ofH[:])
	clear(mlH[:])
	var llMax, ofMax, mlMax uint8
	var bits uint32
	for _, s := range seqs {
		llH[s.llCode]++
		ofH[s.ofCode]++
		mlH[s.mlCode]++
		llMax, ofMax, mlMax = max(llMax, s.llCode), max(ofMax, s.ofCode), max(mlMax, s.mlCode)
		bits += uint32(llBitsTable[s.llCode]) + uint32(mlBitsTable[s.mlCode]) + uint32(s.ofCode)
	}
	sp.ll.HistogramFinished(llMax, int(slices.Max(llH[:llMax+1])))
	sp.of.HistogramFinished(ofMax, int(slices.Max(ofH[:ofMax+1])))
	sp.ml.HistogramFinished(mlMax, int(slices.Max(mlH[:mlMax+1])))
	n := len(seqs)
	_, _, ll, err1 := b.chooseComp(n, sp.hdr, &sp.ll, b.coders.llPrev, &fsePredefEnc[tableLiteralLengths], seqs[0].llCode, 9)
	_, _, of, err2 := b.chooseComp(n, sp.hdr, &sp.of, b.coders.ofPrev, &fsePredefEnc[tableOffsets], seqs[0].ofCode, 8)
	_, _, ml, err3 := b.chooseComp(n, sp.hdr, &sp.ml, b.coders.mlPrev, &fsePredefEnc[tableMatchLengths], seqs[0].mlCode, 9)
	if err1 != nil || err2 != nil || err3 != nil {
		return n * 10
	}
	// Number of sequences, modes, and the end mark of the bitstream.
	hdr := 2
	if n >= 128 {
		hdr++
	}
	if n >= 0x7f00 {
		hdr++
	}
	return hdr + int(bits+ll+of+ml+8)/8
}
