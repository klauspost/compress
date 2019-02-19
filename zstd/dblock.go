package zstd

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/klauspost/compress/huff0"
)

type BlockType uint8

//go:generate stringer -type=BlockType,LiteralsBlockType,seqCompMode,tableIndex

const (
	BlockTypeRaw BlockType = iota
	BlockTypeRLE
	BlockTypeCompressed
	BlockTypeReserved

	// maxCompressedBlockSize is the biggest allowed compressed block size (128KB)
	maxCompressedBlockSize = 128 << 10

	// Maximum possible block size (all Raw+Uncompressed).
	maxBlockSize = (1 << 21) - 1

	maxMatchLen = 131074

	// We support slightly less than the reference decoder to be able to
	// use ints on 32 bit archs.
	maxOffsetBits = 30
)

const (
	LiteralsBlockRaw LiteralsBlockType = iota
	LiteralsBlockRLE
	LiteralsBlockCompressed
	LiteralsBlockTreeless
)

type LiteralsBlockType uint8

var (
	// ErrReservedBlockType is returned when a reserved block type is found.
	// Typically this indicates wrong or corrupted input.
	ErrReservedBlockType = errors.New("invalid input: reserved block type encountered")

	// ErrReservedBlockType is returned when a reserved block type is found.
	// Typically this indicates wrong or corrupted input.
	ErrCompressedSizeTooBig = errors.New("invalid input: compressed size too big")

	huffDecoderPool = sync.Pool{New: func() interface{} {
		return &huff0.Scratch{}
	}}

	fseDecoderPool = sync.Pool{New: func() interface{} {
		return &fseDecoder{}
	}}
)

type dBlock struct {
	data       []byte
	dst        []byte
	literalBuf []byte
	WindowSize uint64
	Type       BlockType
	RLESize    uint32
	Last       bool
	// Use less memory
	lowMem      bool
	history     chan *history
	input       chan struct{}
	result      chan decodeOutput
	sequenceBuf []seq
}

func (b *dBlock) String() string {
	if b == nil {
		return "<nil>"
	}
	return fmt.Sprintf("Steam Size: %d, Type: %v, Last: %t, Window: %d", len(b.data), b.Type, b.Last, b.WindowSize)
}

func newDBlock(lowMem bool) *dBlock {
	b := dBlock{
		lowMem:  lowMem,
		result:  make(chan decodeOutput, 1),
		input:   make(chan struct{}, 1),
		history: make(chan *history, 1),
	}
	go b.startDecoder()
	return &b
}

// reset will reset the block.
// Input must be a start of a block and will be at the end of the block when returned.
func (b *dBlock) reset(br io.Reader, windowSize uint64) error {
	b.WindowSize = windowSize
	var tmp [4]byte
	_, err := io.ReadFull(br, tmp[:3])
	if err != nil {
		if debug {
			fmt.Println("Reading block header:", err)
		}
		return err
	}
	bh := uint32(tmp[0]) | (uint32(tmp[1]) << 8) | (uint32(tmp[2]) << 16)
	b.Last = bh&1 != 0
	b.Type = BlockType((bh >> 1) & 3)
	// find size.
	cSize := int(bh >> 3)
	switch b.Type {
	case BlockTypeReserved:
		return ErrReservedBlockType
	case BlockTypeRLE:
		b.RLESize = uint32(cSize)
		cSize = 1
	case BlockTypeCompressed:
		if debug {
			fmt.Println("Data size on stream:", cSize)
		}
		b.RLESize = 0
		if cSize > maxCompressedBlockSize || uint64(cSize) > b.WindowSize {
			if debug {
				fmt.Printf("compressed block too big: %+v\n", b)
			}
			return ErrCompressedSizeTooBig
		}
	default:
		b.RLESize = 0
	}

	// Read block data.
	if cap(b.data) < cSize {
		if b.lowMem {
			b.data = make([]byte, 0, maxBlockSize)
		} else {
			b.data = make([]byte, 0, cSize)
		}
	}
	if cap(b.dst) <= maxBlockSize {
		b.dst = make([]byte, 0, maxBlockSize+1)
	}
	b.data = b.data[:cSize]
	// Read all.
	_, err = io.ReadFull(br, b.data)
	if err != nil {
		if debug {
			fmt.Println("Reading block:", err)
		}
		return err
	}
	// Start decoding
	b.input <- struct{}{}
	return nil
}

// Close will release resources.
// Closed dBlock cannot be reset.
func (b *dBlock) Close() {
	close(b.input)
	close(b.history)
	close(b.result)
}

// decode will prepare decoding the block when it receives the history.
func (b *dBlock) startDecoder() {
	for {
		_, ok := <-b.input
		if !ok {
			return
		}
		//fmt.Println("dBlock: Got block input")
		switch b.Type {
		case BlockTypeRLE:
			if cap(b.dst) < int(b.RLESize) {
				if b.lowMem {
					b.dst = make([]byte, b.RLESize)
				} else {
					b.dst = make([]byte, maxBlockSize)
				}
			}
			o := decodeOutput{
				d:   b,
				b:   b.dst[:b.RLESize],
				err: nil,
			}
			v := b.data[0]
			for i := range o.b {
				o.b[i] = v
			}
			hist := <-b.history
			hist.append(o.b)
			// TODO: We should check if result is closed.
			b.result <- o
		case BlockTypeRaw:
			o := decodeOutput{
				d:   b,
				b:   b.data,
				err: nil,
			}
			hist := <-b.history
			hist.append(o.b)
			// TODO: We should check if result is closed.
			b.result <- o
		case BlockTypeCompressed:
			err := b.decodeCompressed()
			o := decodeOutput{
				d:   b,
				b:   b.dst,
				err: err,
			}
			//fmt.Println("Decompressed to ", len(b.dst), "bytes, error:", err)
			b.result <- o
		default:
			panic("Invalid block type")
		}
		//fmt.Println("dBlock: Finished block")
	}
}

var ErrBlockTooSmall = errors.New("block too small")

// decodeCompressed ...
func (b *dBlock) decodeCompressed() error {
	b.dst = b.dst[:0]
	in := b.data
	// There must be at least one byte for Literals_Block_Type and one for Sequences_Section_Header
	if len(in) < 2 {
		return ErrBlockTooSmall
	}
	litType := LiteralsBlockType(in[0] & 3)
	var litRegenSize int
	var litCompSize int
	sizeFormat := (in[0] >> 2) & 3
	var fourStreams bool
	//fmt.Println("Literals type:", litType, "sizeFormat:", sizeFormat)
	switch litType {
	case LiteralsBlockRaw, LiteralsBlockRLE:
		switch sizeFormat {
		case 0, 2:
			// Regenerated_Size uses 5 bits (0-31). Literals_Section_Header uses 1 byte.
			litRegenSize = int(in[0] >> 3)
			in = in[1:]
		case 1:
			// Regenerated_Size uses 12 bits (0-4095). Literals_Section_Header uses 2 bytes.
			litRegenSize = int(in[0]>>4) + (int(in[1]) << 4)
			in = in[2:]
		case 3:
			//  Regenerated_Size uses 20 bits (0-1048575). Literals_Section_Header uses 3 bytes.
			if len(in) < 3 {
				fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, len(in))
				return ErrBlockTooSmall
			}
			litRegenSize = int(in[0]>>4) + (int(in[1]) << 4) + (int(in[2]) << 12)
			in = in[3:]
		}
	case LiteralsBlockCompressed, LiteralsBlockTreeless:
		switch sizeFormat {
		case 0, 1:
			// Both Regenerated_Size and Compressed_Size use 10 bits (0-1023).
			if len(in) < 3 {
				fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, len(in))
				return ErrBlockTooSmall
			}
			n := uint64(in[0]>>4) + (uint64(in[1]) << 4) + (uint64(in[2]) << 12)
			litRegenSize = int(n & 1023)
			litCompSize = int(n >> 10)
			fourStreams = sizeFormat == 1
			in = in[3:]
		case 2:
			fourStreams = true
			if len(in) < 4 {
				fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, len(in))
				return ErrBlockTooSmall
			}
			n := uint64(in[0]>>4) + (uint64(in[1]) << 4) + (uint64(in[2]) << 12) + (uint64(in[3]) << 20)
			litRegenSize = int(n & 16383)
			litCompSize = int(n >> 14)
			in = in[4:]
		case 3:
			fourStreams = true
			if len(in) < 5 {
				fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, len(in))
				return ErrBlockTooSmall
			}
			n := uint64(in[0]>>4) + (uint64(in[1]) << 4) + (uint64(in[2]) << 12) + (uint64(in[3]) << 20) + (uint64(in[4]) << 28)
			litRegenSize = int(n & 262143)
			litCompSize = int(n >> 18)
			in = in[5:]
		}
	}
	var literals []byte
	var huff *huff0.Scratch
	switch litType {
	case LiteralsBlockRaw:
		if len(in) < litRegenSize {
			fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, "remain:", len(in), "want:", litRegenSize)
			return ErrBlockTooSmall
		}
		literals = in[:litRegenSize]
		in = in[litRegenSize:]
		//fmt.Printf("Found %d uncompressed literals\n", litRegenSize)
	case LiteralsBlockRLE:
		if len(in) < 1 {
			fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, "remain:", len(in), "want:", 1)
			return ErrBlockTooSmall
		}
		if cap(b.literalBuf) < litRegenSize {
			if b.lowMem {
				b.literalBuf = make([]byte, litRegenSize)
			} else {
				b.literalBuf = make([]byte, litRegenSize, 1<<18)
			}
		}
		literals = b.literalBuf[:litRegenSize]
		v := in[0]
		for i := range literals {
			literals[i] = v
		}
		in = in[1:]
		//fmt.Printf("Found %d RLE compressed literals\n", litRegenSize)
	case LiteralsBlockTreeless:
		if len(in) < litCompSize {
			fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, "remain:", len(in), "want:", litCompSize)
			return ErrBlockTooSmall
		}
		// Store compressed literals, so we defer decoding until we get history.
		literals = in[:litCompSize]
		in = in[litCompSize:]
		//fmt.Printf("Found %d compressed literals\n", litCompSize)
	case LiteralsBlockCompressed:
		if len(in) < litCompSize {
			fmt.Println("too small: litType:", litType, " sizeFormat", sizeFormat, "remain:", len(in), "want:", litCompSize)
			return ErrBlockTooSmall
		}
		literals = in[:litCompSize]
		in = in[litCompSize:]

		huff = huffDecoderPool.Get().(*huff0.Scratch)
		var err error
		huff, literals, err = huff0.ReadTable(literals, huff)
		if err != nil {
			fmt.Println("reading huffman table:", err)
			return err
		}
		// Ensure we have space to store it.
		if cap(b.literalBuf) < litRegenSize {
			if b.lowMem {
				b.literalBuf = make([]byte, 0, litRegenSize)
			} else {
				b.literalBuf = make([]byte, 0, 1<<18)
			}
		}
		// Use our out buffer.
		huff.Out = b.literalBuf[:0]
		if fourStreams {
			literals, err = huff.Decompress4X(literals, litRegenSize)
		} else {
			literals, err = huff.Decompress1X(literals)
		}
		if err != nil {
			return err
		}
		if len(literals) != litRegenSize {
			return fmt.Errorf("literal output size mismatch want %d, got %d", litRegenSize, len(literals))
		}
		//fmt.Printf("Decompressed %d literals into %d bytes\n", litCompSize, litRegenSize)
	}

	// Decode Sequences
	// https://github.com/facebook/zstd/blob/dev/doc/zstd_compression_format.md#sequences-section
	if len(in) < 1 {
		return ErrBlockTooSmall
	}
	seqHeader := in[0]
	nSeqs := 0
	switch {
	case seqHeader == 0:
		in = in[1:]
	case seqHeader < 128:
		nSeqs = int(seqHeader)
		in = in[1:]
	case seqHeader < 255:
		if len(in) < 2 {
			return ErrBlockTooSmall
		}
		nSeqs = int(seqHeader-128)<<8 | int(in[1])
		in = in[2:]
	case seqHeader == 255:
		if len(in) < 3 {
			return ErrBlockTooSmall
		}
		nSeqs = 0x7f00 + int(in[1]) + (int(in[2]) << 8)
		in = in[3:]
	}

	// Allocate sequences
	if cap(b.sequenceBuf) < nSeqs {
		if b.lowMem {
			b.sequenceBuf = make([]seq, nSeqs)
		} else {
			// Allocate max
			b.sequenceBuf = make([]seq, nSeqs, 0x7f00+0xffff)
		}
	} else {
		// Reuse buffer
		b.sequenceBuf = b.sequenceBuf[:nSeqs]
	}
	var seqs = &sequenceDecoders{}
	if nSeqs > 0 {
		if len(in) < 1 {
			return ErrBlockTooSmall
		}
		br := byteReader{b: in, off: 0}
		compMode := br.Uint8()
		br.advance(1)
		for i := uint(0); i < 3; i++ {
			mode := seqCompMode((compMode >> (6 - i*2)) & 3)
			//fmt.Println("Table", tableIndex(i), "is", mode)
			var seq *sequenceDecoder
			switch tableIndex(i) {
			case tableLiteralLengths:
				seq = &seqs.litLengths
			case tableOffsets:
				seq = &seqs.offsets
			case tableMatchLengths:
				seq = &seqs.matchLengths
			}
			switch mode {
			case compModePredefined:
				seq.fse = &fsePredef[i]
			case compModeRLE:
				if br.remain() < 1 {
					return ErrBlockTooSmall
				}
				v := br.Uint8()
				br.advance(1)
				dec := fseDecoderPool.Get().(*fseDecoder)
				symb, err := decSymbolValue(v, symbolTableX[i])
				if err != nil {
					fmt.Println("RLE Transform table error:", err)
					return err
				}
				dec.setRLE(symb)
				seq.fse = dec
				//fmt.Println("RLE set to ", *symb)
			case compModeFSE:
				//fmt.Println("Reading table for", tableIndex(i))
				dec := fseDecoderPool.Get().(*fseDecoder)
				err := dec.readNCount(&br, uint16(maxTableSymbol[i]))
				if err != nil {
					fmt.Println("Read table error:", err)
					return err
				}
				//fmt.Println("Read table ok")
				err = dec.transform(symbolTableX[i])
				if err != nil {
					fmt.Println("Transform table error:", err)
					return err
				}
				seq.fse = dec
			case compModeRepeat:
				seq.repeat = true
			}
			if br.overread() {
				return io.ErrUnexpectedEOF
			}
		}
		in = br.unread()
	}

	// Wait for history.
	// All time spent after this is critical since it is strictly sequential.
	hist := <-b.history

	// Decode treeless literal block.
	if litType == LiteralsBlockTreeless {
		if hist.huffTree == nil {
			return errors.New("literal block was treeless, but no history was defined")
		}
		// Ensure we have space to store it.
		if cap(b.literalBuf) < litRegenSize {
			if b.lowMem {
				b.literalBuf = make([]byte, 0, litRegenSize)
			} else {
				b.literalBuf = make([]byte, 0, 1<<18)
			}
		}
		var err error
		// Use our out buffer.
		huff = hist.huffTree
		huff.Out = b.literalBuf[:0]
		if fourStreams {
			literals, err = huff.Decompress4X(literals, litRegenSize)
		} else {
			literals, err = huff.Decompress1X(literals)
		}
		if err != nil {
			fmt.Println("decompressing literals:", err)
			return err
		}
		if len(literals) != litRegenSize {
			return fmt.Errorf("literal output size mismatch want %d, got %d", litRegenSize, len(literals))
		}
	} else {
		if hist.huffTree != nil && huff != nil {
			huffDecoderPool.Put(hist.huffTree)
			hist.huffTree = nil
		}
	}
	if huff != nil {
		huff.Out = nil
		hist.huffTree = huff
	}
	//fmt.Println("Final literals:", len(literals), "and", nSeqs, "sequences.")

	if nSeqs == 0 {
		// Decompressed content is defined entirely as Literals Section content.
		b.dst = append(b.dst, literals...)
		hist.append(literals)
		return nil
	}

	seqs, err := seqs.mergeHistory(&hist.decoders)
	if err != nil {
		return err
	}
	//fmt.Println("History merged ok")

	br := &bitReader{}
	if err := br.init(in); err != nil {
		return err
	}

	if err := seqs.initialize(br, hist, literals, b.dst[:0]); err != nil {
		fmt.Println("initializing sequences:", err)
		return err
	}

	err = seqs.decode(nSeqs, br, hist.b)
	if err != nil {
		return err
	}
	if !br.finished() {
		return fmt.Errorf("%d extra bits on block, should be 0", br.remain())
	}

	err = br.close()
	if err != nil {
		fmt.Printf("Closing sequences: %v, %+v\n", err, *br)
	}

	// Set output and release references.
	b.dst = seqs.out
	seqs.out, seqs.literals, seqs.hist = nil, nil, nil

	if b.Last {
		// if last block we don't care about history.
		//fmt.Println("Last block, no history returned")
		hist.b = hist.b[:0]
		return nil
	}
	hist.append(b.dst)
	hist.recentOffsets = seqs.prevOffset
	return nil
}

type sequenceDecoder struct {
	// decoder keeps track of the current state and updates it from the bitstream.
	fse    *fseDecoder
	state  fseState
	repeat bool
}

func (s *sequenceDecoder) init(br *bitReader) error {
	if s.fse == nil {
		return errors.New("sequence decoder not defined")
	}
	s.state.init(br, s.fse.actualTableLog, s.fse.dt[:1<<s.fse.actualTableLog])
	return nil
}

type sequenceDecoders struct {
	litLengths   sequenceDecoder
	offsets      sequenceDecoder
	matchLengths sequenceDecoder
	prevOffset   [3]int
	hist         []byte
	literals     []byte
	out          []byte
	maxBits      uint8
}

func (s *sequenceDecoders) initialize(br *bitReader, hist *history, literals, out []byte) error {
	if err := s.litLengths.init(br); err != nil {
		return errors.New("litLengths:" + err.Error())
	}
	if err := s.offsets.init(br); err != nil {
		return errors.New("litLengths:" + err.Error())
	}
	if err := s.matchLengths.init(br); err != nil {
		return errors.New("matchLengths:" + err.Error())
	}
	s.literals = literals
	s.hist = hist.b
	s.prevOffset = hist.recentOffsets
	s.maxBits = s.litLengths.fse.maxBits + s.offsets.fse.maxBits + s.matchLengths.fse.maxBits
	//fmt.Println("litLengths index:", s.litLengths.state.state, "state:", s.litLengths.state.dt[s.litLengths.state.state])
	//fmt.Println("offsets index:", s.offsets.state.state, "state:", s.offsets.state.dt[s.offsets.state.state])
	//fmt.Println("matchLengths index:", s.matchLengths.state.state, "state:", s.matchLengths.state.dt[s.matchLengths.state.state])
	s.out = out
	return nil
}

func (s *sequenceDecoders) decode(seqs int, br *bitReader, hist []byte) error {
	for i := seqs - 1; i >= 0; i-- {
		if br.overread() {
			fmt.Printf("reading sequence %d, exceeded available data\n", seqs-i)
			return io.ErrUnexpectedEOF
		}
		var litLen, matchOff, matchLen int
		if br.off > 4+((maxOffsetBits+16+16)>>3) {
			litLen, matchOff, matchLen = s.nextFast(br)
			br.fillFast()
		} else {
			litLen, matchOff, matchLen = s.next(br)
			br.fill()
		}

		//		fmt.Printf("Seq %d: Litlen: %d, matchOff: %d, matchLen: %d\n", seqs-i, litLen, matchOff, matchLen)

		if litLen > len(s.literals) {
			return fmt.Errorf("unexpected literal count, want %d bytes, but only %d is available", litLen, len(s.literals))
		}
		if litLen+matchLen+len(s.out) > maxBlockSize {
			return fmt.Errorf("output (%d) bigger than max block size", litLen+matchLen+len(s.out))
		}
		if matchLen > maxMatchLen {
			return fmt.Errorf("match len (%d) bigger than max allowed length", matchLen)
		}
		if matchOff > len(s.out)+len(hist)+litLen {
			return fmt.Errorf("match offset (%d) bigger than current history (%d)", matchOff, len(s.out)+len(hist)+litLen)
		}
		if matchOff == 0 && matchLen > 0 {
			return fmt.Errorf("zero matchoff and matchlen > 0")
		}

		s.out = append(s.out, s.literals[:litLen]...)
		//fmt.Println("Added literals", hex.EncodeToString(s.literals[:litLen]))
		s.literals = s.literals[litLen:]
		out := s.out

		// Copy from history
		if v := matchOff - len(s.out); v > 0 {
			// v is the start position in history from end.
			start := len(s.hist) - v
			//fmt.Println("Grabbing", matchLen, "bytes from history starting history pos", start)
			if matchLen > v {
				// Some goes into current block.
				// Copy remainder of history
				out = append(out, s.hist[start:]...)
				matchOff -= v
				matchLen -= v
				//fmt.Println("partial grab", matchLen, "left at offset", matchOff, hex.EncodeToString(s.hist[start:]))
			} else {
				//fmt.Println("full grab", hex.EncodeToString(s.hist[start:start+matchLen]))
				out = append(out, s.hist[start:start+matchLen]...)
				matchLen = 0
			}
		}
		// We must be in current buffer now
		if matchLen > 0 {
			start := len(s.out) - matchOff
			//fmt.Println("Copying", matchLen, "bytes from buffer at offset", start, "Size before:", len(s.out))
			if matchLen <= len(s.out)-start {
				// No overlap
				out = append(out, s.out[start:start+matchLen]...)
				//fmt.Println("added", hex.EncodeToString(s.out[start:start+matchLen]))
			} else {
				// Overlapping copy
				// Create destination
				// FIXME: Should be done by extending slice.
				//out = append(out, make([]byte, matchLen)...)
				out = out[:len(out)+matchLen]
				//d := len(out) - len(s.out)
				//fmt.Println("Overlapping. len(out):", len(out), "dst:", len(out)-matchLen, "start:", start)
				src := out[start : start+matchLen]
				// Destination is the space we just added.
				dst := out[len(out)-matchLen:]
				if debug && len(dst) != len(src) {
					return fmt.Errorf("SIZE: %d != %d", len(dst), len(src))
				}
				dst = dst[:len(src)]
				for i := range src {
					dst[i] = src[i]
				}
				//fmt.Println("Added", hex.EncodeToString(dst))
			}
		}
		s.out = out
		if i == 0 {
			break
		}
		s.updateAlt(br)
	}

	// Add final literals
	s.out = append(s.out, s.literals...)
	//fmt.Println("Added literals", hex.EncodeToString(s.literals))
	return nil
}

// update states, at least 27 bits must be available.
func (s *sequenceDecoders) update(br *bitReader) {
	// Max 8 bits
	s.litLengths.state.next(br)
	// Max 9 bits
	s.matchLengths.state.next(br)
	// Max 8 bits
	s.offsets.state.next(br)
}

var bitMask [16]uint16

func init() {
	for i := range bitMask[:] {
		bitMask[i] = uint16((1 << uint(i)) - 1)
	}
}

// update states, at least 27 bits must be available.
func (s *sequenceDecoders) updateAlt(br *bitReader) {
	// Update all 3 states at once. Approx 20% faster.
	a, b, c := s.litLengths.state.dt[s.litLengths.state.state], s.matchLengths.state.dt[s.matchLengths.state.state], s.offsets.state.dt[s.offsets.state.state]

	nBits := a.nbBits + b.nbBits + c.nbBits
	if nBits == 0 {
		s.litLengths.state.state = a.newState
		s.matchLengths.state.state = b.newState
		s.offsets.state.state = c.newState
		return
	}
	bits := br.getBitsFast(nBits)
	lowBits := uint16(bits >> ((c.nbBits + b.nbBits) & 31))
	s.litLengths.state.state = a.newState + lowBits

	lowBits = uint16(bits >> (c.nbBits & 31))
	lowBits &= bitMask[b.nbBits&15]
	s.matchLengths.state.state = b.newState + lowBits

	lowBits = uint16(bits) & bitMask[c.nbBits&15]
	s.offsets.state.state = c.newState + lowBits
}

func (s *sequenceDecoders) nextFast(br *bitReader) (ll, mo, ml int) {
	// Final will not read from stream.
	ll, llB := s.litLengths.state.final()
	ml, mlB := s.matchLengths.state.final()
	mo, moB := s.offsets.state.final()

	// extra bits are stored in reverse order.
	br.fillFast()
	if s.maxBits <= 32 {
		mo += br.getBits(moB)
		ml += br.getBits(mlB)
		ll += br.getBits(llB)
	} else {
		mo += br.getBits(moB)
		br.fillFast()
		// matchlength+literal length, max 32 bits
		ml += br.getBits(mlB)
		ll += br.getBits(llB)
	}

	// mo = s.adjustOffset(mo, ll, moB)
	// Inlined for rather big speedup
	if moB > 1 {
		s.prevOffset[2] = s.prevOffset[1]
		s.prevOffset[1] = s.prevOffset[0]
		s.prevOffset[0] = mo
		return
	}

	if ll == 0 {
		// There is an exception though, when current sequence's literals_length = 0.
		// In this case, repeated offsets are shifted by one, so an offset_value of 1 means Repeated_Offset2,
		// an offset_value of 2 means Repeated_Offset3, and an offset_value of 3 means Repeated_Offset1 - 1_byte.
		mo++
	}

	if mo == 0 {
		mo = s.prevOffset[0]
		return
	}
	var temp int
	if mo == 3 {
		temp = s.prevOffset[0] - 1
	} else {
		temp = s.prevOffset[mo]
	}

	if temp == 0 {
		// 0 is not valid; input is corrupted; force offset to 1
		fmt.Println("temp was 0")
		temp = 1
	}

	if mo != 1 {
		s.prevOffset[2] = s.prevOffset[1]
	}
	s.prevOffset[1] = s.prevOffset[0]
	s.prevOffset[0] = temp
	mo = temp
	return
}

func (s *sequenceDecoders) next(br *bitReader) (ll, mo, ml int) {
	// Final will not read from stream.
	ll, llB := s.litLengths.state.final()
	ml, mlB := s.matchLengths.state.final()
	mo, moB := s.offsets.state.final()

	// extra bits are stored in reverse order.
	br.fill()
	if s.maxBits <= 32 {
		mo += br.getBits(moB)
		ml += br.getBits(mlB)
		ll += br.getBits(llB)
	} else {
		mo += br.getBits(moB)
		br.fill()
		ml += br.getBits(mlB)
		br.fill()
		ll += br.getBits(llB)

	}
	mo = s.adjustOffset(mo, ll, moB)
	return
}

func (s *sequenceDecoders) adjustOffset(offset, litLen int, offsetB uint8) int {
	if offsetB > 1 {
		s.prevOffset[2] = s.prevOffset[1]
		s.prevOffset[1] = s.prevOffset[0]
		s.prevOffset[0] = offset
		return offset
	}

	if litLen == 0 {
		// There is an exception though, when current sequence's literals_length = 0.
		// In this case, repeated offsets are shifted by one, so an offset_value of 1 means Repeated_Offset2,
		// an offset_value of 2 means Repeated_Offset3, and an offset_value of 3 means Repeated_Offset1 - 1_byte.
		offset++
	}

	if offset == 0 {
		return s.prevOffset[0]
	}
	var temp int
	if offset == 3 {
		temp = s.prevOffset[0] - 1
	} else {
		temp = s.prevOffset[offset]
	}

	if temp == 0 {
		// 0 is not valid; input is corrupted; force offset to 1
		fmt.Println("temp was 0")
		temp = 1
	}

	if offset != 1 {
		s.prevOffset[2] = s.prevOffset[1]
	}
	s.prevOffset[1] = s.prevOffset[0]
	s.prevOffset[0] = temp
	return temp
}

// mergeHistory will merge history.
func (s *sequenceDecoders) mergeHistory(hist *sequenceDecoders) (*sequenceDecoders, error) {
	for i := uint(0); i < 3; i++ {
		var sNew, sHist *sequenceDecoder
		switch i {
		case 0:
			sNew = &s.litLengths
			sHist = &hist.litLengths
		case 1:
			sNew = &s.offsets
			sHist = &hist.offsets
		case 2:
			sNew = &s.matchLengths
			sHist = &hist.matchLengths
		}
		if sNew.repeat {
			if sHist.fse == nil {
				return nil, fmt.Errorf("sequence stream %d, repeat requested, but no history", i)
			}
			continue
		}
		if sNew.fse == nil {
			return nil, fmt.Errorf("sequence stream %d, no fse found", i)
		}
		if sHist.fse != nil && !sHist.fse.preDefined {
			fseDecoderPool.Put(sHist.fse)
		}
		sHist.fse = sNew.fse
	}
	return hist, nil
}

type seq struct {
	literals    uint32
	matchLen    uint32
	matchOffset uint32
}

type seqCompMode uint8

const (
	compModePredefined seqCompMode = iota
	compModeRLE
	compModeFSE
	compModeRepeat
)
