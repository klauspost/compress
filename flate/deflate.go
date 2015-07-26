// Copyright 2009 The Go Authors. All rights reserved.
// Copyright (c) 2015 Klaus Post
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package flate

import (
	"bytes"
	"fmt"
	"github.com/klauspost/match"
	"io"
	"math"
)

const (
	NoCompression      = 0
	BestSpeed          = 1
	fastCompression    = 3
	BestCompression    = 9
	DefaultCompression = -1
	logWindowSize      = 15
	windowSize         = 1 << logWindowSize
	windowMask         = windowSize - 1
	logMaxOffsetSize   = 15  // Standard DEFLATE
	minMatchLength     = 4   // The smallest match that the compressor looks for
	maxMatchLength     = 258 // The longest match for the compressor
	minOffsetSize      = 1   // The shortest offset that makes any sense

	// The maximum number of tokens we put into a single flat block, just too
	// stop things from getting too large.
	maxFlateBlockTokens = 1 << 14
	maxStoreBlockSize   = 65535
	hashBits            = 17 // After 17 performance degrades
	hashSize            = 1 << hashBits
	hashMask            = (1 << hashBits) - 1
	hashShift           = (hashBits + minMatchLength - 1) / minMatchLength
	maxHashOffset       = 1 << 24

	skipNever = math.MaxInt32
)

var useSSE42 bool

type compressionLevel struct {
	good, lazy, nice, chain, fastSkipHashing int
	level                                    uint
}

var levels = []compressionLevel{
	{}, // 0
	// For levels 1-3 we don't bother trying with lazy matches
	{4, 0, 8, 4, 4, 1},
	{4, 0, 16, 8, 5, 2},
	{4, 0, 32, 32, 6, 3},
	// Levels 4-9 use increasingly more lazy matching
	// and increasingly stringent conditions for "good enough".
	{4, 4, 16, 16, skipNever, 4},
	{8, 16, 32, 32, skipNever, 5},
	{8, 16, 128, 128, skipNever, 6},
	{8, 32, 128, 256, skipNever, 7},
	{32, 128, 258, 1024, skipNever, 8},
	{32, 258, 258, 4096, skipNever, 9},
}

type compressor struct {
	compressionLevel

	w          *huffmanBitWriter
	hasher     func([]byte) hash
	bulkHasher func([]byte, []hash)

	// compression algorithm
	fill func(*compressor, []byte) int // copy data to window
	step func(*compressor)             // process window
	sync bool                          // requesting flush

	// Input hash chains
	// hashHead[hashValue] contains the largest inputIndex with the specified hash value
	// If hashHead[hashValue] is within the current window, then
	// hashPrev[hashHead[hashValue] & windowMask] contains the previous index
	// with the same hash value.
	chainHead  int
	hashHead   []int
	hashPrev   []int
	hashOffset int

	// input window: unprocessed data is window[index:windowEnd]
	index         int
	window        []byte
	windowEnd     int
	blockStart    int  // window index where current tokens start
	byteAvailable bool // if true, still need to process window[index-1].

	// queued output tokens
	tokens []token

	// deflate state
	length         int
	offset         int
	hash           hash
	maxInsertIndex int
	err            error

	hashMatch [maxMatchLength + minMatchLength]hash
}

type hash int32

func (d *compressor) fillDeflate(b []byte) int {
	if d.index >= 2*windowSize-(minMatchLength+maxMatchLength) {
		// shift the window by windowSize
		copy(d.window, d.window[windowSize:2*windowSize])
		d.index -= windowSize
		d.windowEnd -= windowSize
		if d.blockStart >= windowSize {
			d.blockStart -= windowSize
		} else {
			d.blockStart = math.MaxInt32
		}
		d.hashOffset += windowSize
		if d.hashOffset > maxHashOffset {
			delta := d.hashOffset - 1
			d.hashOffset -= delta
			d.chainHead -= delta
			for i, v := range d.hashPrev {
				if v > delta {
					d.hashPrev[i] -= delta
				} else {
					d.hashPrev[i] = 0
				}
			}
			for i, v := range d.hashHead {
				if v > delta {
					d.hashHead[i] -= delta
				} else {
					d.hashHead[i] = 0
				}
			}
		}
	}
	n := copy(d.window[d.windowEnd:], b)
	d.windowEnd += n
	return n
}

func (d *compressor) fillDeflateBrute(b []byte) int {
	if d.index >= 2*windowSize-(8+maxMatchLength) {
		// shift the window by windowSize
		copy(d.window, d.window[windowSize:2*windowSize])
		d.index -= windowSize
		d.windowEnd -= windowSize
		if d.blockStart >= windowSize {
			d.blockStart -= windowSize
		} else {
			d.blockStart = math.MaxInt32
		}
	}
	n := copy(d.window[d.windowEnd:], b)
	d.windowEnd += n
	return n
}

func (d *compressor) writeBlock(tokens []token, index int, eof bool) error {
	if index > 0 || eof {
		var window []byte
		if d.blockStart <= index {
			window = d.window[d.blockStart:index]
		}
		d.blockStart = index
		d.w.writeBlock(tokens, eof, window)
		return d.w.err
	}
	return nil
}

// fillWindow will fill the current window with the supplied
// dictionary and calculate all hashes.
// This is much faster than doing a full encode.
// Should only be used after a start/reset.
func (d *compressor) fillWindow(b []byte) {
	// Any better way of finding if we are storing?
	if d.compressionLevel.good == 0 {
		return
	}
	// If we are given too much, cut it.
	if len(b) > windowSize {
		b = b[len(b)-windowSize:]
	}
	// Add all to window.
	n := copy(d.window[d.windowEnd:], b)

	// Calculate 256 hashes at the time (more L1 cache hits)
	loops := (n + 256 - minMatchLength) / 256
	for j := 0; j < loops; j++ {
		startindex := j * 256
		end := startindex + 256 + minMatchLength - 1
		if end > n {
			end = n
		}
		tocheck := d.window[startindex:end]
		dstSize := len(tocheck) - minMatchLength + 1

		if dstSize <= 0 {
			continue
		}

		dst := d.hashMatch[:dstSize]
		d.bulkHasher(tocheck, dst)
		var newH hash
		for i, val := range dst {
			di := i + startindex
			newH = val & hashMask
			// Get previous value with the same hash.
			// Our chain should point to the previous value.
			d.hashPrev[di&windowMask] = d.hashHead[newH]
			// Set the head of the hash chain to us.
			d.hashHead[newH] = di + d.hashOffset
		}
		d.hash = newH
	}
	// Update window information.
	d.windowEnd += n
	d.index = n
}

// Try to find a match starting at index whose length is greater than prevSize.
// We only look at chainCount possibilities before giving up.
// pos = d.index, prevHead = d.chainHead-d.hashOffset, prevLength=minMatchLength-1, lookahead
func (d *compressor) findMatch(pos int, prevHead int, prevLength int, lookahead int) (length, offset int, ok bool) {
	minMatchLook := maxMatchLength
	if lookahead < minMatchLook {
		minMatchLook = lookahead
	}

	win := d.window[0 : pos+minMatchLook]

	// We quit when we get a match that's at least nice long
	nice := len(win) - pos
	if d.nice < nice {
		nice = d.nice
	}

	// If we've got a match that's good enough, only look in 1/4 the chain.
	tries := d.chain
	length = prevLength
	if length >= d.good {
		tries >>= 2
	}

	wEnd := win[pos+length]
	wPos := win[pos:]
	minIndex := pos - windowSize

	for i := prevHead; tries > 0; tries-- {
		if wEnd == win[i+length] {
			n := match.MatchLen(win[i:], wPos, len(win)-pos)

			if n > length && (n > minMatchLength || pos-i <= 4096) {
				length = n
				offset = pos - i
				ok = true
				if n >= nice {
					// The match is good enough that we don't try to find a better one.
					break
				}
				wEnd = win[pos+n]
			}
		}
		if i == minIndex {
			// hashPrev[i & windowMask] has already been overwritten, so stop now.
			break
		}
		i = d.hashPrev[i&windowMask] - d.hashOffset
		if i < minIndex || i < 0 {
			break
		}
	}
	return
}

func (d *compressor) writeStoredBlock(buf []byte) error {
	if d.w.writeStoredHeader(len(buf), false); d.w.err != nil {
		return d.w.err
	}
	d.w.writeBytes(buf)
	return d.w.err
}

func oldHash(b []byte) hash {
	return hash(b[0])<<(hashShift*3) + hash(b[1])<<(hashShift*2) + hash(b[2])<<hashShift + hash(b[3])
}

func oldBulkHash(b []byte, dst []hash) {
	if len(b) < minMatchLength {
		return
	}
	h := oldHash(b)
	dst[0] = h
	i := 1
	end := len(b) - minMatchLength + 1
	for ; i < end; i++ {
		h = (h << hashShift) + hash(b[i+3])
		dst[i] = h
	}
}
func (d *compressor) initDeflate() {
	d.hashHead = make([]int, hashSize)
	d.hashPrev = make([]int, windowSize)
	d.window = make([]byte, 2*windowSize)
	d.hashOffset = 1
	d.tokens = make([]token, 0, maxFlateBlockTokens+1)
	d.length = minMatchLength - 1
	d.offset = 0
	d.byteAvailable = false
	d.index = 0
	d.hash = 0
	d.chainHead = -1
	d.hasher = oldHash
	d.bulkHasher = oldBulkHash
	if useSSE42 {
		d.hasher = crc32sse
		d.bulkHasher = crc32sseAll
	}
}

func (d *compressor) deflate() {
	if d.windowEnd-d.index < minMatchLength+maxMatchLength && !d.sync {
		fmt.Println("return early")
		return
	}

	d.maxInsertIndex = d.windowEnd - (minMatchLength - 1)
	if d.index < d.maxInsertIndex {
		//d.hash = int(d.window[d.index])<<hashShift + int(d.window[d.index+1])
		d.hash = d.hasher(d.window[d.index:d.index+minMatchLength]) & hashMask
	}

Loop:
	for {
		if d.index > d.windowEnd {
			panic("index > windowEnd")
		}
		lookahead := d.windowEnd - d.index
		if lookahead < minMatchLength+maxMatchLength {
			if !d.sync {
				break Loop
			}
			if d.index > d.windowEnd {
				panic("index > windowEnd")
			}
			if lookahead == 0 {
				// Flush current output block if any.
				if d.byteAvailable {
					// There is still one pending token that needs to be flushed
					d.tokens = append(d.tokens, literalToken(uint32(d.window[d.index-1])))
					d.byteAvailable = false
				}
				if len(d.tokens) > 0 {
					if d.err = d.writeBlock(d.tokens, d.index, false); d.err != nil {
						return
					}
					d.tokens = d.tokens[:0]
				}
				break Loop
			}
		}
		if d.index < d.maxInsertIndex {
			// Update the hash
			//d.hash = (d.hash<<hashShift + int(d.window[d.index+2])) & hashMask
			d.hash = d.hasher(d.window[d.index:d.index+minMatchLength]) & hashMask
			d.chainHead = d.hashHead[d.hash]
			d.hashPrev[d.index&windowMask] = d.chainHead
			d.hashHead[d.hash] = d.index + d.hashOffset
		}
		prevLength := d.length
		prevOffset := d.offset
		d.length = minMatchLength - 1
		d.offset = 0
		minIndex := d.index - windowSize
		if minIndex < 0 {
			minIndex = 0
		}

		if d.chainHead-d.hashOffset >= minIndex &&
			(d.fastSkipHashing != skipNever && lookahead > minMatchLength-1 ||
				d.fastSkipHashing == skipNever && lookahead > prevLength && prevLength < d.lazy) {
			if newLength, newOffset, ok := d.findMatch(d.index, d.chainHead-d.hashOffset, minMatchLength-1, lookahead); ok {
				d.length = newLength
				d.offset = newOffset
			}
		}
		if d.fastSkipHashing != skipNever && d.length >= minMatchLength ||
			d.fastSkipHashing == skipNever && prevLength >= minMatchLength && d.length <= prevLength {
			// There was a match at the previous step, and the current match is
			// not better. Output the previous match.
			if d.fastSkipHashing != skipNever {
				// "d.length-3" should NOT be "d.length-minMatchLength", since the format always assume 3
				d.tokens = append(d.tokens, matchToken(uint32(d.length-3), uint32(d.offset-minOffsetSize)))
			} else {
				d.tokens = append(d.tokens, matchToken(uint32(prevLength-3), uint32(prevOffset-minOffsetSize)))
			}
			// Insert in the hash table all strings up to the end of the match.
			// index and index-1 are already inserted. If there is not enough
			// lookahead, the last two strings are not inserted into the hash
			// table.
			if d.length <= d.fastSkipHashing {
				var newIndex int
				if d.fastSkipHashing != skipNever {
					newIndex = d.index + d.length
				} else {
					newIndex = d.index + prevLength - 1
				}
				// Calculate missing hashes
				end := newIndex
				if end > d.maxInsertIndex {
					end = d.maxInsertIndex
				}
				end += minMatchLength - 1
				startindex := d.index + 1
				if startindex > d.maxInsertIndex {
					startindex = d.maxInsertIndex
				}
				tocheck := d.window[startindex:end]
				dstSize := len(tocheck) - minMatchLength + 1
				if dstSize > 0 {
					dst := d.hashMatch[:dstSize]
					d.bulkHasher(tocheck, dst)
					var newH hash
					for i, val := range dst {
						di := i + startindex
						newH = val & hashMask
						// Get previous value with the same hash.
						// Our chain should point to the previous value.
						d.hashPrev[di&windowMask] = d.hashHead[newH]
						// Set the head of the hash chain to us.
						d.hashHead[newH] = di + d.hashOffset
					}
					d.hash = newH
				}

				d.index = newIndex

				if d.fastSkipHashing == skipNever {
					d.byteAvailable = false
					d.length = minMatchLength - 1
				}
			} else {
				// For matches this long, we don't bother inserting each individual
				// item into the table.
				d.index += d.length
				if d.index < d.maxInsertIndex {
					d.hash = d.hasher(d.window[d.index:d.index+minMatchLength]) & hashMask
					//d.hash = (int(d.window[d.index])<<hashShift + int(d.window[d.index+1]))
				}
			}
			if len(d.tokens) == maxFlateBlockTokens {
				// The block includes the current character
				if d.err = d.writeBlock(d.tokens, d.index, false); d.err != nil {
					return
				}
				d.tokens = d.tokens[:0]
			}
		} else {
			if d.fastSkipHashing != skipNever || d.byteAvailable {
				i := d.index - 1
				if d.fastSkipHashing != skipNever {
					i = d.index
				}
				d.tokens = append(d.tokens, literalToken(uint32(d.window[i])))
				if len(d.tokens) == maxFlateBlockTokens {
					if d.err = d.writeBlock(d.tokens, i+1, false); d.err != nil {
						return
					}
					d.tokens = d.tokens[:0]
				}
			}
			d.index++
			if d.fastSkipHashing == skipNever {
				d.byteAvailable = true
			}
		}
	}
}

func (d *compressor) deflateBrute() {
	if d.windowEnd-d.index < minMatchLength+maxMatchLength && !d.sync {
		return
	}

	d.maxInsertIndex = d.windowEnd - (minMatchLength - 1)
	var m4 []int
	var m8 []int
Loop:
	for {
		if d.index > d.windowEnd {
			fmt.Println("panic:", d.index, ">", d.windowEnd)
			panic("index > windowEnd")
		}
		lookahead := d.windowEnd - d.index
		if lookahead < 8+maxMatchLength {
			if !d.sync {
				//fmt.Println("more lookahead")
				break Loop
			}
			if d.index > d.windowEnd {
				fmt.Println("panic:", d.index, "to", d.windowEnd)
				panic("index > windowEnd")
			}
			if lookahead == 0 {
				// Flush current output block if any.
				if len(d.tokens) > 0 {
					if d.err = d.writeBlock(d.tokens, d.index, false); d.err != nil {
						return
					}
					d.tokens = d.tokens[:0]
				}
				break Loop
			}
		}
		doLit := true
		if lookahead > 8 {
			minIndex := d.index - (1 << (d.compressionLevel.level + 6))
			if minIndex < 0 {
				minIndex = 0
			}

			endIndex := d.index + 3
			length := endIndex - minIndex
			length = (length / 16) * 16
			minIndex = endIndex - length

			//fmt.Println("searchin:", minIndex, "to", endIndex, "current", d.index)

			m8, m4 = match.Match8And4(d.window[d.index:d.index+8], d.window[minIndex:endIndex], m8, m4)
			//fmt.Printf("got %#v\n", m8)
			if len(m8) == 0 && len(m4) == 0 {
				doLit = true
			} else if len(m8) == 0 {
				aa := d.window[d.index+4]
				ab := d.window[d.index+5]
				ac := d.window[d.index+6]
				i := len(m4) - 1
				endi := i - 10
				if endi < 0 {
					endi = 0
				}
				maxMatch := 4
				longestI := m4[i]

				for ; i >= endi; i-- {
					base := m4[i] + 4 + minIndex
					if base > d.index+3 {
						continue
					}
					ba := d.window[base]
					bb := d.window[base+1]
					bc := d.window[base+2]
					switch maxMatch {
					case 4:
						if aa == ba {
							maxMatch = 5
							longestI = m4[i]
							if ab == bb {
								maxMatch = 6
								longestI = m4[i]
								if ac == bc {
									maxMatch = 7
									longestI = m4[i]
									break
								}
							}
						}
					case 5:
						if aa == ba && ab == bb {
							maxMatch = 6
							longestI = m4[i]
							if ac == bc {
								maxMatch = 7
								longestI = m4[i]
								break
							}
						}
					case 6:
						if aa == ba && ab == bb && ac == bc {
							maxMatch = 7
							longestI = m4[i]
							break
						}
					}

				}
				longestI += minIndex
				if false {
					a := d.window[d.index : d.index+maxMatch]
					b := d.window[longestI : longestI+maxMatch]
					if bytes.Compare(a, b) != 0 {
						panic(fmt.Sprintf("doesn't match:\n%v\n%v", a, b))
					}
					a = d.window[d.index : d.index+maxMatch+1]
					b = d.window[longestI : longestI+maxMatch+1]
					if bytes.Compare(a, b) == 0 {
						panic(fmt.Sprintf("shouldn't match:\n%v\n%v", a, b))
					}
				}
				if true {
					if d.index-longestI > 32768 || d.index-longestI <= 0 {
						fmt.Println("found 4 len", maxMatch, "match at offset", d.index-longestI, "abs:", longestI, "windowEnd:", d.windowEnd)
						panic("bang")
					}
					d.tokens = append(d.tokens, matchToken(uint32(maxMatch-3), uint32(d.index-longestI-minOffsetSize)))
					d.index += maxMatch

					//fmt.Println("new index:", d.index)
					doLit = false
				}

			} else {
				maxSearch := maxMatchLength - 8
				if d.index+maxSearch >= d.windowEnd-8 {
					maxSearch = d.windowEnd - d.index - 8
				}
				maxMatch := 0
				i := len(m8) - 1
				longestI := m8[i]
				searchfor := d.window[d.index+8 : d.index+8+maxSearch]
				for ; i >= 0; i-- {
					idx := m8[i] + 8 + minIndex
					m := match.MatchLen(searchfor, d.window[idx:idx+maxSearch], maxSearch)
					if m > maxMatch {
						longestI = m8[i]
						maxMatch = m
						if m >= d.compressionLevel.nice {
							break
						}
					}
				}
				longestI += minIndex
				maxMatch += 8
				if false {
					a := d.window[d.index : d.index+maxMatch]
					b := d.window[longestI : longestI+maxMatch]
					if bytes.Compare(a, b) != 0 {
						panic(fmt.Sprintf("doesn't match:\n%v\n%v", a, b))
					}
				}
				if d.index-longestI > 32768 || d.index-longestI <= 0 {
					fmt.Println("found 4 len", maxMatch, "match at offset", d.index-longestI, "abs:", longestI, "windowEnd:", d.windowEnd)
					panic("bang")
				}
				//fmt.Println("found len", maxMatch, "match at offset", d.index-longestI, "abs:", longestI, "windowEnd:", d.windowEnd)
				d.tokens = append(d.tokens, matchToken(uint32(maxMatch-3), uint32(d.index-longestI-minOffsetSize)))
				d.index += maxMatch
				//fmt.Println("new index:", d.index)
				doLit = false
			}
		}
		if doLit {
			//fmt.Println("literal:", d.window[d.index])
			d.tokens = append(d.tokens, literalToken(uint32(d.window[d.index])))
			d.index++
		}
		if len(d.tokens) == maxFlateBlockTokens {
			//fmt.Println("writeblovk:", len(d.tokens))
			if d.err = d.writeBlock(d.tokens, d.index, false); d.err != nil {
				return
			}
			d.tokens = d.tokens[:0]
		}
	}
}

func (d *compressor) fillStore(b []byte) int {
	n := copy(d.window[d.windowEnd:], b)
	d.windowEnd += n
	return n
}

func (d *compressor) store() {
	if d.windowEnd > 0 {
		d.err = d.writeStoredBlock(d.window[:d.windowEnd])
	}
	d.windowEnd = 0
}

func (d *compressor) write(b []byte) (n int, err error) {
	n = len(b)
	b = b[d.fill(d, b):]
	for len(b) > 0 {
		d.step(d)
		b = b[d.fill(d, b):]
	}
	return n, d.err
}

func (d *compressor) syncFlush() error {
	d.sync = true
	d.step(d)
	if d.err == nil {
		d.w.writeStoredHeader(0, false)
		d.w.flush()
		d.err = d.w.err
	}
	d.sync = false
	return d.err
}

func (d *compressor) init(w io.Writer, level int) (err error) {
	d.w = newHuffmanBitWriter(w)

	switch {
	case level == NoCompression:
		d.window = make([]byte, maxStoreBlockSize)
		d.fill = (*compressor).fillStore
		d.step = (*compressor).store
	case level == DefaultCompression:
		level = 6
		fallthrough
	case 1 <= level && level <= 9:
		d.compressionLevel = levels[level]
		d.initDeflate()
		d.fill = (*compressor).fillDeflateBrute
		d.step = (*compressor).deflateBrute
	default:
		return fmt.Errorf("flate: invalid compression level %d: want value in range [-1, 9]", level)
	}
	return nil
}

var zeroes [32]int
var bzeroes [256]byte

func (d *compressor) reset(w io.Writer) {
	d.w.reset(w)
	d.sync = false
	d.err = nil
	switch d.compressionLevel.chain {
	case 0:
		// level was NoCompression.
		for i := range d.window {
			d.window[i] = 0
		}
		d.windowEnd = 0
	default:
		d.chainHead = -1
		for s := d.hashHead; len(s) > 0; {
			n := copy(s, zeroes[:])
			s = s[n:]
		}
		for s := d.hashPrev; len(s) > 0; s = s[len(zeroes):] {
			copy(s, zeroes[:])
		}
		d.hashOffset = 1

		d.index, d.windowEnd = 0, 0
		for s := d.window; len(s) > 0; {
			n := copy(s, bzeroes[:])
			s = s[n:]
		}
		d.blockStart, d.byteAvailable = 0, false

		d.tokens = d.tokens[:maxFlateBlockTokens+1]
		for i := 0; i <= maxFlateBlockTokens; i++ {
			d.tokens[i] = 0
		}
		d.tokens = d.tokens[:0]
		d.length = minMatchLength - 1
		d.offset = 0
		d.hash = 0
		d.maxInsertIndex = 0
	}
}

func (d *compressor) close() error {
	d.sync = true
	d.step(d)
	if d.err != nil {
		return d.err
	}
	if d.w.writeStoredHeader(0, true); d.w.err != nil {
		return d.w.err
	}
	d.w.flush()
	return d.w.err
}

// NewWriter returns a new Writer compressing data at the given level.
// Following zlib, levels range from 1 (BestSpeed) to 9 (BestCompression);
// higher levels typically run slower but compress more. Level 0
// (NoCompression) does not attempt any compression; it only adds the
// necessary DEFLATE framing. Level -1 (DefaultCompression) uses the default
// compression level.
//
// If level is in the range [-1, 9] then the error returned will be nil.
// Otherwise the error returned will be non-nil.
func NewWriter(w io.Writer, level int) (*Writer, error) {
	var dw Writer
	if err := dw.d.init(w, level); err != nil {
		return nil, err
	}
	return &dw, nil
}

// NewWriterDict is like NewWriter but initializes the new
// Writer with a preset dictionary.  The returned Writer behaves
// as if the dictionary had been written to it without producing
// any compressed output.  The compressed data written to w
// can only be decompressed by a Reader initialized with the
// same dictionary.
func NewWriterDict(w io.Writer, level int, dict []byte) (*Writer, error) {
	dw := &dictWriter{w}
	zw, err := NewWriter(dw, level)
	if err != nil {
		return nil, err
	}
	zw.d.fillWindow(dict)
	zw.dict = append(zw.dict, dict...) // duplicate dictionary for Reset method.
	return zw, err
}

type dictWriter struct {
	w io.Writer
}

func (w *dictWriter) Write(b []byte) (n int, err error) {
	return w.w.Write(b)
}

// A Writer takes data written to it and writes the compressed
// form of that data to an underlying writer (see NewWriter).
type Writer struct {
	d    compressor
	dict []byte
}

// Write writes data to w, which will eventually write the
// compressed form of data to its underlying writer.
func (w *Writer) Write(data []byte) (n int, err error) {
	return w.d.write(data)
}

// Flush flushes any pending compressed data to the underlying writer.
// It is useful mainly in compressed network protocols, to ensure that
// a remote reader has enough data to reconstruct a packet.
// Flush does not return until the data has been written.
// If the underlying writer returns an error, Flush returns that error.
//
// In the terminology of the zlib library, Flush is equivalent to Z_SYNC_FLUSH.
func (w *Writer) Flush() error {
	// For more about flushing:
	// http://www.bolet.org/~pornin/deflate-flush.html
	return w.d.syncFlush()
}

// Close flushes and closes the writer.
func (w *Writer) Close() error {
	return w.d.close()
}

// Reset discards the writer's state and makes it equivalent to
// the result of NewWriter or NewWriterDict called with dst
// and w's level and dictionary.
func (w *Writer) Reset(dst io.Writer) {
	if dw, ok := w.d.w.w.(*dictWriter); ok {
		// w was created with NewWriterDict
		dw.w = dst
		w.d.reset(dw)
		w.d.fillWindow(w.dict)
	} else {
		// w was created with NewWriter
		w.d.reset(dst)
	}
}
