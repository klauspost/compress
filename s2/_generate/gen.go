package main

//go:generate go run gen.go -out ../encodeblock_amd64.s -stubs ../encodeblock_amd64.go -pkg=s2
//go:generate gofmt -w ../encodeblock_amd64.go
//go:generate go run cleanup.go ../encodeblock_amd64.s

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"strings"

	. "github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/buildtags"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

// insert extra checks here and there.
const debug = false

const (
	limit14B = math.MaxUint32
	// Use 12 bit table when no more than...
	limit12B = 16<<10 - 1
	// Use 10 bit table when no more than...
	limit10B = 4<<10 - 1
	// Use 8 bit table when no more than...
	limit8B = 512 - 1
)

func main() {
	flag.Parse()
	Constraint(buildtags.Not("appengine").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())
	Constraint(buildtags.Term("gc").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())

	// We need a function to add comments.
	TEXT("_dummy_", 0, "func()")
	Comment("#ifdef GOAMD64_v4")
	Comment("#ifndef GOAMD64_v3")
	Comment("#define GOAMD64_v3")
	Comment("#endif")
	Comment("#endif")
	RET()

	o := options{
		bmi1:         false,
		bmi2:         false,
		snappy:       false,
		avx2:         false,
		outputMargin: 9,
	}
	o.genEncodeBlockAsm("encodeBlockAsm", 14, 6, 6, limit14B)
	o.genEncodeBlockAsm("encodeBlockAsm4MB", 14, 6, 6, 4<<20)
	o.genEncodeBlockAsm("encodeBlockAsm12B", 12, 5, 5, limit12B)
	o.genEncodeBlockAsm("encodeBlockAsm10B", 10, 5, 4, limit10B)
	o.genEncodeBlockAsm("encodeBlockAsm8B", 8, 4, 4, limit8B)

	o.outputMargin = 6
	o.maxSkip = 100 // Blocks can be long, limit max skipping.
	o.genEncodeBetterBlockAsm("encodeBetterBlockAsm", 17, 14, 7, 7, limit14B)
	o.genEncodeBetterBlockAsm("encodeBetterBlockAsm4MB", 17, 14, 7, 7, 4<<20)
	o.maxSkip = 0
	o.genEncodeBetterBlockAsm("encodeBetterBlockAsm12B", 14, 12, 6, 6, limit12B)
	o.genEncodeBetterBlockAsm("encodeBetterBlockAsm10B", 12, 10, 5, 6, limit10B)
	o.genEncodeBetterBlockAsm("encodeBetterBlockAsm8B", 10, 8, 4, 6, limit8B)

	// Snappy compatible
	o.snappy = true
	o.outputMargin = 9
	o.genEncodeBlockAsm("encodeSnappyBlockAsm", 14, 6, 6, limit14B)
	o.genEncodeBlockAsm("encodeSnappyBlockAsm64K", 14, 6, 6, 64<<10-1)
	o.genEncodeBlockAsm("encodeSnappyBlockAsm12B", 12, 5, 5, limit12B)
	o.genEncodeBlockAsm("encodeSnappyBlockAsm10B", 10, 5, 4, limit10B)
	o.genEncodeBlockAsm("encodeSnappyBlockAsm8B", 8, 4, 4, limit8B)

	o.maxSkip = 100
	o.genEncodeBetterBlockAsm("encodeSnappyBetterBlockAsm", 17, 14, 7, 7, limit14B)
	o.maxSkip = 0
	o.genEncodeBetterBlockAsm("encodeSnappyBetterBlockAsm64K", 16, 14, 7, 7, 64<<10-1)
	o.genEncodeBetterBlockAsm("encodeSnappyBetterBlockAsm12B", 14, 12, 6, 6, limit12B)
	o.genEncodeBetterBlockAsm("encodeSnappyBetterBlockAsm10B", 12, 10, 5, 6, limit10B)
	o.genEncodeBetterBlockAsm("encodeSnappyBetterBlockAsm8B", 10, 8, 4, 6, limit8B)

	o.maxSkip = 0
	o.skipOutput = true
	o.snappy = true
	o.genEncodeBlockAsm("calcBlockSize", 13, 5, 6, limit14B)
	o.genEncodeBlockAsm("calcBlockSizeSmall", 9, 4, 4, 1024)
	o.skipOutput = false
	o.snappy = false

	o.snappy = false
	o.outputMargin = 0
	o.maxLen = math.MaxUint32
	o.maxOffset = math.MaxUint32 - 1
	o.genEmitLiteral()
	o.genEmitRepeat()
	o.genEmitCopy()
	o.snappy = true
	o.genEmitCopyNoRepeat()
	o.snappy = false
	o.genMatchLen()
	o.cvtLZ4BlockAsm(false)
	o.cvtLZ4BlockAsm(true)
	o.snappy = true
	o.cvtLZ4BlockAsm(false)
	o.cvtLZ4BlockAsm(true)

	Generate()
}

func debugval(v Op) {
	value := reg.R15
	MOVQ(v, value)
	INT(Imm(3))
}

func debugval32(v Op) {
	value := reg.R15L
	MOVL(v, value)
	INT(Imm(3))
}

var assertCounter int

// assert will insert code if debug is enabled.
// The code should jump to 'ok' is assertion is success.
func assert(fn func(ok LabelRef)) {
	if debug {
		caller := [100]uintptr{0}
		runtime.Callers(2, caller[:])
		frame, _ := runtime.CallersFrames(caller[:]).Next()

		ok := fmt.Sprintf("assert_check_%d_ok_srcline_%d", assertCounter, frame.Line)
		fn(LabelRef(ok))
		// Emit several since delve is imprecise.
		INT(Imm(3))
		INT(Imm(3))
		Label(ok)
		assertCounter++
	}
}

type options struct {
	snappy       bool
	bmi1         bool
	bmi2         bool
	skipOutput   bool
	avx2         bool
	maxLen       int
	maxOffset    int
	outputMargin int // Should be at least 5.
	maxSkip      int
}

func (o options) genEncodeBlockAsm(name string, tableBits, skipLog, hashBytes, maxLen int) {
	dstTxt := "dst, "
	if o.skipOutput {
		dstTxt = ""
	}
	TEXT(name, 0, "func("+dstTxt+"src []byte) int")
	Doc(name+" encodes a non-empty src to a guaranteed-large-enough dst.",
		fmt.Sprintf("Maximum input %d bytes.", maxLen),
		"It assumes that the varint-encoded length of the decompressed bytes has already been written.", "")
	Pragma("noescape")

	o.maxLen = maxLen
	o.maxOffset = maxLen - 1
	var literalMaxOverhead = maxLitOverheadFor(maxLen)

	var tableSize = 4 * (1 << tableBits)
	// Memzero needs at least 128 bytes.
	if tableSize < 128 {
		panic("tableSize must be at least 128 bytes")
	}

	lenSrcBasic, err := Param("src").Len().Resolve()
	if err != nil {
		panic(err)
	}
	lenSrcQ := lenSrcBasic.Addr

	var lenDstQ Mem
	if debug && !o.skipOutput {
		lenDstBasic, err := Param("dst").Len().Resolve()
		if err != nil {
			panic(err)
		}
		lenDstQ = lenDstBasic.Addr
	}

	// Bail if we can't compress to at least this.
	dstLimitPtrQ := AllocLocal(8)

	// sLimitL is when to stop looking for offset/length copies.
	sLimitL := AllocLocal(4)

	// nextEmitL keeps track of the point we have emitted to.
	nextEmitL := AllocLocal(4)

	// Repeat stores the last match offset.
	repeatL := AllocLocal(4)

	// nextSTempL keeps nextS while other functions are being called.
	nextSTempL := AllocLocal(4)

	// Alloc table last
	table := AllocLocal(tableSize)

	dst := GP64()
	if !o.skipOutput {
		dstBaseBasic, err := Param("dst").Base().Resolve()
		if err != nil {
			panic(err)
		}
		dstBaseQ := dstBaseBasic.Addr
		MOVQ(dstBaseQ, dst)
	} else {
		// Zero dst address
		XORQ(dst, dst)
	}

	srcBaseBasic, err := Param("src").Base().Resolve()
	if err != nil {
		panic(err)
	}
	srcBaseQ := srcBaseBasic.Addr

	// Zero table
	{
		iReg := GP64()
		MOVQ(U32(tableSize/8/16), iReg)
		tablePtr := GP64()
		LEAQ(table, tablePtr)
		zeroXmm := XMM()
		PXOR(zeroXmm, zeroXmm)

		Label("zero_loop_" + name)
		for i := 0; i < 8; i++ {
			MOVOU(zeroXmm, Mem{Base: tablePtr, Disp: i * 16})
		}
		ADDQ(U8(16*8), tablePtr)
		DECQ(iReg)
		JNZ(LabelRef("zero_loop_" + name))
	}

	{
		// nextEmit is offset n src where the next emitLiteral should start from.
		MOVL(U32(0), nextEmitL)

		const inputMargin = 8
		tmp, tmp2, tmp3 := GP64(), GP64(), GP64()
		MOVQ(lenSrcQ, tmp)
		LEAQ(Mem{Base: tmp, Disp: -o.outputMargin}, tmp2)
		// sLimitL := len(src) - inputMargin
		LEAQ(Mem{Base: tmp, Disp: -inputMargin}, tmp3)

		assert(func(ok LabelRef) {
			CMPQ(tmp3, lenSrcQ)
			JB(ok)
		})

		MOVL(tmp3.As32(), sLimitL)

		// dstLimit := (len(src) - outputMargin ) - len(src)>>5
		SHRQ(U8(5), tmp)
		SUBL(tmp.As32(), tmp2.As32()) // tmp2 = tmp2 - tmp

		assert(func(ok LabelRef) {
			// if len(src) > len(src) - len(src)>>5 - outputMargin: ok
			CMPQ(lenSrcQ, tmp2)
			JAE(ok)
		})

		LEAQ(Mem{Base: dst, Index: tmp2, Scale: 1}, tmp2)
		MOVQ(tmp2, dstLimitPtrQ)
	}

	// s = 1
	s := GP32()
	MOVL(U32(1), s)
	// repeatL = 1
	MOVL(s, repeatL)

	src := GP64()
	Load(Param("src").Base(), src)

	// Load cv
	Label("search_loop_" + name)
	candidate := GP32()
	{
		assert(func(ok LabelRef) {
			// Check if somebody changed src
			tmp := GP64()
			MOVQ(srcBaseQ, tmp)
			CMPQ(tmp, src)
			JEQ(ok)
		})

		cv := GP64()
		nextS := GP32()
		// nextS := s + (s-nextEmit)>>6 + 4
		if o.maxSkip == 0 {
			tmp := GP64()
			MOVL(s, tmp.As32())           // tmp = s
			SUBL(nextEmitL, tmp.As32())   // tmp = s - nextEmit
			SHRL(U8(skipLog), tmp.As32()) // tmp = (s - nextEmit) >> skipLog
			LEAL(Mem{Base: s, Disp: 4, Index: tmp, Scale: 1}, nextS)
		}
		// if nextS > sLimit {goto emitRemainder}
		{
			CMPL(nextS.As32(), sLimitL)
			JAE(LabelRef("emit_remainder_" + name))
		}
		MOVQ(Mem{Base: src, Index: s, Scale: 1}, cv)
		assert(func(ok LabelRef) {
			// Check if s is valid (we should have jumped above if not)
			tmp := GP64()
			MOVQ(lenSrcQ, tmp)
			CMPQ(tmp, s.As64())
			JA(ok)
		})
		// move nextS to stack.
		MOVL(nextS.As32(), nextSTempL)

		candidate2 := GP32()
		hasher := hashN(hashBytes, tableBits)
		{
			hash0, hash1 := GP64(), GP64()
			MOVQ(cv, hash0)
			MOVQ(cv, hash1)
			SHRQ(U8(8), hash1)
			hasher.hash(hash0)
			hasher.hash(hash1)
			MOVL(table.Idx(hash0, 4), candidate)
			MOVL(table.Idx(hash1, 4), candidate2)
			assert(func(ok LabelRef) {
				CMPQ(hash0, U32(tableSize))
				JB(ok)
			})
			assert(func(ok LabelRef) {
				CMPQ(hash1, U32(tableSize))
				JB(ok)
			})

			MOVL(s, table.Idx(hash0, 4))
			tmp := GP32()
			LEAL(Mem{Base: s, Disp: 1}, tmp)
			MOVL(tmp, table.Idx(hash1, 4))
		}

		// Can be moved up if registers are available.
		hash2 := GP64()
		{
			// hash2 := hash6(cv>>16, tableBits)
			// hasher = hash6(tableBits)
			MOVQ(cv, hash2)
			SHRQ(U8(16), hash2)
			hasher.hash(hash2)
			assert(func(ok LabelRef) {
				CMPQ(hash2, U32(tableSize))
				JB(ok)
			})
		}

		// En/disable repeat matching.
		if true {
			// Check repeat at offset checkRep
			const checkRep = 1
			{
				// rep = s - repeat
				rep := GP32()
				MOVL(s, rep)
				SUBL(repeatL, rep) // rep = s - repeat

				// if uint32(cv>>(checkRep*8)) == load32(src, s-repeat+checkRep) {
				left, right := GP64(), GP64()
				MOVL(Mem{Base: src, Index: rep, Disp: checkRep, Scale: 1}, right.As32())
				MOVQ(cv, left)
				SHRQ(U8(checkRep*8), left)
				CMPL(left.As32(), right.As32())
				// BAIL, no repeat.
				JNE(LabelRef("no_repeat_found_" + name))
			}
			// base = s + checkRep
			base := GP32()
			LEAL(Mem{Base: s, Disp: checkRep}, base)

			// nextEmit before repeat.
			nextEmit := GP32()
			MOVL(nextEmitL, nextEmit)

			// Extend back
			if true {
				i := GP32()
				MOVL(base, i)
				SUBL(repeatL, i)
				JZ(LabelRef("repeat_extend_back_end_" + name))

				Label("repeat_extend_back_loop_" + name)
				// if base <= nextemit {exit}
				CMPL(base.As32(), nextEmit)
				JBE(LabelRef("repeat_extend_back_end_" + name))
				// if src[i-1] == src[base-1]
				tmp, tmp2 := GP64(), GP64()
				MOVB(Mem{Base: src, Index: i, Scale: 1, Disp: -1}, tmp.As8())
				MOVB(Mem{Base: src, Index: base, Scale: 1, Disp: -1}, tmp2.As8())
				CMPB(tmp.As8(), tmp2.As8())
				JNE(LabelRef("repeat_extend_back_end_" + name))
				LEAL(Mem{Base: base, Disp: -1}, base)
				DECL(i)
				JNZ(LabelRef("repeat_extend_back_loop_" + name))
			}
			Label("repeat_extend_back_end_" + name)

			// Base is now at start. Emit until base.
			// d += emitLiteral(dst[d:], src[nextEmit:base])
			if true {
				o.emitLiteralsDstP(nextEmitL, base, src, dst, "repeat_emit_"+name)
			}

			// Extend forward
			{
				// s += 4 + checkRep
				ADDL(U8(4+checkRep), s)

				if true {
					// candidate := s - repeat + 4 + checkRep
					MOVL(s, candidate)
					SUBL(repeatL, candidate) // candidate = s - repeat

					// srcLeft = len(src) - s
					srcLeft := GP64()
					MOVQ(lenSrcQ, srcLeft)
					SUBL(s, srcLeft.As32())
					assert(func(ok LabelRef) {
						// if srcleft < maxint32: ok
						CMPQ(srcLeft, U32(0x7fffffff))
						JB(ok)
					})
					// Forward address
					forwardStart := GP64()
					LEAQ(Mem{Base: src, Index: s, Scale: 1}, forwardStart)
					// End address
					backStart := GP64()
					LEAQ(Mem{Base: src, Index: candidate, Scale: 1}, backStart)

					length := o.matchLen("repeat_extend_"+name, forwardStart, backStart, srcLeft, LabelRef("repeat_extend_forward_end_"+name))
					forwardStart, backStart, srcLeft = nil, nil, nil
					Label("repeat_extend_forward_end_" + name)
					// s+= length
					ADDL(length.As32(), s)
				}
			}
			// Emit
			if true {
				// length = s-base
				length := GP32()
				MOVL(s, length)
				SUBL(base.As32(), length) // length = s - base

				offsetVal := GP32()
				MOVL(repeatL, offsetVal)

				if !o.snappy {
					// if nextEmit == 0 {do copy instead...}
					TESTL(nextEmit, nextEmit)
					JZ(LabelRef("repeat_as_copy_" + name))

					// Emit as repeat...
					o.emitRepeat("match_repeat_"+name, length, offsetVal, nil, dst, LabelRef("repeat_end_emit_"+name), false)

					// Emit as copy instead...
					Label("repeat_as_copy_" + name)
				}
				o.emitCopy("repeat_as_copy_"+name, length, offsetVal, nil, dst, nil, LabelRef("repeat_end_emit_"+name))

				Label("repeat_end_emit_" + name)
				// Store new dst and nextEmit
				MOVL(s, nextEmitL)
			}
			// if s >= sLimit is picked up on next loop.
			if false {
				CMPL(s.As32(), sLimitL)
				JAE(LabelRef("emit_remainder_" + name))
			}
			JMP(LabelRef("search_loop_" + name))
		}
		Label("no_repeat_found_" + name)
		{
			// Check candidates are ok. All must be < s and < len(src)
			assert(func(ok LabelRef) {
				tmp := GP64()
				MOVQ(lenSrcQ, tmp)
				CMPL(tmp.As32(), candidate)
				JA(ok)
			})
			assert(func(ok LabelRef) {
				CMPL(s, candidate)
				JA(ok)
			})
			assert(func(ok LabelRef) {
				tmp := GP64()
				MOVQ(lenSrcQ, tmp)
				CMPL(tmp.As32(), candidate2)
				JA(ok)
			})
			assert(func(ok LabelRef) {
				CMPL(s, candidate2)
				JA(ok)
			})

			CMPL(Mem{Base: src, Index: candidate, Scale: 1}, cv.As32())
			JEQ(LabelRef("candidate_match_" + name))

			tmp := GP32()
			// cv >>= 8
			SHRQ(U8(8), cv)

			// candidate = int(table[hash2]) - load early.
			MOVL(table.Idx(hash2, 4), candidate)
			assert(func(ok LabelRef) {
				tmp := GP64()
				MOVQ(lenSrcQ, tmp)
				CMPL(tmp.As32(), candidate)
				JA(ok)
			})
			assert(func(ok LabelRef) {
				// We may get s and s+1
				tmp := GP32()
				LEAL(Mem{Base: s, Disp: 2}, tmp)
				CMPL(tmp, candidate)
				JA(ok)
			})

			LEAL(Mem{Base: s, Disp: 2}, tmp)

			//if uint32(cv>>8) == load32(src, candidate2)
			CMPL(Mem{Base: src, Index: candidate2, Scale: 1}, cv.As32())
			JEQ(LabelRef("candidate2_match_" + name))

			// table[hash2] = uint32(s + 2)
			MOVL(tmp, table.Idx(hash2, 4))

			// cv >>= 8 (>> 16 total)
			SHRQ(U8(8), cv)

			// if uint32(cv>>16) == load32(src, candidate)
			CMPL(Mem{Base: src, Index: candidate, Scale: 1}, cv.As32())
			JEQ(LabelRef("candidate3_match_" + name))

			// No match found, next loop
			// s = nextS
			MOVL(nextSTempL, s)
			JMP(LabelRef("search_loop_" + name))

			// Matches candidate at s + 2 (3rd check)
			Label("candidate3_match_" + name)
			ADDL(U8(2), s)
			JMP(LabelRef("candidate_match_" + name))

			// Match at s + 1 (we calculated the hash, lets store it)
			Label("candidate2_match_" + name)
			// table[hash2] = uint32(s + 2)
			MOVL(tmp, table.Idx(hash2, 4))
			// s++
			INCL(s)
			MOVL(candidate2, candidate)
		}
	}

	Label("candidate_match_" + name)
	// We have a match at 's' with src offset in "candidate" that matches at least 4 bytes.
	// Extend backwards
	if true {
		ne := GP32()
		MOVL(nextEmitL, ne)
		TESTL(candidate, candidate)
		JZ(LabelRef("match_extend_back_end_" + name))

		// candidate is tested when decremented, so we loop back here.
		Label("match_extend_back_loop_" + name)
		// if s <= nextEmit {exit}
		CMPL(s, ne)
		JBE(LabelRef("match_extend_back_end_" + name))
		// if src[candidate-1] == src[s-1]
		tmp, tmp2 := GP64(), GP64()
		MOVB(Mem{Base: src, Index: candidate, Scale: 1, Disp: -1}, tmp.As8())
		MOVB(Mem{Base: src, Index: s, Scale: 1, Disp: -1}, tmp2.As8())
		CMPB(tmp.As8(), tmp2.As8())
		JNE(LabelRef("match_extend_back_end_" + name))
		LEAL(Mem{Base: s, Disp: -1}, s)
		DECL(candidate)
		JZ(LabelRef("match_extend_back_end_" + name))
		JMP(LabelRef("match_extend_back_loop_" + name))
	}
	Label("match_extend_back_end_" + name)

	// Bail if we exceed the maximum size.
	if true {
		// tmp = s-nextEmit
		tmp := GP64()
		MOVL(s, tmp.As32())
		SUBL(nextEmitL, tmp.As32())
		// tmp = &dst + s-nextEmit
		LEAQ(Mem{Base: dst, Index: tmp, Scale: 1, Disp: literalMaxOverhead}, tmp)
		CMPQ(tmp, dstLimitPtrQ)
		JB(LabelRef("match_dst_size_check_" + name))
		ri, err := ReturnIndex(0).Resolve()
		if err != nil {
			panic(err)
		}
		MOVQ(U32(0), ri.Addr)
		if o.avx2 {
			VZEROUPPER()
		}
		RET()
	}
	Label("match_dst_size_check_" + name)
	{
		base := GP32()
		MOVL(s, base.As32())
		o.emitLiteralsDstP(nextEmitL, base, src, dst, "match_emit_"+name)
	}
	cv := GP64()
	Label("match_nolit_loop_" + name)
	{
		// Update repeat
		{
			// repeat = base - candidate
			repeatVal := GP64().As32()
			MOVL(s, repeatVal)
			SUBL(candidate, repeatVal)
			MOVL(repeatVal, repeatL)
		}
		// s+=4, candidate+=4
		ADDL(U8(4), s)
		ADDL(U8(4), candidate)
		// Extend the 4-byte match as long as possible and emit copy.
		{
			assert(func(ok LabelRef) {
				// s must be > candidate cannot be equal.
				CMPL(s, candidate)
				JA(ok)
			})
			// srcLeft = len(src) - s
			srcLeft := GP64()
			MOVQ(lenSrcQ, srcLeft)
			SUBL(s, srcLeft.As32())
			assert(func(ok LabelRef) {
				// if srcleft < maxint32: ok
				CMPQ(srcLeft, U32(0x7fffffff))
				JB(ok)
			})

			a, b := GP64(), GP64()
			LEAQ(Mem{Base: src, Index: s, Scale: 1}, a)
			LEAQ(Mem{Base: src, Index: candidate, Scale: 1}, b)
			length := o.matchLen("match_nolit_"+name,
				a, b,
				srcLeft,
				LabelRef("match_nolit_end_"+name),
			)
			Label("match_nolit_end_" + name)
			assert(func(ok LabelRef) {
				CMPL(length.As32(), U32(math.MaxInt32))
				JB(ok)
			})
			a, b, srcLeft = nil, nil, nil

			// s += length (length is destroyed, use it now)
			ADDL(length.As32(), s)

			// Load offset from repeat value.
			offset := GP64()
			MOVL(repeatL, offset.As32())

			// length += 4
			ADDL(U8(4), length.As32())
			MOVL(s, nextEmitL) // nextEmit = s
			o.emitCopy("match_nolit_"+name, length, offset, nil, dst, nil, LabelRef("match_nolit_emitcopy_end_"+name))
			Label("match_nolit_emitcopy_end_" + name)

			// if s >= sLimit { end }
			{
				CMPL(s.As32(), sLimitL)
				JAE(LabelRef("emit_remainder_" + name))
			}
			// Start load s-2 as early as possible...
			MOVQ(Mem{Base: src, Index: s, Scale: 1, Disp: -2}, cv)
			// Bail if we exceed the maximum size.
			{
				CMPQ(dst, dstLimitPtrQ)
				JB(LabelRef("match_nolit_dst_ok_" + name))
				ri, err := ReturnIndex(0).Resolve()
				if err != nil {
					panic(err)
				}
				MOVQ(U32(0), ri.Addr)
				if o.avx2 {
					VZEROUPPER()
				}
				RET()
				Label("match_nolit_dst_ok_" + name)
			}
		}
		// cv must be set to value at s-2 before arriving here
		{
			// Check for an immediate match, otherwise start search at s+1
			// Index s-2
			hasher := hashN(hashBytes, tableBits)
			hash0, hash1 := GP64(), GP64()
			MOVQ(cv, hash0) // src[s-2]
			SHRQ(U8(16), cv)
			MOVQ(cv, hash1) // src[s]
			hasher.hash(hash0)
			hasher.hash(hash1)
			sm2 := GP32() // s - 2
			LEAL(Mem{Base: s, Disp: -2}, sm2)
			assert(func(ok LabelRef) {
				CMPQ(hash0, U32(tableSize))
				JB(ok)
			})
			assert(func(ok LabelRef) {
				CMPQ(hash1, U32(tableSize))
				JB(ok)
			})
			addr := GP64()
			LEAQ(table.Idx(hash1, 4), addr)
			MOVL(Mem{Base: addr}, candidate)
			MOVL(sm2, table.Idx(hash0, 4))
			MOVL(s, Mem{Base: addr})
			CMPL(Mem{Base: src, Index: candidate, Scale: 1}, cv.As32())
			JEQ(LabelRef("match_nolit_loop_" + name))
			INCL(s)
		}
		JMP(LabelRef("search_loop_" + name))
	}

	Label("emit_remainder_" + name)
	// Bail if we exceed the maximum size.
	// if d+len(src)-nextEmitL > dstLimitPtrQ {	return 0
	{
		// remain = len(src) - nextEmit
		remain := GP64()
		MOVQ(lenSrcQ, remain)
		SUBL(nextEmitL, remain.As32())

		dstExpect := GP64()
		// dst := dst + (len(src)-nextEmitL)

		LEAQ(Mem{Base: dst, Index: remain, Scale: 1, Disp: literalMaxOverhead}, dstExpect)
		CMPQ(dstExpect, dstLimitPtrQ)
		JB(LabelRef("emit_remainder_ok_" + name))
		ri, err := ReturnIndex(0).Resolve()
		if err != nil {
			panic(err)
		}
		if o.avx2 {
			VZEROUPPER()
		}
		MOVQ(U32(0), ri.Addr)
		RET()
		Label("emit_remainder_ok_" + name)
	}
	{
		// emitLiteral(dst[d:], src[nextEmitL:])
		emitEnd := GP64()
		MOVQ(lenSrcQ, emitEnd)

		// Emit final literals.
		// Since we may be at the end of source,
		// we cannot have output margin.
		x := o.outputMargin
		o.outputMargin = 0
		o.emitLiteralsDstP(nextEmitL, emitEnd, src, dst, "emit_remainder_"+name)
		o.outputMargin = x
		// Assert size is < limit
		assert(func(ok LabelRef) {
			// if dstBaseQ <  dstLimitPtrQ: ok
			CMPQ(dst, dstLimitPtrQ)
			JB(ok)
		})
	}

	// length := start - base (ptr arithmetic)
	length := GP64()
	if !o.skipOutput {
		base := Load(Param("dst").Base(), GP64())
		MOVQ(dst, length)
		SUBQ(base, length)
	} else {
		length = dst
	}

	// Assert size is < len(src)
	assert(func(ok LabelRef) {
		// if len(src) >= length: ok
		CMPQ(lenSrcQ, length)
		JAE(ok)
	})
	// Assert size is < len(dst)
	if !o.skipOutput {
		assert(func(ok LabelRef) {
			// if len(dst) >= length: ok
			CMPQ(lenDstQ, length)
			JAE(ok)
		})
	}
	if o.avx2 {
		VZEROUPPER()
	}
	Store(length, ReturnIndex(0))
	RET()
}

func maxLitOverheadFor(n int) int {
	switch {
	case n == 0:
		return 0
	case n < 60:
		return 1
	case n < 1<<8:
		return 2
	case n < 1<<16:
		return 3
	case n < 1<<24:
		return 4
	}
	return 5
}

func (o options) genEncodeBetterBlockAsm(name string, lTableBits, sTableBits, skipLog, lHashBytes, maxLen int) {
	TEXT(name, 0, "func(dst, src []byte) int")
	Doc(name+" encodes a non-empty src to a guaranteed-large-enough dst.",
		fmt.Sprintf("Maximum input %d bytes.", maxLen),
		"It assumes that the varint-encoded length of the decompressed bytes has already been written.", "")
	Pragma("noescape")

	if lHashBytes > 7 || lHashBytes <= 4 {
		panic("lHashBytes must be <= 7 and >4")
	}
	var literalMaxOverhead = maxLitOverheadFor(maxLen)

	const sHashBytes = 4
	o.maxLen = maxLen
	o.maxOffset = maxLen - 1

	var lTableSize = 4 * (1 << lTableBits)
	var sTableSize = 4 * (1 << sTableBits)

	// Memzero needs at least 128 bytes.
	if (lTableSize + sTableSize) < 128 {
		panic("tableSize must be at least 128 bytes")
	}

	lenSrcBasic, err := Param("src").Len().Resolve()
	if err != nil {
		panic(err)
	}
	lenSrcQ := lenSrcBasic.Addr

	lenDstBasic, err := Param("dst").Len().Resolve()
	if err != nil {
		panic(err)
	}
	lenDstQ := lenDstBasic.Addr

	// Bail if we can't compress to at least this.
	dstLimitPtrQ := AllocLocal(8)

	// sLimitL is when to stop looking for offset/length copies.
	sLimitL := AllocLocal(4)

	// nextEmitL keeps track of the point we have emitted to.
	nextEmitL := AllocLocal(4)

	// Repeat stores the last match offset.
	repeatL := AllocLocal(4)

	// nextSTempL keeps nextS while other functions are being called.
	nextSTempL := AllocLocal(4)

	// Alloc table last, lTab must be before sTab.
	lTab := AllocLocal(lTableSize)
	sTab := AllocLocal(sTableSize)

	dst := GP64()
	{
		dstBaseBasic, err := Param("dst").Base().Resolve()
		if err != nil {
			panic(err)
		}
		dstBaseQ := dstBaseBasic.Addr
		MOVQ(dstBaseQ, dst)
	}

	srcBaseBasic, err := Param("src").Base().Resolve()
	if err != nil {
		panic(err)
	}
	srcBaseQ := srcBaseBasic.Addr

	// Zero table
	{
		iReg := GP64()
		MOVQ(U32((sTableSize+lTableSize)/8/16), iReg)
		tablePtr := GP64()
		LEAQ(lTab, tablePtr)
		zeroXmm := XMM()
		PXOR(zeroXmm, zeroXmm)

		Label("zero_loop_" + name)
		for i := 0; i < 8; i++ {
			MOVOU(zeroXmm, Mem{Base: tablePtr, Disp: i * 16})
		}
		ADDQ(U8(16*8), tablePtr)
		DECQ(iReg)
		JNZ(LabelRef("zero_loop_" + name))
	}

	{
		// nextEmit is offset n src where the next emitLiteral should start from.
		MOVL(U32(0), nextEmitL)

		const inputMargin = 8
		tmp, tmp2, tmp3 := GP64(), GP64(), GP64()
		MOVQ(lenSrcQ, tmp)
		LEAQ(Mem{Base: tmp, Disp: -o.outputMargin}, tmp2)
		// sLimitL := len(src) - inputMargin
		LEAQ(Mem{Base: tmp, Disp: -inputMargin}, tmp3)

		assert(func(ok LabelRef) {
			CMPQ(tmp3, lenSrcQ)
			JB(ok)
		})

		MOVL(tmp3.As32(), sLimitL)

		// dstLimit := (len(src) - 5 ) - len(src)>>5
		SHRQ(U8(5), tmp)
		SUBL(tmp.As32(), tmp2.As32()) // tmp2 = tmp2 - tmp

		assert(func(ok LabelRef) {
			// if len(src) > len(src) - len(src)>>5 - 5: ok
			CMPQ(lenSrcQ, tmp2)
			JAE(ok)
		})

		LEAQ(Mem{Base: dst, Index: tmp2, Scale: 1}, tmp2)
		MOVQ(tmp2, dstLimitPtrQ)
	}

	// s = 1
	s := GP32()
	MOVL(U32(1), s)
	// repeatL = 0
	MOVL(U32(0), repeatL)

	src := GP64()
	Load(Param("src").Base(), src)

	// Load cv
	Label("search_loop_" + name)
	candidate := GP32()
	{
		assert(func(ok LabelRef) {
			// Check if somebody changed src
			tmp := GP64()
			MOVQ(srcBaseQ, tmp)
			CMPQ(tmp, src)
			JEQ(ok)
		})

		cv := GP64()
		nextS := GP32()
		// nextS := s + (s-nextEmit)>>skipLog + 1
		if o.maxSkip == 0 {
			tmp := GP64()
			MOVL(s, tmp.As32())           // tmp = s
			SUBL(nextEmitL, tmp.As32())   // tmp = s - nextEmit
			SHRL(U8(skipLog), tmp.As32()) // tmp = (s - nextEmit) >> skipLog
			LEAL(Mem{Base: s, Disp: 1, Index: tmp, Scale: 1}, nextS)
		} else {
			/*
				nextS = (s-nextEmit)>>7 + 1
				if nextS > maxSkip {
					nextS = s + maxSkip
				} else {
					nextS += s
				}
			*/
			tmp := GP64()
			MOVL(s, tmp.As32())           // tmp = s
			SUBL(nextEmitL, tmp.As32())   // tmp = s - nextEmit
			SHRL(U8(skipLog), tmp.As32()) // tmp = (s - nextEmit) >> skipLog
			CMPL(tmp.As32(), U8(o.maxSkip-1))
			JBE(LabelRef("check_maxskip_ok_" + name))
			LEAL(Mem{Base: s, Disp: o.maxSkip, Scale: 1}, nextS)
			JMP(LabelRef("check_maxskip_cont_" + name))

			Label("check_maxskip_ok_" + name)
			LEAL(Mem{Base: s, Disp: 1, Index: tmp, Scale: 1}, nextS)
			Label("check_maxskip_cont_" + name)
		}
		// if nextS > sLimit {goto emitRemainder}
		{
			CMPL(nextS.As32(), sLimitL)
			JAE(LabelRef("emit_remainder_" + name))
		}
		MOVQ(Mem{Base: src, Index: s, Scale: 1}, cv)
		assert(func(ok LabelRef) {
			// Check if s is valid (we should have jumped above if not)
			tmp := GP64()
			MOVQ(lenSrcQ, tmp)
			CMPQ(tmp, s.As64())
			JA(ok)
		})
		// move nextS to stack.
		MOVL(nextS.As32(), nextSTempL)

		candidateS := GP32()
		lHasher := hashN(lHashBytes, lTableBits)
		{
			sHasher := hashN(sHashBytes, sTableBits)
			hash0, hash1 := GP64(), GP64()
			MOVQ(cv, hash0)
			MOVQ(cv, hash1)
			lHasher.hash(hash0)
			sHasher.hash(hash1)
			MOVL(lTab.Idx(hash0, 4), candidate)
			MOVL(sTab.Idx(hash1, 4), candidateS)
			assert(func(ok LabelRef) {
				CMPQ(hash0, U32(lTableSize))
				JB(ok)
			})
			assert(func(ok LabelRef) {
				CMPQ(hash1, U32(sTableSize))
				JB(ok)
			})

			MOVL(s, lTab.Idx(hash0, 4))
			MOVL(s, sTab.Idx(hash1, 4))
		}

		longVal := GP64()
		shortVal := GP64()
		MOVQ(Mem{Base: src, Index: candidate, Scale: 1}, longVal)
		MOVQ(Mem{Base: src, Index: candidateS, Scale: 1}, shortVal)

		// If we have at least 8 bytes match, choose that first.
		CMPQ(longVal, cv.As64())
		JEQ(LabelRef("candidate_match_" + name))

		CMPQ(shortVal, cv.As64())
		JNE(LabelRef("no_short_found_" + name))
		MOVL(candidateS.As32(), candidate.As32())
		JMP(LabelRef("candidate_match_" + name))

		Label("no_short_found_" + name)
		MOVL(longVal.As32(), longVal.As32())

		// En/disable repeat matching.
		// Too small improvement
		if false {
			{
				CMPL(repeatL, U8(0))
				JEQ(LabelRef("no_repeat_found_" + name))
			}
			// Check repeat at offset checkRep
			const checkRep = 1
			const wantRepeatBytes = 6
			const repeatMask = ((1 << (wantRepeatBytes * 8)) - 1) << (8 * checkRep)
			{
				// rep = s - repeat
				rep := GP32()
				MOVL(s, rep)
				SUBL(repeatL, rep) // rep = s - repeat

				// if uint32(cv>>(checkRep*8)) == load32(src, s-repeat+checkRep) {
				tmp := GP64()
				mask := GP64()
				MOVQ(Mem{Base: src, Index: rep, Disp: 0, Scale: 1}, tmp.As64())
				MOVQ(U64(repeatMask), mask)
				XORQ(cv.As64(), tmp.As64())
				TESTQ(mask.As64(), tmp.As64())
				// BAIL, no repeat.
				JNE(LabelRef("no_repeat_found_" + name))
			}
			// base = s + checkRep
			base := GP32()
			LEAL(Mem{Base: s, Disp: checkRep}, base)

			// nextEmit before repeat.
			nextEmit := GP32()
			MOVL(nextEmitL, nextEmit)

			// Extend back
			if true {
				i := GP32()
				MOVL(base, i)
				SUBL(repeatL, i)
				JZ(LabelRef("repeat_extend_back_end_" + name))

				Label("repeat_extend_back_loop_" + name)
				// if base <= nextemit {exit}
				CMPL(base.As32(), nextEmit)
				JBE(LabelRef("repeat_extend_back_end_" + name))
				// if src[i-1] == src[base-1]
				tmp, tmp2 := GP64(), GP64()
				MOVB(Mem{Base: src, Index: i, Scale: 1, Disp: -1}, tmp.As8())
				MOVB(Mem{Base: src, Index: base, Scale: 1, Disp: -1}, tmp2.As8())
				CMPB(tmp.As8(), tmp2.As8())
				JNE(LabelRef("repeat_extend_back_end_" + name))
				LEAL(Mem{Base: base, Disp: -1}, base)
				DECL(i)
				JNZ(LabelRef("repeat_extend_back_loop_" + name))
			}
			Label("repeat_extend_back_end_" + name)

			// Base is now at start. Emit until base.
			// d += emitLiteral(dst[d:], src[nextEmit:base])
			if true {
				o.emitLiteralsDstP(nextEmitL, base, src, dst, "repeat_emit_"+name)
			}

			// Extend forward
			{
				// s += 4 + checkRep
				ADDL(U8(wantRepeatBytes+checkRep), s)

				if true {
					// candidate := s - repeat + 4 + checkRep
					MOVL(s, candidate)
					SUBL(repeatL, candidate) // candidate = s - repeat

					// srcLeft = len(src) - s
					srcLeft := GP64()
					MOVQ(lenSrcQ, srcLeft)
					SUBL(s, srcLeft.As32())
					assert(func(ok LabelRef) {
						// if srcleft < maxint32: ok
						CMPQ(srcLeft, U32(0x7fffffff))
						JB(ok)
					})
					// Forward address
					forwardStart := GP64()
					LEAQ(Mem{Base: src, Index: s, Scale: 1}, forwardStart)
					// End address
					backStart := GP64()
					LEAQ(Mem{Base: src, Index: candidate, Scale: 1}, backStart)

					length := o.matchLen("repeat_extend_"+name, forwardStart, backStart, srcLeft, LabelRef("repeat_extend_forward_end_"+name))
					forwardStart, backStart, srcLeft = nil, nil, nil
					Label("repeat_extend_forward_end_" + name)
					// s+= length
					ADDL(length.As32(), s)
				}
			}
			// Emit
			if true {
				// length = s-base
				length := GP32()
				MOVL(s, length)
				SUBL(base.As32(), length) // length = s - base

				offsetVal := GP32()
				MOVL(repeatL, offsetVal)

				// Emit as repeat...
				o.emitRepeat("match_repeat_"+name, length, offsetVal, nil, dst, LabelRef("repeat_end_emit_"+name), false)

				Label("repeat_end_emit_" + name)
				// Store new dst and nextEmit
				MOVL(s, nextEmitL)
			}
			// if s >= sLimit is picked up on next loop.
			if false {
				CMPL(s.As32(), sLimitL)
				JAE(LabelRef("emit_remainder_" + name))
			}
			JMP(LabelRef("search_loop_" + name))
		}
		Label("no_repeat_found_" + name)
		{
			// Check candidates are ok. All must be < s and < len(src)
			assert(func(ok LabelRef) {
				tmp := GP64()
				MOVQ(lenSrcQ, tmp)
				CMPL(tmp.As32(), candidate)
				JA(ok)
			})
			assert(func(ok LabelRef) {
				CMPL(s, candidate)
				JA(ok)
			})
			assert(func(ok LabelRef) {
				tmp := GP64()
				MOVQ(lenSrcQ, tmp)
				CMPL(tmp.As32(), candidateS)
				JA(ok)
			})
			assert(func(ok LabelRef) {
				CMPL(s, candidateS)
				JA(ok)
			})

			CMPL(longVal.As32(), cv.As32())
			JEQ(LabelRef("candidate_match_" + name))

			//if uint32(cv) == load32(src, candidateS)
			CMPL(shortVal.As32(), cv.As32())
			JEQ(LabelRef("candidateS_match_" + name))

			// No match found, next loop
			// s = nextS
			MOVL(nextSTempL, s)
			JMP(LabelRef("search_loop_" + name))

			// Short match at s, try a long candidate at s+1
			Label("candidateS_match_" + name)
			if true {
				hash0 := GP64()
				SHRQ(U8(8), cv)
				MOVQ(cv, hash0)
				lHasher.hash(hash0)
				MOVL(lTab.Idx(hash0, 4), candidate)
				INCL(s)
				assert(func(ok LabelRef) {
					CMPQ(hash0, U32(lTableSize))
					JB(ok)
				})
				MOVL(s, lTab.Idx(hash0, 4))
				CMPL(Mem{Base: src, Index: candidate, Scale: 1}, cv.As32())
				JEQ(LabelRef("candidate_match_" + name))
				// No match, decrement s again and use short match at s...
				DECL(s)
			}
			MOVL(candidateS, candidate)
		}
	}

	Label("candidate_match_" + name)
	// We have a match at 's' with src offset in "candidate" that matches at least 4 bytes.
	// Extend backwards
	if true {
		ne := GP32()
		MOVL(nextEmitL, ne)
		TESTL(candidate, candidate)
		JZ(LabelRef("match_extend_back_end_" + name))

		// candidate is tested when decremented, so we loop back here.
		Label("match_extend_back_loop_" + name)
		// if s <= nextEmit {exit}
		CMPL(s, ne)
		JBE(LabelRef("match_extend_back_end_" + name))
		// if src[candidate-1] == src[s-1]
		tmp, tmp2 := GP64(), GP64()
		MOVB(Mem{Base: src, Index: candidate, Scale: 1, Disp: -1}, tmp.As8())
		MOVB(Mem{Base: src, Index: s, Scale: 1, Disp: -1}, tmp2.As8())
		CMPB(tmp.As8(), tmp2.As8())
		JNE(LabelRef("match_extend_back_end_" + name))
		LEAL(Mem{Base: s, Disp: -1}, s)
		DECL(candidate)
		JZ(LabelRef("match_extend_back_end_" + name))
		JMP(LabelRef("match_extend_back_loop_" + name))
	}
	Label("match_extend_back_end_" + name)

	// Bail if we exceed the maximum size.
	if true {
		// tmp = s-nextEmit
		tmp := GP64()
		MOVL(s, tmp.As32())
		SUBL(nextEmitL, tmp.As32())
		// tmp = &dst + s-nextEmit
		LEAQ(Mem{Base: dst, Index: tmp, Scale: 1, Disp: literalMaxOverhead}, tmp)
		CMPQ(tmp, dstLimitPtrQ)
		JB(LabelRef("match_dst_size_check_" + name))
		ri, err := ReturnIndex(0).Resolve()
		if err != nil {
			panic(err)
		}
		MOVQ(U32(0), ri.Addr)
		if o.avx2 {
			VZEROUPPER()
		}
		RET()
	}
	Label("match_dst_size_check_" + name)

	base := GP32()
	MOVL(s, base.As32())

	// s+=4, candidate+=4
	ADDL(U8(4), s)
	ADDL(U8(4), candidate)
	// Extend the 4-byte match as long as possible and emit copy.
	{
		assert(func(ok LabelRef) {
			// s must be > candidate cannot be equal.
			CMPL(s, candidate)
			JA(ok)
		})
		// srcLeft = len(src) - s
		srcLeft := GP64()
		MOVQ(lenSrcQ, srcLeft)
		SUBL(s, srcLeft.As32())
		assert(func(ok LabelRef) {
			// if srcleft < maxint32: ok
			CMPQ(srcLeft, U32(0x7fffffff))
			JB(ok)
		})

		a, b := GP64(), GP64()
		LEAQ(Mem{Base: src, Index: s, Scale: 1}, a)
		LEAQ(Mem{Base: src, Index: candidate, Scale: 1}, b)
		length := o.matchLen("match_nolit_"+name,
			a, b,
			srcLeft,
			LabelRef("match_nolit_end_"+name),
		)
		Label("match_nolit_end_" + name)
		assert(func(ok LabelRef) {
			CMPL(length.As32(), U32(math.MaxInt32))
			JB(ok)
		})
		a, b, srcLeft = nil, nil, nil

		offset := GP64()
		offset32 := offset.As32()
		{
			// offset = base - candidate
			MOVL(s, offset32)
			SUBL(candidate, offset32)
			Comment("Check if repeat")
			if !o.snappy {
				CMPL(repeatL, offset32)
				JEQ(LabelRef("match_is_repeat_" + name))
			}

			// NOT REPEAT
			{
				// Check if match is better..
				if o.maxOffset > 65535 {
					CMPL(length.As32(), U8(1))
					JA(LabelRef("match_length_ok_" + name))
					CMPL(offset32, U32(65535))
					JBE(LabelRef("match_length_ok_" + name))
					// Match is equal or worse to the encoding.
					MOVL(nextSTempL, s)
					INCL(s)
					JMP(LabelRef("search_loop_" + name))
					Label("match_length_ok_" + name)
				}
				// Store updated repeat
				MOVL(offset32, repeatL)
				// Emit....
				o.emitLiteralsDstP(nextEmitL, base, src, dst, "match_emit_"+name)
				// s += length (length is destroyed, use it now)
				ADDL(length.As32(), s)

				// length += 4
				ADDL(U8(4), length.As32())
				MOVL(s, nextEmitL) // nextEmit = s
				o.emitCopy("match_nolit_"+name, length, offset, nil, dst, nil, LabelRef("match_nolit_emitcopy_end_"+name))
				// Jumps at end
			}
			// REPEAT
			if !o.snappy {
				Label("match_is_repeat_" + name)
				// Emit....
				o.emitLiteralsDstP(nextEmitL, base, src, dst, "match_emit_repeat_"+name)
				// s += length (length is destroyed, use it now)
				ADDL(length.As32(), s)

				// length += 4
				ADDL(U8(4), length.As32())
				MOVL(s, nextEmitL) // nextEmit = s
				o.emitRepeat("match_nolit_repeat_"+name, length, offset, nil, dst, LabelRef("match_nolit_emitcopy_end_"+name), false)
			}
		}
		Label("match_nolit_emitcopy_end_" + name)

		// if s >= sLimit { end }
		{
			CMPL(s.As32(), sLimitL)
			JAE(LabelRef("emit_remainder_" + name))
		}

		// Bail if we exceed the maximum size.
		{
			CMPQ(dst, dstLimitPtrQ)
			JB(LabelRef("match_nolit_dst_ok_" + name))
			ri, err := ReturnIndex(0).Resolve()
			if err != nil {
				panic(err)
			}
			MOVQ(U32(0), ri.Addr)
			if o.avx2 {
				VZEROUPPER()
			}
			RET()
		}
	}
	Label("match_nolit_dst_ok_" + name)
	if true {
		lHasher := hashN(lHashBytes, lTableBits)
		sHasher := hashN(sHashBytes, sTableBits)

		index0, index1 := GP64(), GP64()
		// index0 := base + 1
		LEAQ(Mem{Base: base, Disp: 1}, index0)
		// index1 := s - 2
		LEAQ(Mem{Base: s, Disp: -2}, index1)
		hash0l, hash0s, hash1l, hash1s := GP64(), GP64(), GP64(), GP64()
		MOVQ(Mem{Base: src, Index: index0, Scale: 1, Disp: 0}, hash0l)
		MOVQ(Mem{Base: src, Index: index0, Scale: 1, Disp: 1}, hash0s)
		MOVQ(Mem{Base: src, Index: index1, Scale: 1, Disp: 0}, hash1l)
		MOVQ(Mem{Base: src, Index: index1, Scale: 1, Disp: 1}, hash1s)

		lHasher.hash(hash0l)
		sHasher.hash(hash0s)
		lHasher.hash(hash1l)
		sHasher.hash(hash1s)

		plusone0, plusone1 := GP64(), GP64()
		LEAQ(Mem{Base: index0, Disp: 1}, plusone0)
		LEAQ(Mem{Base: index1, Disp: 1}, plusone1)
		MOVL(index0.As32(), lTab.Idx(hash0l, 4))
		MOVL(index1.As32(), lTab.Idx(hash1l, 4))
		MOVL(plusone0.As32(), sTab.Idx(hash0s, 4))
		MOVL(plusone1.As32(), sTab.Idx(hash1s, 4))

		// index2 := (index0 + index1 + 1) >> 1
		index2 := GP64()
		LEAQ(Mem{Base: index1, Disp: 1, Index: index0, Scale: 1}, index2)
		SHRQ(U8(1), index2)

		ADDQ(U8(1), index0)
		SUBQ(U8(1), index1)

		Label("index_loop_" + name)
		// for index2 < index1
		CMPQ(index2, index1)
		JAE(LabelRef("search_loop_" + name))
		hash0l, hash1l = GP64(), GP64()
		MOVQ(Mem{Base: src, Index: index0, Scale: 1, Disp: 0}, hash0l)
		MOVQ(Mem{Base: src, Index: index2, Scale: 1, Disp: 0}, hash1l)

		lHasher.hash(hash0l)
		lHasher.hash(hash1l)

		MOVL(index0.As32(), lTab.Idx(hash0l, 4))
		MOVL(index2.As32(), lTab.Idx(hash1l, 4))

		ADDQ(U8(2), index0)
		ADDQ(U8(2), index2)
		JMP(LabelRef("index_loop_" + name))
	} else {
		lHasher := hashN(lHashBytes, lTableBits)
		sHasher := hashN(sHashBytes, sTableBits)

		// Index base+1 long, base+2 short...
		cv := GP64()
		INCL(base)
		MOVQ(Mem{Base: src, Index: base, Scale: 1, Disp: 0}, cv)
		hash0, hash1, hash2, hash3 := GP64(), GP64(), GP64(), GP64()
		MOVQ(cv, hash0) // src[base+1]
		MOVQ(cv, hash1)
		MOVQ(cv, hash2)
		SHRQ(U8(8), hash1) // src[base+2]
		MOVQ(hash1, hash3)
		SHRQ(U8(16), hash2)        // src[base+3]
		bp1, bp2 := GP32(), GP32() // base+1
		LEAL(Mem{Base: base, Disp: 1}, bp1)
		LEAL(Mem{Base: base, Disp: 2}, bp2)

		// Load s-2 early
		MOVQ(Mem{Base: src, Index: s, Scale: 1, Disp: -2}, cv)

		lHasher.hash(hash0)
		lHasher.hash(hash3)
		sHasher.hash(hash1)
		sHasher.hash(hash2)
		assert(func(ok LabelRef) {
			CMPQ(hash0, U32(lTableSize))
			JB(ok)
		})
		assert(func(ok LabelRef) {
			CMPQ(hash3, U32(lTableSize))
			JB(ok)
		})
		assert(func(ok LabelRef) {
			CMPQ(hash1, U32(sTableSize))
			JB(ok)
		})
		assert(func(ok LabelRef) {
			CMPQ(hash2, U32(sTableSize))
			JB(ok)
		})
		MOVL(base, lTab.Idx(hash0, 4))
		MOVL(bp1, lTab.Idx(hash3, 4))
		MOVL(bp1, sTab.Idx(hash1, 4))
		MOVL(bp2, sTab.Idx(hash2, 4))

		// Index s-2 long, s-1 long+short...
		MOVQ(cv, hash0) // src[s-2]
		MOVQ(cv, hash1) // src[s-1]
		SHRQ(U8(8), hash1)
		MOVQ(hash1, hash3)
		sm1, sm2 := GP32(), GP32() // s -1, s - 2
		LEAL(Mem{Base: s, Disp: -2}, sm2)
		LEAL(Mem{Base: s, Disp: -1}, sm1)
		lHasher.hash(hash0)
		sHasher.hash(hash1)
		lHasher.hash(hash3)
		assert(func(ok LabelRef) {
			CMPQ(hash0, U32(lTableSize))
			JB(ok)
		})
		assert(func(ok LabelRef) {
			CMPQ(hash3, U32(lTableSize))
			JB(ok)
		})
		assert(func(ok LabelRef) {
			CMPQ(hash1, U32(sTableSize))
			JB(ok)
		})
		MOVL(sm2, lTab.Idx(hash0, 4))
		MOVL(sm1, sTab.Idx(hash1, 4))
		MOVL(sm1, lTab.Idx(hash3, 4))
		JMP(LabelRef("search_loop_" + name))
	}

	Label("emit_remainder_" + name)
	// Bail if we exceed the maximum size.
	// if d+len(src)-nextEmitL > dstLimitPtrQ {	return 0
	{
		// remain = len(src) - nextEmit
		remain := GP64()
		MOVQ(lenSrcQ, remain)
		SUBL(nextEmitL, remain.As32())

		dstExpect := GP64()
		// dst := dst + (len(src)-nextEmitL)

		LEAQ(Mem{Base: dst, Index: remain, Scale: 1, Disp: literalMaxOverhead}, dstExpect)
		CMPQ(dstExpect, dstLimitPtrQ)
		JB(LabelRef("emit_remainder_ok_" + name))
		ri, err := ReturnIndex(0).Resolve()
		if err != nil {
			panic(err)
		}
		MOVQ(U32(0), ri.Addr)
		if o.avx2 {
			VZEROUPPER()
		}
		RET()
		Label("emit_remainder_ok_" + name)
	}
	// emitLiteral(dst[d:], src[nextEmitL:])
	emitEnd := GP64()
	MOVQ(lenSrcQ, emitEnd)

	// Emit final literals.
	// Since we may be at the end of source,
	// we cannot have output margin.
	x := o.outputMargin
	o.outputMargin = 0
	o.emitLiteralsDstP(nextEmitL, emitEnd, src, dst, "emit_remainder_"+name)
	o.outputMargin = x

	// Assert size is < limit
	assert(func(ok LabelRef) {
		// if dstBaseQ <  dstLimitPtrQ: ok
		CMPQ(dst, dstLimitPtrQ)
		JB(ok)
	})

	// length := start - base (ptr arithmetic)
	length := GP64()
	dstBase := Load(Param("dst").Base(), GP64())
	MOVQ(dst, length)
	SUBQ(dstBase, length)

	// Assert size is < len(src)
	assert(func(ok LabelRef) {
		// if len(src) >= length: ok
		CMPQ(lenSrcQ, length)
		JAE(ok)
	})
	// Assert size is < len(dst)
	assert(func(ok LabelRef) {
		// if len(dst) >= length: ok
		CMPQ(lenDstQ, length)
		JAE(ok)
	})
	Store(length, ReturnIndex(0))
	if o.avx2 {
		VZEROUPPER()
	}
	RET()
}

// emitLiterals emits literals from nextEmit to base, updates nextEmit, dstBase.
// Checks if base == nextemit.
// src & base are untouched.
func (o options) emitLiterals(nextEmitL Mem, base reg.GPVirtual, src reg.GPVirtual, dstBase Mem, name string) {
	nextEmit, litLen, dstBaseTmp, litBase := GP32(), GP32(), GP64(), GP64()
	MOVL(nextEmitL, nextEmit)
	CMPL(nextEmit, base.As32())
	JEQ(LabelRef("emit_literal_skip_" + name))
	MOVL(base.As32(), litLen.As32())

	// Base is now next emit.
	MOVL(base.As32(), nextEmitL)

	// litBase = src[nextEmitL:]
	LEAQ(Mem{Base: src, Index: nextEmit, Scale: 1}, litBase)
	SUBL(nextEmit, litLen.As32()) // litlen = base - nextEmit

	// Load (and store when we return)
	MOVQ(dstBase, dstBaseTmp)
	o.emitLiteral(name, litLen, nil, dstBaseTmp, litBase, LabelRef("emit_literal_done_"+name), true)
	Label("emit_literal_done_" + name)

	// Emitted length must be > litlen.
	// We have already checked for len(0) above.
	assert(func(ok LabelRef) {
		tmp := GP64()
		MOVQ(dstBaseTmp, tmp)
		SUBQ(dstBase, tmp) // tmp = dstBaseTmp - dstBase
		// if tmp > litLen: ok
		CMPQ(tmp, litLen.As64())
		JA(ok)
	})
	// Store updated dstBase
	MOVQ(dstBaseTmp, dstBase)
	Label("emit_literal_skip_" + name)
}

// emitLiterals emits literals from nextEmit to base, updates nextEmit, dstBase.
// Checks if base == nextemit.
// src & base are untouched.
func (o options) emitLiteralsDstP(nextEmitL Mem, base reg.GPVirtual, src, dst reg.GPVirtual, name string) {
	nextEmit, litLen, litBase := GP32(), GP32(), GP64()
	MOVL(nextEmitL, nextEmit)
	CMPL(nextEmit, base.As32())
	JEQ(LabelRef("emit_literal_done_" + name))
	MOVL(base.As32(), litLen.As32())

	// Base is now next emit.
	MOVL(base.As32(), nextEmitL)

	// litBase = src[nextEmitL:]
	LEAQ(Mem{Base: src, Index: nextEmit, Scale: 1}, litBase)
	SUBL(nextEmit, litLen.As32()) // litlen = base - nextEmit

	// Load (and store when we return)
	o.emitLiteral(name, litLen, nil, dst, litBase, LabelRef("emit_literal_done_"+name), true)
	Label("emit_literal_done_" + name)
}

type hashGen struct {
	bytes     int
	tablebits int
	mulreg    reg.GPVirtual
}

// hashN uses multiply to get a 'output' hash on the hash of the lowest 'bytes' bytes in value.
func hashN(hashBytes, tablebits int) hashGen {
	h := hashGen{
		bytes:     hashBytes,
		tablebits: tablebits,
		mulreg:    GP64(),
	}
	primebytes := uint64(0)
	switch hashBytes {
	case 3:
		primebytes = 506832829
	case 4:
		primebytes = 2654435761
	case 5:
		primebytes = 889523592379
	case 6:
		primebytes = 227718039650203
	case 7:
		primebytes = 58295818150454627
	case 8:
		primebytes = 0xcf1bbcdcb7a56463
	default:
		panic("invalid hash length")
	}
	MOVQ(Imm(primebytes), h.mulreg)
	return h
}

// hash uses multiply to get hash of the value.
func (h hashGen) hash(val reg.GPVirtual) {
	// Move value to top of register.
	if h.bytes < 8 {
		SHLQ(U8(64-8*h.bytes), val)
	}
	//  329 AMD64               :IMUL r64, r64                         L:   0.86ns=  3.0c  T:   0.29ns=  1.00c
	// 2020 BMI2                :MULX r64, r64, r64                    L:   1.14ns=  4.0c  T:   0.29ns=  1.00c
	IMULQ(h.mulreg, val)
	// Move value to bottom
	// 2032 BMI2                :SHRX r64, r64, r64                    L:   0.29ns=  1.0c  T:   0.12ns=  0.42c
	//  236 AMD64               :SHR r64, imm8                         L:   0.29ns=  1.0c  T:   0.13ns=  0.46c
	SHRQ(U8(64-h.tablebits), val)
}

func (o options) genEmitLiteral() {
	TEXT("emitLiteral", NOSPLIT, "func(dst, lit []byte) int")
	Doc("emitLiteral writes a literal chunk and returns the number of bytes written.", "",
		"It assumes that:",
		fmt.Sprintf("  dst is long enough to hold the encoded bytes with margin of %d bytes", o.outputMargin),
		"  0 <= len(lit) && len(lit) <= math.MaxUint32", "")
	Pragma("noescape")

	dstBase, litBase, litLen, retval := GP64(), GP64(), GP64(), GP64()
	Load(Param("lit").Len(), litLen)
	Load(Param("dst").Base(), dstBase)
	Load(Param("lit").Base(), litBase)
	TESTQ(litLen, litLen)
	JZ(LabelRef("emit_literal_end_standalone_skip"))
	o.emitLiteral("standalone", litLen, retval, dstBase, litBase, "emit_literal_end_standalone", false)

	Label("emit_literal_end_standalone_skip")
	XORQ(retval, retval)

	Label("emit_literal_end_standalone")
	Store(retval, ReturnIndex(0))
	RET()

}

// emitLiteral can be used for inlining an emitLiteral call.
// litLen must be > 0.
// stack must have at least 32 bytes.
// retval will contain emitted bytes, but can be nil if this is not interesting.
// dstBase and litBase are updated.
// Uses 2 GP registers.
// If updateDst is true dstBase will have the updated end pointer and an additional register will be used.
func (o options) emitLiteral(name string, litLen, retval, dstBase, litBase reg.GPVirtual, end LabelRef, updateDst bool) {
	n := GP32()
	n16 := GP32()

	// litLen must be > 0
	assert(func(ok LabelRef) {
		TESTL(litLen.As32(), litLen.As32())
		JNZ(ok)
	})

	// We always add litLen bytes
	if retval != nil {
		MOVL(litLen.As32(), retval.As32())
	}
	// n = litlen - 1
	LEAL(Mem{Base: litLen.As32(), Disp: -1}, n)

	// Find number of bytes to emit for tag.
	CMPL(n.As32(), U8(60))
	JB(LabelRef("one_byte_" + name))
	CMPL(n.As32(), U32(1<<8))
	JB(LabelRef("two_bytes_" + name))
	if o.maxLen >= 1<<16 {
		CMPL(n.As32(), U32(1<<16))
		JB(LabelRef("three_bytes_" + name))
	} else {
		JB(LabelRef("three_bytes_" + name))
	}
	if o.maxLen >= 1<<16 {
		if o.maxLen >= 1<<24 {
			CMPL(n.As32(), U32(1<<24))
			JB(LabelRef("four_bytes_" + name))
		} else {
			JMP(LabelRef("four_bytes_" + name))
		}
	}
	if o.maxLen >= 1<<24 {
		Label("five_bytes_" + name)
		if !o.skipOutput {
			MOVB(U8(252), Mem{Base: dstBase})
			MOVL(n.As32(), Mem{Base: dstBase, Disp: 1})
		}
		if retval != nil {
			ADDQ(U8(5), retval)
		}
		ADDQ(U8(5), dstBase)
		JMP(LabelRef("memmove_long_" + name))
	}
	if o.maxLen >= 1<<16 {
		Label("four_bytes_" + name)
		if !o.skipOutput {
			MOVL(n, n16)
			SHRL(U8(16), n16.As32())
			MOVB(U8(248), Mem{Base: dstBase})
			MOVW(n.As16(), Mem{Base: dstBase, Disp: 1})
			MOVB(n16.As8(), Mem{Base: dstBase, Disp: 3})
		}
		if retval != nil {
			ADDQ(U8(4), retval)
		}
		ADDQ(U8(4), dstBase)
		JMP(LabelRef("memmove_long_" + name))
	}
	Label("three_bytes_" + name)
	if !o.skipOutput {
		MOVB(U8(0xf4), Mem{Base: dstBase})
		MOVW(n.As16(), Mem{Base: dstBase, Disp: 1})
	}
	if retval != nil {
		ADDQ(U8(3), retval)
	}
	ADDQ(U8(3), dstBase)
	JMP(LabelRef("memmove_long_" + name))

	Label("two_bytes_" + name)
	if !o.skipOutput {
		MOVB(U8(0xf0), Mem{Base: dstBase})
		MOVB(n.As8(), Mem{Base: dstBase, Disp: 1})
	}
	if retval != nil {
		ADDQ(U8(2), retval)
	}
	ADDQ(U8(2), dstBase)
	CMPL(n.As32(), U8(64))
	JB(LabelRef("memmove_" + name))
	JMP(LabelRef("memmove_long_" + name))

	Label("one_byte_" + name)
	if !o.skipOutput {
		SHLB(U8(2), n.As8())
		MOVB(n.As8(), Mem{Base: dstBase})
	}
	if retval != nil {
		ADDQ(U8(1), retval)
	}
	ADDQ(U8(1), dstBase)
	// Fallthrough

	Label("memmove_" + name)

	// copy(dst[i:], lit)
	dstEnd := GP64()
	copyEnd := end
	if updateDst {
		copyEnd = LabelRef("memmove_end_copy_" + name)
		LEAQ(Mem{Base: dstBase, Index: litLen, Scale: 1}, dstEnd)
	}
	if !o.skipOutput {

		length := GP64()
		MOVL(litLen.As32(), length.As32())

		// We wrote one byte, we have that less in output margin.
		o.outputMargin--
		// updates litBase.
		o.genMemMoveShort("emit_lit_memmove_"+name, dstBase, litBase, length, copyEnd)
		o.outputMargin++
	}
	if updateDst {
		Label("memmove_end_copy_" + name)
		MOVQ(dstEnd, dstBase)
	}
	JMP(end)

	// > 64 bytes
	Label("memmove_long_" + name)

	// copy(dst[i:], lit)
	dstEnd = GP64()
	copyEnd = end
	if updateDst {
		copyEnd = LabelRef("memmove_end_copy_long_" + name)
		LEAQ(Mem{Base: dstBase, Index: litLen, Scale: 1}, dstEnd)
	}
	if !o.skipOutput {
		length := GP64()
		MOVL(litLen.As32(), length.As32())

		// updates litBase.
		o.genMemMoveLong("emit_lit_memmove_long_"+name, dstBase, litBase, length, copyEnd)
	}
	if updateDst {
		Label("memmove_end_copy_long_" + name)
		MOVQ(dstEnd, dstBase)
	}
	JMP(end)
	// Should be unreachable
	if debug {
		INT(Imm(3))
	}
	return
}

// genEmitRepeat generates a standlone emitRepeat.
func (o options) genEmitRepeat() {
	TEXT("emitRepeat", NOSPLIT, "func(dst []byte, offset, length int) int")
	Doc("emitRepeat writes a repeat chunk and returns the number of bytes written.",
		"Length must be at least 4 and < 1<<32", "")
	Pragma("noescape")

	dstBase, offset, length, retval := GP64(), GP64(), GP64(), GP64()

	// retval = 0
	XORQ(retval, retval)

	Load(Param("dst").Base(), dstBase)
	Load(Param("offset"), offset)
	Load(Param("length"), length)
	o.emitRepeat("standalone", length, offset, retval, dstBase, LabelRef("gen_emit_repeat_end"), false)
	Label("gen_emit_repeat_end")
	Store(retval, ReturnIndex(0))
	RET()
}

// emitRepeat can be used for inlining an emitRepeat call.
// length >= 4 and < 1<<32
// length is modified. dstBase is updated. retval is added to input.
// retval can be nil.
// Will jump to end label when finished.
// Uses 1 GP register.
// longer indicates we know match will be > 12
func (o options) emitRepeat(name string, length reg.GPVirtual, offset reg.GPVirtual, retval reg.GPVirtual, dstBase reg.GPVirtual, end LabelRef, longer bool) {
	Comment("emitRepeat")
	if longer {
		// Skip initial length tests
		LEAL(Mem{Base: length, Disp: -4}, length.As32()) // length -= 4
		JMP(LabelRef("cant_repeat_two_offset_" + name))
	}
	Label("emit_repeat_again_" + name)
	tmp := GP32()
	MOVL(length.As32(), tmp)                         // Copy length
	LEAL(Mem{Base: length, Disp: -4}, length.As32()) // length -= 4

	// if length <= 4 (use copied value)
	CMPL(tmp.As32(), U8(8))
	JBE(LabelRef("repeat_two_" + name))

	// length < 8 && offset < 2048
	CMPL(tmp.As32(), U8(12))
	JAE(LabelRef("cant_repeat_two_offset_" + name))
	if o.maxLen >= 2048 {
		CMPL(offset.As32(), U32(2048))
		JB(LabelRef("repeat_two_offset_" + name))
	}

	const maxRepeat = ((1 << 24) - 1) + 65536
	Label("cant_repeat_two_offset_" + name)
	CMPL(length.As32(), U32((1<<8)+4))
	JB(LabelRef("repeat_three_" + name)) // if length < (1<<8)+4
	if o.maxLen >= (1<<16)+(1<<8) {
		CMPL(length.As32(), U32((1<<16)+(1<<8)))
		JB(LabelRef("repeat_four_" + name)) // if length < (1 << 16) + (1 << 8)
	} else {
		// Not needed, we should skip to it when generating.
		// JMP(LabelRef("repeat_four_" + name)) // if length < (1 << 16) + (1 << 8)
	}
	if o.maxLen >= maxRepeat {
		CMPL(length.As32(), U32(maxRepeat))
		JB(LabelRef("repeat_five_" + name)) // If less than 24 bits to represent.

		if !o.skipOutput {
			// We have have more than 24 bits
			// Emit so we have at least 4 bytes left.
			LEAL(Mem{Base: length, Disp: -(maxRepeat - 4)}, length.As32()) // length -= (maxRepeat - 4)
			MOVL(U32(7<<2|tagCopy1|(65531<<16)), Mem{Base: dstBase})       // dst[0] = 7<<2 | tagCopy1, dst[1] = 0 dst[2+3] = 65531
			MOVB(U8(255), Mem{Base: dstBase, Disp: 4})
			ADDQ(U8(5), dstBase)
		}
		if retval != nil {
			ADDQ(U8(5), retval)
		}
		JMP(LabelRef("emit_repeat_again_" + name))
	} else {
		// Not needed.
		// JMP(LabelRef("repeat_five_" + name)) // If less than 24 bits to represent.
	}

	// Must be able to be within 5 bytes.
	if o.maxLen >= (1<<16)+(1<<8) {
		Label("repeat_five_" + name)
		LEAL(Mem{Base: length, Disp: -65536}, length.As32()) // length -= 65536
		if !o.skipOutput {
			MOVL(length.As32(), offset.As32())
			MOVW(U16(7<<2|tagCopy1), Mem{Base: dstBase})     // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
			MOVW(length.As16(), Mem{Base: dstBase, Disp: 2}) // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
			SARL(U8(16), offset.As32())                      // offset = length >> 16
			MOVB(offset.As8(), Mem{Base: dstBase, Disp: 4})  // dst[4] = length >> 16
		}
		if retval != nil {
			ADDQ(U8(5), retval) // i += 5
		}
		ADDQ(U8(5), dstBase) // dst += 5
		JMP(end)
	}
	Label("repeat_four_" + name)
	if !o.skipOutput {
		LEAL(Mem{Base: length, Disp: -256}, length.As32()) // length -= 256
		MOVW(U16(6<<2|tagCopy1), Mem{Base: dstBase})       // dst[0] = 6<<2 | tagCopy1, dst[1] = 0
		MOVW(length.As16(), Mem{Base: dstBase, Disp: 2})   // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	}
	if retval != nil {
		ADDQ(U8(4), retval) // i += 4
	}
	ADDQ(U8(4), dstBase) // dst += 4
	JMP(end)

	Label("repeat_three_" + name)
	if !o.skipOutput {
		LEAL(Mem{Base: length, Disp: -4}, length.As32()) // length -= 4
		MOVW(U16(5<<2|tagCopy1), Mem{Base: dstBase})     // dst[0] = 5<<2 | tagCopy1, dst[1] = 0
		MOVB(length.As8(), Mem{Base: dstBase, Disp: 2})  // dst[2] = uint8(length)
	}
	if retval != nil {
		ADDQ(U8(3), retval) // i += 3
	}
	ADDQ(U8(3), dstBase) // dst += 3
	JMP(end)

	Label("repeat_two_" + name)
	// dst[0] = uint8(length)<<2 | tagCopy1, dst[1] = 0
	if !o.skipOutput {
		SHLL(U8(2), length.As32())
		ORL(U8(tagCopy1), length.As32())
		MOVW(length.As16(), Mem{Base: dstBase}) // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	}
	if retval != nil {
		ADDQ(U8(2), retval) // i += 2
	}
	ADDQ(U8(2), dstBase) // dst += 2
	JMP(end)

	Label("repeat_two_offset_" + name)
	// Emit the remaining copy, encoded as 2 bytes.
	// dst[1] = uint8(offset)
	// dst[0] = uint8(offset>>8)<<5 | uint8(length)<<2 | tagCopy1
	tmp = GP64()
	XORQ(tmp, tmp)
	// Use scale and displacement to shift and subtract values from length.
	if !o.skipOutput {
		LEAL(Mem{Base: tmp, Index: length, Scale: 4, Disp: tagCopy1}, length.As32())
		MOVB(offset.As8(), Mem{Base: dstBase, Disp: 1}) // Store offset lower byte
		SARL(U8(8), offset.As32())                      // Remove lower
		SHLL(U8(5), offset.As32())                      // Shift back up
		ORL(offset.As32(), length.As32())               // OR result
		MOVB(length.As8(), Mem{Base: dstBase, Disp: 0})
	}
	if retval != nil {
		ADDQ(U8(2), retval) // i += 2
	}
	ADDQ(U8(2), dstBase) // dst += 2

	JMP(end)
}

// emitCopy writes a copy chunk and returns the number of bytes written.
//
// It assumes that:
//	dst is long enough to hold the encoded bytes
//	1 <= offset && offset <= math.MaxUint32
//	4 <= length && length <= 1 << 24

// genEmitCopy generates a standlone emitCopy
func (o options) genEmitCopy() {
	TEXT("emitCopy", NOSPLIT, "func(dst []byte, offset, length int) int")
	Doc("emitCopy writes a copy chunk and returns the number of bytes written.", "",
		"It assumes that:",
		"  dst is long enough to hold the encoded bytes",
		"  1 <= offset && offset <= math.MaxUint32",
		"  4 <= length && length <= 1 << 24", "")
	Pragma("noescape")

	dstBase, offset, length, retval := GP64(), GP64(), GP64(), GP64()

	//	i := 0
	XORQ(retval, retval)
	Load(Param("dst").Base(), dstBase)
	Load(Param("offset"), offset)
	Load(Param("length"), length)
	o.emitCopy("standalone", length, offset, retval, dstBase, nil, LabelRef("gen_emit_copy_end"))
	Label("gen_emit_copy_end")
	Store(retval, ReturnIndex(0))
	RET()
}

// emitCopy writes a copy chunk and returns the number of bytes written.
//
// It assumes that:
//	dst is long enough to hold the encoded bytes
//	1 <= offset && offset <= math.MaxUint32
//	4 <= length && length <= 1 << 24

// genEmitCopy generates a standlone emitCopy
func (o options) genEmitCopyNoRepeat() {
	TEXT("emitCopyNoRepeat", NOSPLIT, "func(dst []byte, offset, length int) int")
	Doc("emitCopyNoRepeat writes a copy chunk and returns the number of bytes written.", "",
		"It assumes that:",
		"  dst is long enough to hold the encoded bytes",
		"  1 <= offset && offset <= math.MaxUint32",
		"  4 <= length && length <= 1 << 24", "")
	Pragma("noescape")

	dstBase, offset, length, retval := GP64(), GP64(), GP64(), GP64()

	//	i := 0
	XORQ(retval, retval)

	Load(Param("dst").Base(), dstBase)
	Load(Param("offset"), offset)
	Load(Param("length"), length)
	o.emitCopy("standalone_snappy", length, offset, retval, dstBase, nil, "gen_emit_copy_end_snappy")
	Label("gen_emit_copy_end_snappy")
	Store(retval, ReturnIndex(0))
	RET()
}

const (
	tagLiteral = 0x00
	tagCopy1   = 0x01
	tagCopy2   = 0x02
	tagCopy4   = 0x03
)

// emitCopy can be used for inlining an emitCopy call.
// length is modified (and junk). dstBase is updated. retval is added to input.
// retval can be nil.
// Will jump to end label when finished.
// Uses 2 GP registers.
// dstLimit is optional. If set each iteration will check if dstLimit is reached.
// If so, it will jump to "end" without emitting everything.
func (o options) emitCopy(name string, length, offset, retval, dstBase, dstLimit reg.GPVirtual, end LabelRef) {
	Comment("emitCopy")

	checkDst := func(reg reg.GPVirtual) {
		if dstLimit != nil {
			CMPQ(reg, dstLimit)
			JAE(end)
		}
	}

	if o.maxOffset >= 65536 {
		//if offset >= 65536 {
		CMPL(offset.As32(), U32(65536))
		JB(LabelRef("two_byte_offset_" + name))

		// offset is >= 65536
		//	if length <= 64 goto four_bytes_remain_
		Label("four_bytes_loop_back_" + name)
		CMPL(length.As32(), U8(64))
		JBE(LabelRef("four_bytes_remain_" + name))

		if !o.skipOutput {
			// Emit a length 64 copy, encoded as 5 bytes.
			//		dst[0] = 63<<2 | tagCopy4
			MOVB(U8(63<<2|tagCopy4), Mem{Base: dstBase})
			//		dst[4] = uint8(offset >> 24)
			//		dst[3] = uint8(offset >> 16)
			//		dst[2] = uint8(offset >> 8)
			//		dst[1] = uint8(offset)
			MOVL(offset.As32(), Mem{Base: dstBase, Disp: 1})
		}
		//		length -= 64
		LEAL(Mem{Base: length, Disp: -64}, length.As32())
		if retval != nil {
			ADDQ(U8(5), retval) // i+=5
		}
		ADDQ(U8(5), dstBase) // dst+=5

		//	if length >= 4 {
		CMPL(length.As32(), U8(4))
		JB(LabelRef("four_bytes_remain_" + name))

		// Emit remaining as repeats
		//	return 5 + emitRepeat(dst[5:], offset, length)
		// Inline call to emitRepeat. Will jump to end
		if !o.snappy {
			o.emitRepeat(name+"_emit_copy", length, offset, retval, dstBase, end, false)
		} else {
			checkDst(dstBase)
			JMP(LabelRef("four_bytes_loop_back_" + name))
		}

		Label("four_bytes_remain_" + name)
		//	if length == 0 {
		//		return i
		//	}
		TESTL(length.As32(), length.As32())
		JZ(end)

		// Emit a copy, offset encoded as 4 bytes.
		//	dst[i+0] = uint8(length-1)<<2 | tagCopy4
		//	dst[i+1] = uint8(offset)
		//	dst[i+2] = uint8(offset >> 8)
		//	dst[i+3] = uint8(offset >> 16)
		//	dst[i+4] = uint8(offset >> 24)
		tmp := GP64()
		XORL(tmp.As32(), tmp.As32())
		// Use displacement to subtract 1 from upshifted length.
		if !o.skipOutput {
			LEAL(Mem{Base: tmp, Disp: -(1 << 2) | tagCopy4, Index: length, Scale: 4}, length.As32())
			MOVB(length.As8(), Mem{Base: dstBase})
			MOVL(offset.As32(), Mem{Base: dstBase, Disp: 1})
		}
		//	return i + 5
		if retval != nil {
			ADDQ(U8(5), retval)
		}
		ADDQ(U8(5), dstBase)
		JMP(end)
	}
	Label("two_byte_offset_" + name)
	// Offset no more than 2 bytes.

	//if length > 64 {
	CMPL(length.As32(), U8(64))
	JBE(LabelRef("two_byte_offset_short_" + name))

	// if offset < 2048 {
	if !o.snappy {
		CMPL(offset.As32(), U32(2048))
		JAE(LabelRef("long_offset_short_" + name))
		// Emit a length 8 copy, encoded as 2 bytes.
		// Emit remaining as repeat value (minimum 4 bytes).
		// dst[1] = uint8(offset)
		// dst[0] = uint8(offset>>8)<<5 | uint8(8-4)<<2 | tagCopy1
		if !o.skipOutput {
			tmp := GP64()
			MOVL(U32(tagCopy1), tmp.As32())
			// Use scale and displacement to shift and subtract values from length.
			LEAL(Mem{Base: tmp, Disp: (8 - 4) << 2}, tmp.As32())
			MOVB(offset.As8(), Mem{Base: dstBase, Disp: 1}) // Store offset lower byte
			tmp2 := GP64()
			MOVL(offset.As32(), tmp2.As32())
			SHRL(U8(8), tmp2.As32())     // Remove lower
			SHLL(U8(5), tmp2.As32())     // Shift back up
			ORL(tmp2.As32(), tmp.As32()) // OR result
			MOVB(tmp.As8(), Mem{Base: dstBase, Disp: 0})
		}
		if retval != nil {
			ADDQ(U8(2), retval) // i += 2
		}
		ADDQ(U8(2), dstBase)       // dst += 2
		SUBL(U8(8), length.As32()) // length  -= 8
		// emitRepeat(dst[2:], offset, length)
		o.emitRepeat(name+"_emit_copy_short_2b", length, offset, retval, dstBase, end, true)

		Label("long_offset_short_" + name)
	}

	// Emit a length 60 copy, encoded as 3 bytes.
	// Emit remaining as repeat value (minimum 4 bytes).
	//	dst[2] = uint8(offset >> 8)
	//	dst[1] = uint8(offset)
	//	dst[0] = 59<<2 | tagCopy2
	if !o.skipOutput {
		MOVB(U8(59<<2|tagCopy2), Mem{Base: dstBase})
		MOVW(offset.As16(), Mem{Base: dstBase, Disp: 1})
	}
	//	length -= 60
	LEAL(Mem{Base: length, Disp: -60}, length.As32())

	// Emit remaining as repeats, at least 4 bytes remain.
	//	return 3 + emitRepeat(dst[3:], offset, length)
	//}
	ADDQ(U8(3), dstBase)
	if retval != nil {
		ADDQ(U8(3), retval)
	}
	// Inline call to emitRepeat. Will jump to end
	if !o.snappy {
		o.emitRepeat(name+"_emit_copy_short", length, offset, retval, dstBase, end, false)
	} else {
		checkDst(dstBase)
		JMP(LabelRef("two_byte_offset_" + name))
	}

	Label("two_byte_offset_short_" + name)

	// Create a length * 4 as early as possible.
	length4 := GP32()
	MOVL(length.As32(), length4)
	SHLL(U8(2), length4)

	//if length >= 12 || offset >= 2048 {
	CMPL(length.As32(), U8(12))
	JAE(LabelRef("emit_copy_three_" + name))
	if o.maxOffset >= 2048 {
		CMPL(offset.As32(), U32(2048))
		JAE(LabelRef("emit_copy_three_" + name))
	}
	// Emit the remaining copy, encoded as 2 bytes.
	// dst[1] = uint8(offset)
	// dst[0] = uint8(offset>>8)<<5 | uint8(length-4)<<2 | tagCopy1
	// Use scale and displacement to shift and subtract values from length.
	if !o.skipOutput {
		LEAL(Mem{Base: length4, Disp: -(4 << 2) | tagCopy1}, length4.As32())
		MOVB(offset.As8(), Mem{Base: dstBase, Disp: 1}) // Store offset lower byte
		SHRL(U8(8), offset.As32())                      // Remove lower
		SHLL(U8(5), offset.As32())                      // Shift back up
		ORL(offset.As32(), length4.As32())              // OR result
		MOVB(length4.As8(), Mem{Base: dstBase, Disp: 0})
	}
	if retval != nil {
		ADDQ(U8(2), retval) // i += 2
	}
	ADDQ(U8(2), dstBase) // dst += 2
	// return 2
	JMP(end)

	Label("emit_copy_three_" + name)
	//	// Emit the remaining copy, encoded as 3 bytes.
	//	dst[2] = uint8(offset >> 8)
	//	dst[1] = uint8(offset)
	//	dst[0] = uint8(length-1)<<2 | tagCopy2

	if !o.skipOutput {
		LEAL(Mem{Base: length4, Disp: -(1 << 2) | tagCopy2}, length4.As32())
		MOVB(length4.As8(), Mem{Base: dstBase})
		MOVW(offset.As16(), Mem{Base: dstBase, Disp: 1})
	}
	//	return 3
	if retval != nil {
		ADDQ(U8(3), retval) // i += 3
	}
	ADDQ(U8(3), dstBase) // dst += 3
	JMP(end)
}

// func memmove(to, from unsafe.Pointer, n uintptr)
// src and dst may not overlap.
// Non AVX uses 2 GP register, 16 SSE2 registers.
// AVX uses 4 GP registers 16 AVX/SSE registers.
// All passed registers may be updated.
// Length must be 1 -> 64 bytes
func (o options) genMemMoveShort(name string, dst, src, length reg.GPVirtual, end LabelRef) {
	if o.skipOutput {
		JMP(end)
		return
	}
	Comment("genMemMoveShort")
	AX, CX := GP64(), GP64()
	name += "_memmove_"

	// Only enable if length can be 0.
	if false {
		TESTQ(length, length)
		JEQ(end)
	}
	assert(func(ok LabelRef) {
		CMPQ(length, U8(64))
		JBE(ok)
	})
	assert(func(ok LabelRef) {
		TESTQ(length, length)
		JNZ(ok)
	})

	if o.outputMargin <= 3 {
		CMPQ(length, U8(3))
		JB(LabelRef(name + "move_1or2"))
		JE(LabelRef(name + "move_3"))
	} else if o.outputMargin >= 4 && o.outputMargin < 8 {
		CMPQ(length, U8(4))
		JBE(LabelRef(name + "move_4"))
	}
	if o.outputMargin <= 7 {
		CMPQ(length, U8(8))
		JB(LabelRef(name + "move_4through7"))
	} else if o.outputMargin >= 8 {
		CMPQ(length, U8(8))
		JBE(LabelRef(name + "move_8"))
	}
	CMPQ(length, U8(16))
	JBE(LabelRef(name + "move_8through16"))
	CMPQ(length, U8(32))
	JBE(LabelRef(name + "move_17through32"))
	if debug {
		CMPQ(length, U8(64))
		JBE(LabelRef(name + "move_33through64"))
		INT(U8(3))
	}
	JMP(LabelRef(name + "move_33through64"))

	//genMemMoveLong(name, dst, src, length, end)

	if o.outputMargin <= 3 {
		Label(name + "move_1or2")
		MOVB(Mem{Base: src}, AX.As8())
		MOVB(Mem{Base: src, Disp: -1, Index: length, Scale: 1}, CX.As8())
		MOVB(AX.As8(), Mem{Base: dst})
		MOVB(CX.As8(), Mem{Base: dst, Disp: -1, Index: length, Scale: 1})
		JMP(end)

		Label(name + "move_3")
		MOVW(Mem{Base: src}, AX.As16())
		MOVB(Mem{Base: src, Disp: 2}, CX.As8())
		MOVW(AX.As16(), Mem{Base: dst})
		MOVB(CX.As8(), Mem{Base: dst, Disp: 2})
		JMP(end)
	}

	if o.outputMargin >= 4 && o.outputMargin < 8 {
		// Use single move.
		Label(name + "move_4")
		MOVL(Mem{Base: src}, AX.As32())
		MOVL(AX.As32(), Mem{Base: dst})
		JMP(end)
	}
	if o.outputMargin < 8 {
		Label(name + "move_4through7")
		MOVL(Mem{Base: src}, AX.As32())
		MOVL(Mem{Base: src, Disp: -4, Index: length, Scale: 1}, CX.As32())
		MOVL(AX.As32(), Mem{Base: dst})
		MOVL(CX.As32(), Mem{Base: dst, Disp: -4, Index: length, Scale: 1})
		JMP(end)
	} else {
		// Use single move.
		Label(name + "move_8")
		MOVQ(Mem{Base: src}, AX)
		MOVQ(AX, Mem{Base: dst})
		JMP(end)
	}

	Label(name + "move_8through16")
	MOVQ(Mem{Base: src}, AX)
	MOVQ(Mem{Base: src, Disp: -8, Index: length, Scale: 1}, CX)
	MOVQ(AX, Mem{Base: dst})
	MOVQ(CX, Mem{Base: dst, Disp: -8, Index: length, Scale: 1})
	JMP(end)

	Label(name + "move_17through32")
	X0, X1, X2, X3 := XMM(), XMM(), XMM(), XMM()

	MOVOU(Mem{Base: src}, X0)
	MOVOU(Mem{Base: src, Disp: -16, Index: length, Scale: 1}, X1)
	MOVOU(X0, Mem{Base: dst})
	MOVOU(X1, Mem{Base: dst, Disp: -16, Index: length, Scale: 1})
	JMP(end)

	Label(name + "move_33through64")
	MOVOU(Mem{Base: src}, X0)
	MOVOU(Mem{Base: src, Disp: 16}, X1)
	MOVOU(Mem{Base: src, Disp: -32, Index: length, Scale: 1}, X2)
	MOVOU(Mem{Base: src, Disp: -16, Index: length, Scale: 1}, X3)
	MOVOU(X0, Mem{Base: dst})
	MOVOU(X1, Mem{Base: dst, Disp: 16})
	MOVOU(X2, Mem{Base: dst, Disp: -32, Index: length, Scale: 1})
	MOVOU(X3, Mem{Base: dst, Disp: -16, Index: length, Scale: 1})
	JMP(end)
}

// func genMemMoveLong(to, from unsafe.Pointer, n uintptr)
// src and dst may not overlap.
// length must be >= 64 bytes.
// Non AVX uses 2 GP register, 16 SSE2 registers.
// AVX uses 4 GP registers 16 AVX/SSE registers.
// All passed registers may be updated.
func (o options) genMemMoveLong(name string, dst, src, length reg.GPVirtual, end LabelRef) {
	if o.skipOutput {
		JMP(end)
		return
	}
	Comment("genMemMoveLong")
	name += "large_"

	assert(func(ok LabelRef) {
		CMPQ(length, U8(64))
		JAE(ok)
	})

	// These are disabled.
	// AVX is ever so slightly faster, but it is disabled for simplicity.
	const branchLoops = false
	const avx = false && branchLoops
	if branchLoops {
		CMPQ(length, U8(128))
		JBE(LabelRef(name + "move_65through128"))
		CMPQ(length, U32(256))
		JBE(LabelRef(name + "move_129through256"))
		if avx {
			JMP(LabelRef(name + "avxUnaligned"))
		} else {
			JMP(LabelRef(name + "forward_sse"))
		}

		X0, X1, X2, X3, X4, X5, X6, X7 := XMM(), XMM(), XMM(), XMM(), XMM(), XMM(), XMM(), XMM()
		X8, X9, X10, X11, X12, X13, X14, X15 := XMM(), XMM(), XMM(), XMM(), XMM(), XMM(), XMM(), XMM()
		Label(name + "move_65through128")
		MOVOU(Mem{Base: src}, X0)
		MOVOU(Mem{Base: src, Disp: 16}, X1)
		MOVOU(Mem{Base: src, Disp: 32}, X2)
		MOVOU(Mem{Base: src, Disp: 48}, X3)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -64}, X12)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -48}, X13)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -32}, X14)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -16}, X15)
		MOVOU(X0, Mem{Base: dst})
		MOVOU(X1, Mem{Base: dst, Disp: 16})
		MOVOU(X2, Mem{Base: dst, Disp: 32})
		MOVOU(X3, Mem{Base: dst, Disp: 48})
		MOVOU(X12, Mem{Base: dst, Index: length, Scale: 1, Disp: -64})
		MOVOU(X13, Mem{Base: dst, Index: length, Scale: 1, Disp: -48})
		MOVOU(X14, Mem{Base: dst, Index: length, Scale: 1, Disp: -32})
		MOVOU(X15, Mem{Base: dst, Index: length, Scale: 1, Disp: -16})
		JMP(end)

		Label(name + "move_129through256")
		MOVOU(Mem{Base: src}, X0)
		MOVOU(Mem{Base: src, Disp: 16}, X1)
		MOVOU(Mem{Base: src, Disp: 32}, X2)
		MOVOU(Mem{Base: src, Disp: 48}, X3)
		MOVOU(Mem{Base: src, Disp: 64}, X4)
		MOVOU(Mem{Base: src, Disp: 80}, X5)
		MOVOU(Mem{Base: src, Disp: 96}, X6)
		MOVOU(Mem{Base: src, Disp: 112}, X7)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -128}, X8)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -112}, X9)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -96}, X10)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -80}, X11)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -64}, X12)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -48}, X13)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -32}, X14)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -16}, X15)
		MOVOU(X0, Mem{Base: dst})
		MOVOU(X1, Mem{Base: dst, Disp: 16})
		MOVOU(X2, Mem{Base: dst, Disp: 32})
		MOVOU(X3, Mem{Base: dst, Disp: 48})
		MOVOU(X4, Mem{Base: dst, Disp: 64})
		MOVOU(X5, Mem{Base: dst, Disp: 80})
		MOVOU(X6, Mem{Base: dst, Disp: 96})
		MOVOU(X7, Mem{Base: dst, Disp: 112})
		MOVOU(X8, Mem{Base: dst, Index: length, Scale: 1, Disp: -128})
		MOVOU(X9, Mem{Base: dst, Index: length, Scale: 1, Disp: -112})
		MOVOU(X10, Mem{Base: dst, Index: length, Scale: 1, Disp: -96})
		MOVOU(X11, Mem{Base: dst, Index: length, Scale: 1, Disp: -80})
		MOVOU(X12, Mem{Base: dst, Index: length, Scale: 1, Disp: -64})
		MOVOU(X13, Mem{Base: dst, Index: length, Scale: 1, Disp: -48})
		MOVOU(X14, Mem{Base: dst, Index: length, Scale: 1, Disp: -32})
		MOVOU(X15, Mem{Base: dst, Index: length, Scale: 1, Disp: -16})
		JMP(end)
		if avx {
			Label(name + "avxUnaligned")
			AX, CX, R8, R10 := GP64(), GP64(), GP64(), GP64()
			// Memory layout on the source side
			// src                                       CX
			// |<---------length before correction--------->|
			// |       |<--length corrected-->|             |
			// |       |                  |<--- AX  --->|
			// |<-R11->|                  |<-128 bytes->|
			// +----------------------------------------+
			// | Head  | Body             | Tail        |
			// +-------+------------------+-------------+
			// ^       ^                  ^
			// |       |                  |
			// Save head into Y4          Save tail into X5..X12
			//         |
			//         src+R11, where R11 = ((dst & -32) + 32) - dst
			// Algorithm:
			// 1. Unaligned save of the tail's 128 bytes
			// 2. Unaligned save of the head's 32  bytes
			// 3. Destination-aligned copying of body (128 bytes per iteration)
			// 4. Put head on the new place
			// 5. Put the tail on the new place
			// It can be important to satisfy processor's pipeline requirements for
			// small sizes as the cost of unaligned memory region copying is
			// comparable with the cost of main loop. So code is slightly messed there.
			// There is more clean implementation of that algorithm for bigger sizes
			// where the cost of unaligned part copying is negligible.
			// You can see it after gobble_big_data_fwd label.
			Y0, Y1, Y2, Y3, Y4 := YMM(), YMM(), YMM(), YMM(), YMM()

			LEAQ(Mem{Base: src, Index: length, Scale: 1}, CX)
			MOVQ(dst, R10)
			// CX points to the end of buffer so we need go back slightly. We will use negative offsets there.
			MOVOU(Mem{Base: CX, Disp: -0x80}, X5)
			MOVOU(Mem{Base: CX, Disp: -0x70}, X6)
			MOVQ(U32(0x80), AX)

			// Align destination address
			ANDQ(U32(0xffffffe0), dst)
			ADDQ(U8(32), dst)
			// Continue tail saving.
			MOVOU(Mem{Base: CX, Disp: -0x60}, X7)
			MOVOU(Mem{Base: CX, Disp: -0x50}, X8)
			// Make R8 delta between aligned and unaligned destination addresses.
			MOVQ(dst, R8)
			SUBQ(R10, R8)
			// Continue tail saving.
			MOVOU(Mem{Base: CX, Disp: -0x40}, X9)
			MOVOU(Mem{Base: CX, Disp: -0x30}, X10)
			// Let's make bytes-to-copy value adjusted as we've prepared unaligned part for copying.
			SUBQ(R8, length)
			// Continue tail saving.
			MOVOU(Mem{Base: CX, Disp: -0x20}, X11)
			MOVOU(Mem{Base: CX, Disp: -0x10}, X12)
			// The tail will be put on its place after main body copying.
			// It's time for the unaligned heading part.
			VMOVDQU(Mem{Base: src}, Y4)
			// Adjust source address to point past head.
			ADDQ(R8, src)
			SUBQ(AX, length)

			// Aligned memory copying there
			Label(name + "gobble_128_loop")
			VMOVDQU(Mem{Base: src}, Y0)
			VMOVDQU(Mem{Base: src, Disp: 0x20}, Y1)
			VMOVDQU(Mem{Base: src, Disp: 0x40}, Y2)
			VMOVDQU(Mem{Base: src, Disp: 0x60}, Y3)
			ADDQ(AX, src)
			VMOVDQA(Y0, Mem{Base: dst})
			VMOVDQA(Y1, Mem{Base: dst, Disp: 0x20})
			VMOVDQA(Y2, Mem{Base: dst, Disp: 0x40})
			VMOVDQA(Y3, Mem{Base: dst, Disp: 0x60})
			ADDQ(AX, dst)
			SUBQ(AX, length)
			JA(LabelRef(name + "gobble_128_loop"))
			// Now we can store unaligned parts.
			ADDQ(AX, length)
			ADDQ(dst, length)
			VMOVDQU(Y4, Mem{Base: R10})
			VZEROUPPER()
			MOVOU(X5, Mem{Base: length, Disp: -0x80})
			MOVOU(X6, Mem{Base: length, Disp: -0x70})
			MOVOU(X7, Mem{Base: length, Disp: -0x60})
			MOVOU(X8, Mem{Base: length, Disp: -0x50})
			MOVOU(X9, Mem{Base: length, Disp: -0x40})
			MOVOU(X10, Mem{Base: length, Disp: -0x30})
			MOVOU(X11, Mem{Base: length, Disp: -0x20})
			MOVOU(X12, Mem{Base: length, Disp: -0x10})
			JMP(end)

			return
		}
	}

	// Store start and end for sse_tail
	Label(name + "forward_sse")
	X0, X1, X2, X3, X4, X5 := XMM(), XMM(), XMM(), XMM(), XMM(), XMM()
	// X6, X7 :=  XMM(), XMM()
	//X8, X9, X10, X11 := XMM(), XMM(), XMM(), XMM()

	MOVOU(Mem{Base: src}, X0)
	MOVOU(Mem{Base: src, Disp: 16}, X1)
	MOVOU(Mem{Base: src, Disp: -32, Index: length, Scale: 1}, X2)
	MOVOU(Mem{Base: src, Disp: -16, Index: length, Scale: 1}, X3)

	// forward (only)
	dstAlign := GP64()
	bigLoops := GP64()
	MOVQ(length, bigLoops)
	SHRQ(U8(5), bigLoops) // bigLoops = length / 32

	MOVQ(dst, dstAlign)
	ANDL(U32(31), dstAlign.As32())
	srcOff := GP64()
	MOVQ(U32(64), srcOff)
	SUBQ(dstAlign, srcOff)

	// Move 32 bytes/loop
	DECQ(bigLoops)
	JA(LabelRef(name + "forward_sse_loop_32"))

	// Can be moved inside loop for less regs.
	srcPos := GP64()
	LEAQ(Mem{Disp: -32, Base: src, Scale: 1, Index: srcOff}, srcPos)
	dstPos := GP64()
	LEAQ(Mem{Disp: -32, Base: dst, Scale: 1, Index: srcOff}, dstPos)

	Label(name + "big_loop_back")

	MOVOU(Mem{Disp: 0, Base: srcPos}, X4)
	MOVOU(Mem{Disp: 16, Base: srcPos}, X5)

	MOVOA(X4, Mem{Disp: 0, Base: dstPos})
	MOVOA(X5, Mem{Disp: 16, Base: dstPos})
	ADDQ(U8(32), dstPos)
	ADDQ(U8(32), srcPos)
	ADDQ(U8(32), srcOff) // This could be outside the loop, but we lose a reg if we do.
	DECQ(bigLoops)
	JNA(LabelRef(name + "big_loop_back"))

	Label(name + "forward_sse_loop_32")
	MOVOU(Mem{Disp: -32, Base: src, Scale: 1, Index: srcOff}, X4)
	MOVOU(Mem{Disp: -16, Base: src, Scale: 1, Index: srcOff}, X5)
	MOVOA(X4, Mem{Disp: -32, Base: dst, Scale: 1, Index: srcOff})
	MOVOA(X5, Mem{Disp: -16, Base: dst, Scale: 1, Index: srcOff})
	ADDQ(U8(32), srcOff)
	CMPQ(length, srcOff)
	JAE(LabelRef(name + "forward_sse_loop_32"))

	// sse_tail patches up the beginning and end of the transfer.
	MOVOU(X0, Mem{Base: dst, Disp: 0})
	MOVOU(X1, Mem{Base: dst, Disp: 16})
	MOVOU(X2, Mem{Base: dst, Disp: -32, Index: length, Scale: 1})
	MOVOU(X3, Mem{Base: dst, Disp: -16, Index: length, Scale: 1})

	JMP(end)
	return
}

// genMatchLen generates standalone matchLen.
func (o options) genMatchLen() {
	TEXT("matchLen", NOSPLIT, "func(a, b []byte) int")
	Doc("matchLen returns how many bytes match in a and b", "",
		"It assumes that:",
		"  len(a) <= len(b)", "")
	Pragma("noescape")

	aBase, bBase, length := GP64(), GP64(), GP64()

	Load(Param("a").Base(), aBase)
	Load(Param("b").Base(), bBase)
	Load(Param("a").Len(), length)
	l := o.matchLen("standalone", aBase, bBase, length, LabelRef("gen_match_len_end"))
	Label("gen_match_len_end")
	if o.avx2 {
		VZEROUPPER()
	}
	Store(l.As64(), ReturnIndex(0))
	RET()
}

// matchLen returns the number of matching bytes of a and b.
// len is the maximum number of bytes to match.
// Will jump to end when done and returns the length.
// Uses 2 GP registers.
func (o options) matchLen(name string, a, b, len reg.GPVirtual, end LabelRef) reg.GPVirtual {
	Comment("matchLen")
	tmp, matched := GP64(), GP32()
	XORL(matched, matched)
	if o.avx2 {
		// Not faster...
		o.matchLenAVX2(name+"Avx2", a, b, len, LabelRef("avx2_continue_"+name), end, matched)
	}
	Label("avx2_continue_" + name)

	Label("matchlen_loopback_16_" + name)
	tmp2 := GP64()
	CMPL(len.As32(), U8(16))
	JB(LabelRef("matchlen_match8_" + name))
	MOVQ(Mem{Base: a, Index: matched, Scale: 1}, tmp)
	MOVQ(Mem{Base: a, Index: matched, Scale: 1, Disp: 8}, tmp2)
	XORQ(Mem{Base: b, Index: matched, Scale: 1}, tmp)
	JNZ(LabelRef("matchlen_bsf_8_" + name))
	XORQ(Mem{Base: b, Index: matched, Scale: 1, Disp: 8}, tmp2)
	JNZ(LabelRef("matchlen_bsf_16" + name))
	// All 8 byte matched, update and loop.
	LEAL(Mem{Base: len, Disp: -16}, len.As32())
	LEAL(Mem{Base: matched, Disp: 16}, matched)
	JMP(LabelRef("matchlen_loopback_16_" + name))

	Label("matchlen_bsf_16" + name)
	// Not all match.
	Comment("#ifdef GOAMD64_v3")
	// 2016 BMI                 :TZCNT r64, r64                        L:   0.57ns=  2.0c  T:   0.29ns=  1.00c
	//  315 AMD64               :BSF r64, r64                          L:   0.88ns=  3.1c  T:   0.86ns=  3.00c
	TZCNTQ(tmp2, tmp2)
	Comment("#else")
	BSFQ(tmp2, tmp2)
	Comment("#endif")

	SARQ(U8(3), tmp2)
	LEAL(Mem{Base: matched, Index: tmp2, Scale: 1, Disp: 8}, matched)
	JMP(end)

	Label("matchlen_match8_" + name)
	CMPL(len.As32(), U8(8))
	JB(LabelRef("matchlen_match4_" + name))
	MOVQ(Mem{Base: a, Index: matched, Scale: 1}, tmp)
	XORQ(Mem{Base: b, Index: matched, Scale: 1}, tmp)
	JNZ(LabelRef("matchlen_bsf_8_" + name))
	// All 8 byte matched, update and loop.
	LEAL(Mem{Base: len, Disp: -8}, len.As32())
	LEAL(Mem{Base: matched, Disp: 8}, matched)
	JMP(LabelRef("matchlen_match4_" + name))
	Label("matchlen_bsf_8_" + name)

	// Not all match.
	Comment("#ifdef GOAMD64_v3")
	// 2016 BMI                 :TZCNT r64, r64                        L:   0.57ns=  2.0c  T:   0.29ns=  1.00c
	//  315 AMD64               :BSF r64, r64                          L:   0.88ns=  3.1c  T:   0.86ns=  3.00c
	TZCNTQ(tmp, tmp)
	Comment("#else")
	BSFQ(tmp, tmp)
	Comment("#endif")

	SARQ(U8(3), tmp)
	LEAL(Mem{Base: matched, Index: tmp, Scale: 1}, matched)
	JMP(end)

	// Less than 8 bytes left.
	// Test 4 bytes...
	Label("matchlen_match4_" + name)
	CMPL(len.As32(), U8(4))
	JB(LabelRef("matchlen_match2_" + name))
	MOVL(Mem{Base: a, Index: matched, Scale: 1}, tmp.As32())
	CMPL(Mem{Base: b, Index: matched, Scale: 1}, tmp.As32())
	JNE(LabelRef("matchlen_match2_" + name))
	LEAL(Mem{Base: len.As32(), Disp: -4}, len.As32())
	LEAL(Mem{Base: matched, Disp: 4}, matched)

	// Test 2 bytes...
	Label("matchlen_match2_" + name)
	CMPL(len.As32(), U8(1))
	// If we don't have 1, branch appropriately
	JE(LabelRef("matchlen_match1_" + name))
	JB(end)
	// 2 or 3
	MOVW(Mem{Base: a, Index: matched, Scale: 1}, tmp.As16())
	CMPW(Mem{Base: b, Index: matched, Scale: 1}, tmp.As16())
	JNE(LabelRef("matchlen_match1_" + name))
	LEAL(Mem{Base: matched, Disp: 2}, matched)
	SUBL(U8(2), len.As32())
	JZ(end)

	// Test 1 byte...
	Label("matchlen_match1_" + name)
	MOVB(Mem{Base: a, Index: matched, Scale: 1}, tmp.As8())
	CMPB(Mem{Base: b, Index: matched, Scale: 1}, tmp.As8())
	JNE(end)
	LEAL(Mem{Base: matched, Disp: 1}, matched)
	JMP(end)
	return matched
}

// matchLen returns the number of matching bytes of a and b.
// len is the maximum number of bytes to match.
// Will jump to end when done and returns the length.
// Uses 3 GP registers.
// It is better on longer matches.
func (o options) matchLenAVX2(name string, a, b, len reg.GPVirtual, cont, end LabelRef, dst reg.GPVirtual) {
	Comment("matchLenAVX2")

	equalMaskBits := GP64()
	Label(name + "loop")
	{
		CMPQ(len, U8(32))
		JB(cont)
		Comment("load 32 bytes into YMM registers")
		adata := YMM()
		bdata := YMM()
		equalMaskBytes := YMM()
		VMOVDQU(Mem{Base: a}, adata)
		VMOVDQU(Mem{Base: b}, bdata)
		Comment("compare bytes in adata and bdata, like 'bytewise XNOR'",
			"if the byte is the same in adata and bdata, VPCMPEQB will store 0xFF in the same position in equalMaskBytes")
		VPCMPEQB(adata, bdata, equalMaskBytes)
		Comment("like convert byte to bit, store equalMaskBytes into general reg")
		VPMOVMSKB(equalMaskBytes, equalMaskBits.As32())
		CMPL(equalMaskBits.As32(), U32(0xffffffff))
		JNE(LabelRef(name + "cal_prefix"))
		ADDQ(U8(32), a)
		ADDQ(U8(32), b)
		ADDL(U8(32), dst)
		SUBQ(U8(32), len)
		JZ(end)
		JMP(LabelRef(name + "loop"))
	}

	Label(name + "cal_prefix")
	{
		NOTQ(equalMaskBits)
		if o.bmi1 {
			TZCNTQ(equalMaskBits, equalMaskBits)
		} else {
			BSFQ(equalMaskBits, equalMaskBits)
		}
		ADDL(equalMaskBits.As32(), dst)
	}
	JMP(end)
	return
}

func (o options) cvtLZ4BlockAsm(lz4s bool) {
	snap := "Asm"
	name := "lz4_s2_"
	srcAlgo := "LZ4"
	dstAlgo := "S2"
	if o.snappy {
		snap = "SnappyAsm"
		name = "lz4_snappy_"
		dstAlgo = "Snappy"
	}
	if lz4s {
		name = strings.ReplaceAll(name, "lz4", "lz4s")
		srcAlgo = "LZ4s"
	}
	TEXT("cvt"+srcAlgo+"Block"+snap, NOSPLIT, "func(dst, src []byte) (uncompressed int, dstUsed int)")
	Doc("cvt"+srcAlgo+"Block converts an "+srcAlgo+" block to "+dstAlgo, "")
	Pragma("noescape")
	o.outputMargin = 10
	o.maxOffset = math.MaxUint16

	const (
		errCorrupt     = -1
		errDstTooSmall = -2
	)
	dst, dstLen, src, srcLen, retval := GP64(), GP64(), GP64(), GP64(), GP64()

	// retval = 0
	XORQ(retval, retval)

	Load(Param("dst").Base(), dst)
	Load(Param("dst").Len(), dstLen)
	Load(Param("src").Base(), src)
	Load(Param("src").Len(), srcLen)
	srcEnd, dstEnd := GP64(), GP64()
	LEAQ(Mem{Base: src, Index: srcLen, Scale: 1, Disp: 0}, srcEnd)
	LEAQ(Mem{Base: dst, Index: dstLen, Scale: 1, Disp: -o.outputMargin}, dstEnd)
	lastOffset := GP64()
	if !o.snappy {
		XORQ(lastOffset, lastOffset)
	}

	checkSrc := func(reg reg.GPVirtual) {
		if debug {
			name := fmt.Sprintf(name+"ok_%d", rand.Int31())

			CMPQ(reg, srcEnd)
			JB(LabelRef(name))
			JMP(LabelRef(name + "corrupt"))
			Label(name)
		} else {
			CMPQ(reg, srcEnd)
			JAE(LabelRef(name + "corrupt"))
		}
	}
	checkDst := func(reg reg.GPVirtual) {
		CMPQ(reg, dstEnd)
		JAE(LabelRef(name + "dstfull"))
	}

	var lz4MinMatch = 4
	if lz4s {
		lz4MinMatch = 3
	}

	Label(name + "loop")
	checkSrc(src)
	checkDst(dst)
	token := GP64()
	MOVBQZX(Mem{Base: src}, token)
	ll, ml := GP64(), GP64()
	MOVQ(token, ll)
	MOVQ(token, ml)
	SHRQ(U8(4), ll)
	ANDQ(U8(0xf), ml)

	// If upper nibble is 15, literal length is extended
	{
		CMPQ(token, U8(0xf0))
		JB(LabelRef(name + "ll_end"))
		Label(name + "ll_loop")
		INCQ(src) // s++
		checkSrc(src)
		val := GP64()
		MOVBQZX(Mem{Base: src}, val)
		ADDQ(val, ll)
		CMPQ(val, U8(255))
		JEQ(LabelRef(name + "ll_loop"))
		Label(name + "ll_end")
	}

	// if s+ll >= len(src)
	endLits := GP64()
	LEAQ(Mem{Base: src, Index: ll, Scale: 1}, endLits)
	ADDQ(U8(lz4MinMatch), ml)
	checkSrc(endLits)
	INCQ(src) // s++
	INCQ(endLits)
	TESTQ(ll, ll)
	JZ(LabelRef(name + "lits_done"))
	{
		dstEnd := GP64()
		LEAQ(Mem{Base: dst, Index: ll, Scale: 1}, dstEnd)
		checkDst(dstEnd)
		o.outputMargin++
		ADDQ(ll, retval)
		o.emitLiteral(strings.TrimRight(name, "_"), ll, nil, dst, src, LabelRef(name+"lits_emit_done"), true)
		o.outputMargin--
		Label(name + "lits_emit_done")
		MOVQ(endLits, src)
	}
	Label(name + "lits_done")
	// if s == len(src) && ml == lz4MinMatch
	CMPQ(src, srcEnd)
	JNE(LabelRef(name + "match"))
	CMPQ(ml, U8(lz4MinMatch))
	JEQ(LabelRef(name + "done"))
	JMP(LabelRef(name + "corrupt"))

	Label(name + "match")
	if lz4s {
		CMPQ(ml, U8(lz4MinMatch))
		JEQ(LabelRef(name + "loop"))
	}
	// if s >= len(src)-2 {
	end := GP64()
	LEAQ(Mem{Base: src, Disp: 2}, end)
	checkSrc(end)
	offset := GP64()
	MOVWQZX(Mem{Base: src}, offset)
	MOVQ(end, src) // s = s + 2

	if debug {
		// if offset == 0 {
		TESTQ(offset, offset)
		JNZ(LabelRef(name + "c1"))
		JMP(LabelRef(name + "corrupt"))

		Label(name + "c1")

		// if int(offset) > uncompressed {
		CMPQ(offset, retval)
		JB(LabelRef(name + "c2"))
		JMP(LabelRef(name + "corrupt"))

		Label(name + "c2")

	} else {
		// if offset == 0 {
		TESTQ(offset, offset)
		JZ(LabelRef(name + "corrupt"))

		// if int(offset) > uncompressed {
		CMPQ(offset, retval)
		JA(LabelRef(name + "corrupt"))
	}

	// if ml == lz4MinMatch+15 {
	{
		CMPQ(ml, U8(lz4MinMatch+15))
		JNE(LabelRef(name + "ml_done"))

		Label(name + "ml_loop")
		val := GP64()
		MOVBQZX(Mem{Base: src}, val)
		INCQ(src)     // s++
		ADDQ(val, ml) // ml += val
		checkSrc(src)
		CMPQ(val, U8(255))
		JEQ(LabelRef(name + "ml_loop"))
	}
	Label(name + "ml_done")

	// uncompressed += ml
	ADDQ(ml, retval)
	if !o.snappy {
		CMPQ(offset, lastOffset)
		JNE(LabelRef(name + "docopy"))
		// Offsets can only be 16 bits
		{
			// emitRepeat16(dst[d:], offset, ml)
			o.emitRepeat("lz4_s2", ml, offset, nil, dst, LabelRef(name+"loop"), false)
		}
	}
	Label(name + "docopy")
	{
		// emitCopy16(dst[d:], offset, ml)
		if !o.snappy {
			MOVQ(offset, lastOffset)
		}
		o.emitCopy("lz4_s2", ml, offset, nil, dst, dstEnd, LabelRef(name+"loop"))
	}

	Label(name + "done")
	{
		tmp := GP64()
		Load(Param("dst").Base(), tmp)
		SUBQ(tmp, dst)
		Store(retval, ReturnIndex(0))
		Store(dst, ReturnIndex(1))
		RET()
	}
	Label(name + "corrupt")
	{
		tmp := GP64()
		if debug {
			tmp := GP64()
			Load(Param("dst").Base(), tmp)
			SUBQ(tmp, dst)
			Store(dst, ReturnIndex(1))
		}
		XORQ(tmp, tmp)
		LEAQ(Mem{Base: tmp, Disp: errCorrupt}, retval)
		Store(retval, ReturnIndex(0))
		RET()
	}

	Label(name + "dstfull")
	{
		tmp := GP64()
		XORQ(tmp, tmp)
		LEAQ(Mem{Base: tmp, Disp: errDstTooSmall}, retval)
		Store(retval, ReturnIndex(0))
		RET()
	}
}
