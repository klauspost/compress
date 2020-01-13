//+build generate

//go:generate go run gen.go -out encodeblock_amd64.s -stubs encodeblock_amd64.go

package main

import (
	"fmt"

	. "github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/buildtags"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

func main() {
	Constraint(buildtags.Not("appengine").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())
	Constraint(buildtags.Term("gc").ToConstraint())

	genEncodeBlockAsm("encodeBlockAsm", 16, 6)
	genEmitLiteral()
	genEmitRepeat()
	genEmitCopy()
	genMatchLen()
	Generate()
}

func genEncodeBlockAsm(name string, tableBits, skipLog int) {
	TEXT(name, NOSPLIT, "func(dst, src []byte) int")
	Doc(name+" encodes a non-empty src to a guaranteed-large-enough dst.",
		"It assumes that the varint-encoded length of the decompressed bytes has already been written.", "")
	Pragma("noescape")

	// "var table [maxTableSize]uint32" takes up 65536 bytes of stack space. An
	// extra 56 bytes, to call other functions, and an extra 64 bytes, to spill
	// local variables (registers) during calls gives 65536 + 56 + 64 = 65656.
	var (
		tableSize = 1 << tableBits
		//tableMask  = tableSize - 1
		baseStack  = 56
		extraStack = 64
		allocStack = baseStack + extraStack + tableSize
	)
	// Memzero needs at least 128 bytes.
	if tableSize < 128 {
		panic("tableSize must be at least 128 bytes")
	}

	lenSrcBasic, err := Param("src").Len().Resolve()
	if err != nil {
		panic(err)
	}
	lenSrc := lenSrcBasic.Addr

	stack := AllocLocal(allocStack)
	table := stack.Offset(allocStack - tableSize)

	tmpStack := baseStack
	// Bail if we can't compress to at least this.
	dstLimitPtr := stack.Offset(tmpStack)
	tmpStack += 8
	// sLimit is when to stop looking for offset/length copies.
	sLimit := stack.Offset(tmpStack)
	tmpStack += 4
	nextEmit := stack.Offset(tmpStack)
	tmpStack += 4
	repeat := stack.Offset(tmpStack)
	tmpStack += 4
	nextSTemp := stack.Offset(tmpStack)
	tmpStack += 4
	dstBaseBasic, err := Param("dst").Base().Resolve()
	if err != nil {
		panic(err)
	}
	dstBase := dstBaseBasic.Addr

	if tmpStack > extraStack+baseStack {
		panic(fmt.Sprintf("tmp stack exceeded", tmpStack))
	}
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

		// nextEmit is where in src the next emitLiteral should start from.
		MOVL(iReg.As32(), nextEmit)
	}

	{
		const inputMargin = 8
		tmp, tmp2, tmp3 := GP64(), GP64(), GP64()
		MOVQ(lenSrc, tmp)
		LEAQ(Mem{Base: tmp, Disp: -5}, tmp2)
		// sLimit := lenMem{Base:src} - inputMargin
		LEAQ(Mem{Base: tmp, Disp: -inputMargin}, tmp3)
		// dstLimit := lenMem{Base:src} - lenMem{Base:src}>>5 - 5
		SHRQ(U8(5), tmp)
		SUBL(tmp2.As32(), tmp.As32())
		MOVL(tmp3.As32(), sLimit)
		LEAQ(dstBase.Idx(tmp, 1), tmp)
		MOVQ(tmp, dstLimitPtr)
	}

	// s = 1
	s := GP64()
	MOVB(U8(1), s.As8())
	// repeat = 1
	MOVL(s.As32(), repeat)

	src := GP64()
	Load(Param("src").Base(), src)

	// Load cv
	Label("main_loop_" + name)
	{
		Label("search_loop_" + name)
		candidate := GP64().As32()
		{
			cv := GP64()
			MOVQ(Mem{Base: src, Index: s, Scale: 1}, cv)
			nextS := GP64()
			// nextS := s + (s-nextEmit)>>6 + 4
			{
				tmp := GP64()
				MOVL(nextEmit, tmp.As32())
				SUBL(s.As32(), tmp.As32())
				SHRL(U8(skipLog), tmp.As32())
				LEAQ(Mem{Base: s, Disp: 4, Index: tmp, Scale: 1}, nextS)
			}
			// if nextS > sLimit {goto emitRemainder}
			// FIXME: failed to allocate registers???
			if false {
				tmp := GP64()
				MOVL(sLimit, tmp.As32())
				CMPL(nextS.As32(), tmp.As32())
				JGT(LabelRef("emit_remainder_" + name))
			}
			// move nextS to stack.
			MOVL(nextS.As32(), nextSTemp)

			candidate2 := GP64().As32()
			hasher := hash6(tableBits)
			{
				hash0, hash1 := GP64(), GP64()
				MOVQ(cv, hash0)
				MOVQ(cv, hash1)
				SHRQ(U8(8), hash1)
				hasher.hash(hash0)
				hasher.hash(hash1)
				MOVL(table.Idx(hash0, 1), candidate)
				MOVL(table.Idx(hash1, 1), candidate2)
				MOVL(s.As32(), table.Idx(hash0, 1))
				tmp := GP64()
				MOVQ(s, tmp)
				DECQ(tmp)
				MOVL(tmp.As32(), table.Idx(hash1, 1))
			}
			// hash2 := hash6(cv>>16, tableBits)
			hash2 := GP64()
			{
				MOVQ(cv, hash2)
				SHRQ(U8(16), hash2)
				hasher.hash(hash2)
			}
			// Check repeat at offset checkRep
			const checkRep = 1
			{
				// if uint32(cv>>(checkRep*8)) == load32(src, s-repeat+checkRep) {
				left, right, rep := GP64(), GP64(), GP64()
				MOVL(repeat, rep.As32())
				MOVQ(s, right)
				SUBQ(right, rep)
				MOVL(Mem{Base: src, Index: rep, Disp: checkRep}, right.As32())
				MOVQ(cv, left)
				SHLQ(U8(checkRep*8), left)
				CMPL(left.As32(), right.As32())
				JNE(LabelRef("no_repeat_found_" + name))
				{
					base := GP64()
					LEAQ(Mem{Base: s, Disp: 1}, base)
					// Extend back
					{
						i := rep
						ne := GP64().As32()
						MOVL(nextEmit, ne)
						TESTQ(i.As64(), i.As64())
						JZ(LabelRef("repeat_extend_back_end_" + name))

						// I is tested when decremented, so we loop back here.
						Label("repeat_extend_back_loop_" + name)
						CMPL(base.As32(), ne)
						JG(LabelRef("repeat_extend_back_end_" + name))
						// if src[i-1] == src[base-1]
						tmp, tmp2 := GP64(), GP64()
						MOVB(Mem{Base: src, Index: i, Scale: 1, Disp: -1}, tmp.As8())
						MOVB(Mem{Base: src, Index: base, Scale: 1, Disp: -1}, tmp2.As8())
						CMPB(tmp.As8(), tmp2.As8())
						JNE(LabelRef("repeat_extend_back_end_" + name))
						LEAQ(Mem{Base: base, Disp: -1}, base)
						DECQ(i)
						JZ(LabelRef("repeat_extend_back_end_" + name))
						JMP(LabelRef("repeat_extend_back_loop_" + name))
					}
					Label("repeat_extend_back_end_" + name)
					// Base is now at start.
					// d += emitLiteral(dst[d:], src[nextEmit:base])
					emitLiterals(nextEmit, base, src, dstBase, "repeat_emit_"+name)

					// Extend forward
					{
						// s += 4 + checkRep
						ADDQ(U8(4+checkRep), s)

						// candidate := s - repeat + 4 + checkRep
						MOVL(repeat, candidate)
						SUBL(s.As32(), candidate)
						{
							// srcLeft = sLimit - s
							srcLeft := GP64()
							MOVQ(s, srcLeft)
							SUBQ(sLimit, srcLeft)
							// Forward address
							forwardStart := Mem{Base: src, Index: s, Scale: 1}
							// End address
							backStart := Mem{Base: src, Index: candidate, Scale: 1}
							length := matchLen("repeat_extend", forwardStart, backStart, srcLeft, LabelRef("repeat_extend_forward_end_"+name))
							Label("repeat_extend_forward_end_" + name)
							// s+= length
							ADDQ(length, s)
						}
					}
					// Emit
					// FIXME: failed to allocate registers. May be legit.
					if false {
						// length = s-base
						length := base
						SUBQ(s, length)

						offsetVal := GP64()
						MOVQ(repeat, offsetVal)
						dst := GP64()
						MOVQ(dstBase, dst)

						// if nextEmit > 0
						tmp := GP32()
						MOVL(nextEmit, tmp.As32())
						TESTQ(tmp, tmp)
						JZ(LabelRef("repeat_as_copy_" + name))
						emitRepeat("match_repeat_", length, offsetVal, nil, dst, LabelRef("repeat_end_emit_"+name))
						Label("repeat_as_copy_" + name)
						emitCopy("repeat_as_copy_"+name, length, offsetVal, nil, dst, LabelRef("repeat_end_emit_"+name))
						Label("repeat_end_emit_" + name)
						// Store new dst and nextEmit
						MOVQ(dst, dstBase)
						MOVL(s.As32(), nextEmit)
					}
					// if s >= sLimit
					// can be omitted.
					// FIXME: failed to allocate registers???
					if false {
						tmp := GP64()
						MOVL(sLimit, tmp.As32())
						CMPL(s.As32(), tmp.As32())
						JGT(LabelRef("emit_remainder_" + name))
					}
					JMP(LabelRef("search_loop_" + name))
				}
			}
			Label("no_repeat_found_" + name)
			if false {
				CMPL(Mem{Base: src, Index: candidate, Scale: 1}, cv.As32())
				JEQ(LabelRef("candidate_match_" + name))
				// cv >>= 8
				SHRQ(U8(8), cv)

				// candidate = int(table[hash2])
				MOVL(table.Idx(hash2, 1), candidate)

				//if uint32(cv>>8) == load32(src, candidate2)
				CMPL(Mem{Base: src, Index: candidate2, Scale: 1}, cv.As32())
				JEQ(LabelRef("candidate2_match_" + name))

				// table[hash2] = uint32(s + 2)
				tmp := GP64()
				LEAQ(Mem{Base: s, Disp: -2}, tmp)
				MOVL(tmp.As32(), table.Idx(hash2, 1))

				// if uint32(cv>>16) == load32(src, candidate)
				SHRQ(U8(8), cv)
				CMPL(Mem{Base: src, Index: candidate, Scale: 1}, cv.As32())
				JEQ(LabelRef("candidate3_match_" + name))
				// s = nextS
				MOVL(nextSTemp, s.As32())
				JMP(LabelRef("search_loop_" + name))

				// Matches candidate3
				Label("candidate3_match_" + name)
				ADDQ(U8(2), s)
				JMP(LabelRef("candidate_match_" + name))

				Label("candidate2_match_" + name)
				// table[hash2] = uint32(s + 2)
				tmp = GP64()
				LEAQ(Mem{Base: s, Disp: -2}, tmp)
				MOVL(tmp.As32(), table.Idx(hash2, 1))
				// s++
				INCQ(s)
				MOVL(candidate2, candidate)
			} else {
				NOP()
			}
			Label("candidate_match_" + name)
			// We have a match at 's' with src offset in "candidate" that matches at least 4 bytes.
			// Extend backwards
			if false {
				ne := GP64().As32()
				MOVL(nextEmit, ne)
				TESTL(candidate, candidate)
				JZ(LabelRef("match_extend_back_end_" + name))

				// candidate is tested when decremented, so we loop back here.
				Label("match_extend_back_loop_" + name)
				CMPL(s.As32(), ne)
				JG(LabelRef("match_extend_back_end_" + name))
				// if src[candidate-1] == src[s-1]
				tmp, tmp2 := GP64(), GP64()
				MOVB(Mem{Base: src, Index: candidate, Scale: 1, Disp: -1}, tmp.As8())
				MOVB(Mem{Base: src, Index: s, Scale: 1, Disp: -1}, tmp2.As8())
				CMPB(tmp.As8(), tmp2.As8())
				JNE(LabelRef("match_extend_back_end_" + name))
				LEAQ(Mem{Base: s, Disp: -1}, s)
				DECL(candidate)
				JZ(LabelRef("match_extend_back_end_" + name))
				JMP(LabelRef("match_extend_back_loop_" + name))
			} else {
				NOP()
			}
			Label("match_extend_back_end_" + name)

			// Bail if we exceed the maximum size.
			{
				// tmp = s-nextEmit
				tmp := GP64()
				MOVQ(nextEmit, tmp)
				SUBQ(s, tmp)
				LEAQ(dstBase.Idx(tmp, 1), tmp)
				CMPQ(tmp, dstLimitPtr)
				JL(LabelRef("match_dst_size_check_" + name))
				XORQ(tmp, tmp)
				Store(tmp, ReturnIndex(0))
				RET()
			}
			Label("match_dst_size_check_" + name)
			if false {
				base := GP64()
				MOVL(candidate, base.As32())
				emitLiterals(nextEmit, base, src, dstBase, "match_emit_"+name)
			} else {
				NOP()
			}
			Label("match_nolit_loop_" + name)
			if false {
				base := GP64()
				MOVQ(s, base)
				// Update repeat
				{
					repeatVal := GP64()
					MOVL(repeat, repeatVal.As32())
					SUBL(s.As32(), repeatVal.As32())
					MOVL(repeatVal.As32(), repeat)
				}
				// s+=4, candidate+=4
				ADDQ(U8(4), s)
				ADDL(U8(4), candidate)
				// Extend the 4-byte match as long as possible and emit copy.
				if false {
					srcLeft := GP64()
					MOVQ(s, srcLeft)
					SUBQ(sLimit, srcLeft)
					length := matchLen("match_nolit_"+name,
						Mem{Base: src, Index: s, Scale: 1},
						Mem{Base: src, Index: candidate, Scale: 1},
						srcLeft,
						LabelRef("match_nolit_end_"+name),
					)
					Label("match_nolit_end_" + name)
					offset := GP64()
					MOVQ(repeat, offset)
					ADDQ(U8(4), length)
					dst := GP64()
					MOVQ(dstBase, dst)
					emitCopy("match_nolit_"+name, length, offset, nil, dst, LabelRef("match_nolit_emitcopy_end_"+name))
					Label("match_nolit_emitcopy_end_" + name)
					// s += length
					MOVQ(dst, dstBase)
					ADDQ(length, s)
					MOVL(s.As32(), nextEmit)
					CMPL(s.As32(), sLimit)
					JGE(LabelRef("emit_remainder_" + name))

					// Bail if we exceed the maximum size.
					{
						CMPQ(dst, dstLimitPtr)
						JL(LabelRef("match_nolit_dst_ok_" + name))
						tmp := GP64()
						XORQ(tmp, tmp)
						Store(tmp, ReturnIndex(0))
						RET()
						Label("match_nolit_dst_ok_" + name)
					}
				} else {
					NOP()
				}
				if false {
					// Check for an immediate match, otherwise start search at s+1
					x := GP64()
					// Index s-2
					MOVQ(Mem{Base: src, Index: s, Scale: 1, Disp: -2}, x)
					hasher := hash6(tableBits)
					hash0, hash1 := GP64(), GP64()
					MOVQ(x, hash0) // s-2
					SHRQ(U8(16), x)
					MOVQ(x, hash1) // s
					hasher.hash(hash0)
					hasher.hash(hash1)
					c0, c1 := GP64(), GP64()
					MOVL(table.Idx(hash0, 1), c0.As32())
					MOVL(table.Idx(hash1, 1), c1.As32())
					sm2 := GP64()
					LEAQ(Mem{Base: s, Disp: -2}, sm2)
					MOVL(sm2.As32(), table.Idx(hash0, 1))
					MOVL(s.As32(), table.Idx(hash1, 1))
					CMPL(Mem{Base: src, Index: hash1}, x.As32())
					JEQ(LabelRef("match_nolit_loop_" + name))

					INCQ(s)
				}
				JMP(LabelRef("search_loop_" + name))
			} else {
				NOP()
			}
		}
		_ = candidate
	}
	Label("emit_remainder_" + name)

	// TODO:
	// if d+lenMem{Base:src}-nextEmit > dstLimitPtr {	return 0

	// d += emitLiteral(dst[d:], src[nextEmit:])
	emitEnd := GP64()
	MOVQ(lenSrc, emitEnd)
	// FIXME: failed to allocate registers
	if false {
		emitLiterals(nextEmit, emitEnd, src, dstBase, name+"_emit_remainder")
	}
	Label("return_" + name)

	// FIXME, does not return size:
	length := GP64()
	XORQ(length, length)
	Store(length, ReturnIndex(0))
	RET()
}

// emitLiterals emits literals from nextEmit to base, updates nextEmit, dstBase.
// src & base are untouched.
func emitLiterals(nextEmit Mem, base reg.GPVirtual, src reg.GPVirtual, dstBase Mem, name string) {
	tmp, litLen, retval, dstBaseTmp, litBase := GP64(), GP64(), GP64(), GP64(), GP64()
	MOVQ(nextEmit, litLen)
	MOVQ(base, tmp)
	// litBase = src[nextEmit:]
	LEAQ(Mem{Base: src, Index: litLen, Scale: 1}, litBase)
	SUBQ(tmp, litLen) // litlen = base - nextEmit
	MOVQ(dstBase, dstBaseTmp)
	XORQ(retval, retval)
	MOVQ(base, nextEmit)
	emitLiteral(name, litLen, retval, dstBaseTmp, litBase, LabelRef("emit_literal_done_"+name))
	Label("emit_literal_done_" + name)
	// Store updated dstBase
	MOVQ(dstBaseTmp, dstBase)
}

type ptrSize struct {
	size uint8
	reg.Register
}

func (p ptrSize) Offset(off reg.Register) Mem {
	if p.size == 0 {
		p.size = 1
	}
	return Mem{Base: p, Index: off, Scale: p.size}
}

func (p ptrSize) OffsetInfo(dst, off reg.Register) {
	LEAQ(Mem{Base: p, Index: off, Scale: p.size}, dst)
	return
}

type hashGen struct {
	bytes     int
	tablebits int
	mulreg    reg.GPVirtual
}

// hash uses multiply to get a 'output' hash on the hash of the lowest 'bytes' bytes in value.
func hash6(tablebits int) hashGen {
	h := hashGen{
		bytes:     6,
		tablebits: tablebits,
		mulreg:    GP64(),
	}
	MOVQ(Imm(227718039650203), h.mulreg)
	return h
}

// hash uses multiply to get hash of the value.
func (h hashGen) hash(val reg.GPVirtual) {
	// Move value to top of register.
	SHLQ(U8(64-8*h.bytes), val)
	IMULQ(h.mulreg, val)
	// Move value to bottom
	SHRQ(U8(64-h.tablebits), val)
}

func genEmitLiteral() {
	TEXT("emitLiteral", NOSPLIT, "func(dst, lit []byte) int")
	Doc("emitLiteral writes a literal chunk and returns the number of bytes written.", "",
		"It assumes that:",
		"  dst is long enough to hold the encoded bytes",
		"  0 <= len(lit) && len(lit) <= math.MaxUint32", "")
	Pragma("noescape")

	dstBase, litBase, litLen, retval := GP64(), GP64(), GP64(), GP64()
	Load(Param("dst").Base(), dstBase)
	Load(Param("lit").Base(), litBase)
	Load(Param("lit").Len(), litLen)
	emitLiteral("standalone", litLen, retval, dstBase, litBase, "emit_literal_end_standalone")
	Label("emit_literal_end_standalone")
	Store(retval, ReturnIndex(0))
	RET()
}

// emitLiteral can be used for inlining an emitLiteral call.
// stack must have at least 32 bytes
func emitLiteral(name string, litLen, retval, dstBase, litBase reg.GPVirtual, end LabelRef) {
	n := GP64()
	n16 := GP64()

	// We always add litLen bytes
	MOVQ(litLen, retval)
	MOVQ(litLen, n)

	SUBL(U8(1), n.As32())
	// Return if AX was 0
	JC(end)

	// Find number of bytes to emit for tag.
	CMPL(n.As32(), U8(60))
	JLT(LabelRef("one_byte_" + name))
	CMPL(n.As32(), U32(1<<8))
	JLT(LabelRef("two_bytes_" + name))
	CMPL(n.As32(), U32(1<<16))
	JLT(LabelRef("three_bytes_" + name))
	CMPL(n.As32(), U32(1<<24))
	JLT(LabelRef("four_bytes_" + name))

	Label("five_bytes_" + name)
	MOVB(U8(252), Mem{Base: dstBase})
	MOVL(n.As32(), Mem{Base: dstBase, Disp: 1})
	ADDQ(U8(5), retval)
	ADDQ(U8(5), dstBase)
	JMP(LabelRef("memmove_" + name))

	Label("four_bytes_" + name)
	MOVQ(n, n16)
	SHRL(U8(16), n16.As32())
	MOVB(U8(248), Mem{Base: dstBase})
	MOVW(n.As16(), Mem{Base: dstBase, Disp: 1})
	MOVB(n16.As8(), Mem{Base: dstBase, Disp: 3})
	ADDQ(U8(4), retval)
	ADDQ(U8(4), dstBase)
	JMP(LabelRef("memmove_" + name))

	Label("three_bytes_" + name)
	MOVB(U8(0xf4), Mem{Base: dstBase})
	MOVW(n.As16(), Mem{Base: dstBase, Disp: 1})
	ADDQ(U8(3), retval)
	ADDQ(U8(3), dstBase)
	JMP(LabelRef("memmove_" + name))

	Label("two_bytes_" + name)
	MOVB(U8(0xf0), Mem{Base: dstBase})
	MOVB(n.As8(), Mem{Base: dstBase, Disp: 1})
	ADDQ(U8(2), retval)
	ADDQ(U8(2), dstBase)
	JMP(LabelRef("memmove_" + name))

	Label("one_byte_" + name)
	SHLB(U8(2), n.As8())
	MOVB(n.As8(), Mem{Base: dstBase})
	ADDQ(U8(1), retval)
	ADDQ(U8(1), dstBase)

	Label("memmove_" + name)

	// copy(dst[i:], lit)
	genMemMove2("emit_lit_memmove_"+name, dstBase, litBase, litLen, end, false)
}

// genEmitRepeat generates a standlone emitRepeat.
func genEmitRepeat() {
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
	emitRepeat("standalone", length, offset, retval, dstBase, LabelRef("gen_emit_repeat_end"))
	Label("gen_emit_repeat_end")
	Store(retval, ReturnIndex(0))
	RET()
}

// emitRepeat can be used for inlining an emitRepeat call.
// length >= 4 and < 1<<32
// length is modified. dstBase is updated. retval is added to input.
// retval can be nil.
// Will jump to end label when finished.
func emitRepeat(name string, length, offset, retval, dstBase reg.GPVirtual, end LabelRef) {
	Label("emit_repeat_again_" + name)
	tmp := GP64()
	MOVQ(length, tmp) // Copy length
	// length -= 4
	LEAQ(Mem{Base: length, Disp: -4}, length)

	// if length <= 4 (use copied value)
	CMPL(tmp.As32(), U8(8))
	JLE(LabelRef("repeat_two_" + name))

	// length < 8 && offset < 2048
	CMPL(tmp.As32(), U8(12))
	JGE(LabelRef("cant_repeat_two_offset_" + name))
	CMPL(offset.As32(), U32(2048))
	JLT(LabelRef("repeat_two_offset_" + name))

	const maxRepeat = ((1 << 24) - 1) + 65536
	Label("cant_repeat_two_offset_" + name)
	CMPL(length.As32(), U32((1<<8)+4))
	JLT(LabelRef("repeat_three_" + name)) // if length < (1<<8)+4
	CMPL(length.As32(), U32((1<<16)+(1<<8)))
	JLT(LabelRef("repeat_four_" + name)) // if length < (1 << 16) + (1 << 8)
	CMPL(length.As32(), U32(maxRepeat))
	JLT(LabelRef("repeat_five_" + name)) // If less than 24 bits to represent.

	// We have have more than 24 bits
	// Emit so we have at least 4 bytes left.
	LEAQ(Mem{Base: length, Disp: -(maxRepeat - 4)}, length) // length -= (maxRepeat - 4)
	MOVW(U16(7<<2|tagCopy1), Mem{Base: dstBase})            // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	MOVW(U16(65531), Mem{Base: dstBase, Disp: 2})           // 0xfffb
	MOVB(U8(255), Mem{Base: dstBase, Disp: 4})
	ADDQ(U8(5), dstBase)
	if retval != nil {
		ADDQ(U8(5), retval)
	}
	JMP(LabelRef("emit_repeat_again_" + name))

	// Must be able to be within 5 bytes.
	Label("repeat_five_" + name)
	LEAQ(Mem{Base: length, Disp: -65536}, length) // length -= 65536
	MOVQ(length, offset)
	MOVW(U16(7<<2|tagCopy1), Mem{Base: dstBase})     // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	MOVW(length.As16(), Mem{Base: dstBase, Disp: 2}) // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	SARQ(U8(16), offset)                             // offset = length >> 16
	MOVB(offset.As8(), Mem{Base: dstBase, Disp: 4})  // dst[4] = length >> 16
	if retval != nil {
		ADDQ(U8(5), retval) // i += 5
	}
	ADDQ(U8(5), dstBase) // dst += 5
	JMP(end)

	Label("repeat_four_" + name)
	LEAQ(Mem{Base: length, Disp: -256}, length)      // length -= 256
	MOVW(U16(6<<2|tagCopy1), Mem{Base: dstBase})     // dst[0] = 6<<2 | tagCopy1, dst[1] = 0
	MOVW(length.As16(), Mem{Base: dstBase, Disp: 2}) // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	if retval != nil {
		ADDQ(U8(4), retval) // i += 4
	}
	ADDQ(U8(4), dstBase) // dst += 4
	JMP(end)

	Label("repeat_three_" + name)
	LEAQ(Mem{Base: length, Disp: -4}, length)       // length -= 4
	MOVW(U16(5<<2|tagCopy1), Mem{Base: dstBase})    // dst[0] = 5<<2 | tagCopy1, dst[1] = 0
	MOVB(length.As8(), Mem{Base: dstBase, Disp: 2}) // dst[2] = uint8(length)
	if retval != nil {
		ADDQ(U8(3), retval) // i += 3
	}
	ADDQ(U8(3), dstBase) // dst += 3
	JMP(end)

	Label("repeat_two_" + name)
	// dst[0] = uint8(length)<<2 | tagCopy1, dst[1] = 0
	SHLL(U8(2), length.As32())
	ORL(U8(tagCopy1), length.As32())
	MOVW(length.As16(), Mem{Base: dstBase}) // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
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
	LEAQ(Mem{Base: tmp, Index: length, Scale: 4, Disp: tagCopy1}, length)
	MOVB(offset.As8(), Mem{Base: dstBase, Disp: 1}) // Store offset lower byte
	SARL(U8(8), offset.As32())                      // Remove lower
	SHLL(U8(5), offset.As32())                      // Shift back up
	ORL(offset.As32(), length.As32())               // OR result
	MOVB(length.As8(), Mem{Base: dstBase, Disp: 0})
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
func genEmitCopy() {
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
	emitCopy("standalone", length, offset, retval, dstBase, LabelRef("gen_emit_copy_end"))
	Label("gen_emit_copy_end")
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
func emitCopy(name string, length, offset, retval, dstBase reg.GPVirtual, end LabelRef) {
	//if offset >= 65536 {
	CMPL(offset.As32(), U32(65536))
	JL(LabelRef("two_byte_offset_" + name))

	// offset is >= 65536
	//	if length <= 64 goto four_bytes_remain_
	CMPL(length.As32(), U8(64))
	JLE(LabelRef("four_bytes_remain_" + name))

	// Emit a length 64 copy, encoded as 5 bytes.
	//		dst[0] = 63<<2 | tagCopy4
	MOVB(U8(63<<2|tagCopy4), Mem{Base: dstBase})
	//		dst[4] = uint8(offset >> 24)
	//		dst[3] = uint8(offset >> 16)
	//		dst[2] = uint8(offset >> 8)
	//		dst[1] = uint8(offset)
	MOVD(offset, Mem{Base: dstBase, Disp: 1})
	//		length -= 64
	LEAQ(Mem{Base: length, Disp: -64}, length)
	if retval != nil {
		ADDQ(U8(5), retval) // i+=5
	}
	ADDQ(U8(5), dstBase) // dst+=5

	//	if length >= 4 {
	CMPL(length.As32(), U8(4))
	JL(LabelRef("four_bytes_remain_" + name))

	// Emit remaining as repeats
	//	return 5 + emitRepeat(dst[5:], offset, length)
	// Inline call to emitRepeat. Will jump to end
	emitRepeat(name+"_emit_copy", length, offset, retval, dstBase, end)

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
	MOVB(U8(tagCopy4), tmp.As8())
	// Use displacement to subtract 1 from upshifted length.
	LEAQ(Mem{Base: tmp, Disp: -(1 << 2), Index: length, Scale: 4}, length)
	MOVB(length.As8(), Mem{Base: dstBase})
	MOVD(offset, Mem{Base: dstBase, Disp: 1})
	//	return i + 5
	if retval != nil {
		ADDQ(U8(5), retval)
	}
	ADDQ(U8(5), dstBase)
	JMP(end)

	Label("two_byte_offset_" + name)
	// Offset no more than 2 bytes.

	//if length > 64 {
	CMPL(length.As32(), U8(64))
	JLE(LabelRef("two_byte_offset_short_" + name))
	// Emit a length 60 copy, encoded as 3 bytes.
	// Emit remaining as repeat value (minimum 4 bytes).
	//	dst[2] = uint8(offset >> 8)
	//	dst[1] = uint8(offset)
	//	dst[0] = 59<<2 | tagCopy2
	MOVB(U8(59<<2|tagCopy2), Mem{Base: dstBase})
	MOVW(offset.As16(), Mem{Base: dstBase, Disp: 1})
	//	length -= 60
	LEAQ(Mem{Base: length, Disp: -60}, length)

	// Emit remaining as repeats, at least 4 bytes remain.
	//	return 3 + emitRepeat(dst[3:], offset, length)
	//}
	ADDQ(U8(3), dstBase)
	if retval != nil {
		ADDQ(U8(3), retval)
	}
	// Inline call to emitRepeat. Will jump to end
	emitRepeat(name+"_emit_copy_short", length, offset, retval, dstBase, end)

	Label("two_byte_offset_short_" + name)
	//if length >= 12 || offset >= 2048 {
	CMPL(length.As32(), U8(12))
	JGE(LabelRef("emit_copy_three_" + name))
	CMPL(offset.As32(), U32(2048))
	JGE(LabelRef("emit_copy_three_" + name))

	// Emit the remaining copy, encoded as 2 bytes.
	// dst[1] = uint8(offset)
	// dst[0] = uint8(offset>>8)<<5 | uint8(length-4)<<2 | tagCopy1
	tmp = GP64()
	MOVB(U8(tagCopy1), tmp.As8())
	// Use scale and displacement to shift and subtract values from length.
	LEAQ(Mem{Base: tmp, Index: length, Scale: 4, Disp: -(4 << 2)}, length)
	MOVB(offset.As8(), Mem{Base: dstBase, Disp: 1}) // Store offset lower byte
	SARL(U8(8), offset.As32())                      // Remove lower
	SHLL(U8(5), offset.As32())                      // Shift back up
	ORL(offset.As32(), length.As32())               // OR result
	MOVB(length.As8(), Mem{Base: dstBase, Disp: 0})
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
	tmp = GP64()
	MOVB(U8(tagCopy2), tmp.As8())
	LEAQ(Mem{Base: tmp, Disp: -(1 << 2), Index: length, Scale: 4}, length)
	MOVB(length.As8(), Mem{Base: dstBase})
	MOVW(offset.As16(), Mem{Base: dstBase, Disp: 1})
	//	return 3
	if retval != nil {
		ADDQ(U8(3), retval) // i += 3
	}
	ADDQ(U8(3), dstBase) // dst += 3
	JMP(end)
}

// func memmove(to, from unsafe.Pointer, n uintptr)
// to and from will be at the end, n will be 0.
// to and from may not overlap.
// Fairly simplistic for now, can ofc. be extended.
func genMemMove(name string, to, from, n reg.GPVirtual, end LabelRef) {
	tmp := GP64()
	MOVQ(n, tmp)
	// tmp = n/128
	SHRQ(U8(7), tmp)

	TESTQ(tmp, tmp)
	JZ(LabelRef("done_128_" + name))
	Label("loop_128_" + name)
	var xmmregs [8]reg.VecVirtual

	// Prefetch destination for next loop.
	// Prefetching source doesn't provide speedup.
	// This seems to give a small boost.
	const preOff = 128
	PREFETCHT0(Mem{Base: to, Disp: preOff})
	PREFETCHT0(Mem{Base: to, Disp: preOff + 64})

	for i := 0; i < 8; i++ {
		xmmregs[i] = XMM()
		MOVOU(Mem{Base: from}.Offset(i*16), xmmregs[i])
	}
	for i := 0; i < 8; i++ {
		MOVOU(xmmregs[i], Mem{Base: to}.Offset(i*16))
	}
	LEAQ(Mem{Base: n, Disp: -128}, n)
	ADDQ(U8(8*16), from)
	ADDQ(U8(8*16), to)
	DECQ(tmp)
	JNZ(LabelRef("loop_128_" + name))

	Label("done_128_" + name)
	MOVQ(n, tmp)
	// tmp = n/16
	SHRQ(U8(4), tmp)
	TESTQ(tmp, tmp)
	JZ(LabelRef("done_16_" + name))

	Label("loop_16_" + name)
	xmm := XMM()
	MOVOU(Mem{Base: from}, xmm)
	MOVOU(xmm, Mem{Base: to})
	LEAQ(Mem{Base: n, Disp: -16}, n)
	ADDQ(U8(16), from)
	ADDQ(U8(16), to)
	DECQ(tmp)
	JNZ(LabelRef("loop_16_" + name))
	Label("done_16_" + name)

	// TODO: Use REP; MOVSB somehow.
	TESTQ(n, n)
	JZ(end)
	Label("loop_1_" + name)
	MOVB(Mem{Base: from}, tmp.As8())
	MOVB(tmp.As8(), Mem{Base: to})
	INCQ(from)
	INCQ(to)
	DECQ(n)
	JNZ(LabelRef("loop_1_" + name))
}

// func memmove(to, from unsafe.Pointer, n uintptr)
// src and dst may not overlap.
func genMemMove2(name string, dst, src, length reg.GPVirtual, end LabelRef, avx bool) {
	AX, CX := GP64(), GP64()
	NOP()
	name += "_memmove_"
	Label(name + "tail")
	// move_129through256 or smaller work whether or not the source and the
	// destination memory regions overlap because they load all data into
	// registers before writing it back.  move_256through2048 on the other
	// hand can be used only when the memory regions don't overlap or the copy
	// direction is forward.
	//
	// BSR+branch table make almost all memmove/memclr benchmarks worse. Not worth doing.
	TESTQ(length, length)
	JEQ(end)
	CMPQ(length, U8(2))
	JBE(LabelRef(name + "move_1or2"))
	CMPQ(length, U8(4))
	JB(LabelRef(name + "move_3"))
	JBE(LabelRef(name + "move_4"))
	CMPQ(length, U8(8))
	JB(LabelRef(name + "move_5through7"))
	JE(LabelRef(name + "move_8"))
	CMPQ(length, U8(16))
	JBE(LabelRef(name + "move_9through16"))
	CMPQ(length, U8(32))
	JBE(LabelRef(name + "move_17through32"))
	CMPQ(length, U8(64))
	JBE(LabelRef(name + "move_33through64"))
	CMPQ(length, U8(128))
	JBE(LabelRef(name + "move_65through128"))
	CMPQ(length, U32(256))
	JBE(LabelRef(name + "move_129through256"))

	if avx {
		JMP(LabelRef(name + "avxUnaligned"))
	} else {
		if false {
			// Don't check length for now.
			Label(name + "forward")
			CMPQ(length, U32(2048))
			JLS(LabelRef(name + "move_256through2048"))

			genMemMove(name+"fallback", dst, src, length, end)
		} else {
			JMP(LabelRef(name + "move_256through2048"))
		}
	}
	/*
			// If REP MOVSB isn't fast, don't use it
			// FIXME: internal∕cpu·X86+const_offsetX86HasERMS(SB)
			// CMPB(U8(1), U8(1)) // enhanced REP MOVSB/STOSB
			JMP(LabelRef(name + "fwdBy8"))

			// Check alignment
			MOVL(src.As32(), AX.As32())
			ORL(dst.As32(), AX.As32())
			TESTL(U32(7), AX.As32())
			JEQ(LabelRef(name + "fwdBy8"))

			// Do 1 byte at a time
			// MOVQ(length, CX)
			// FIXME:
			// REP;	MOVSB
			JMP(end)

		Label(name + "fwdBy8")
		// Do 8 bytes at a time
		MOVQ(length, CX)
		SHRQ(U8(3), CX)
		ANDQ(U8(7), length)
		// FIXME:
		//REP;	MOVSQ
		JMP(LabelRef(name + "tail"))

		Label(name + "back")

		//check overlap
		MOVQ(src, CX)
		ADDQ(length, CX)
		CMPQ(CX, dst)
		JLS(LabelRef(name + "forward"))

		//whole thing backwards has
		//adjusted addresses

		ADDQ(length, dst)
		ADDQ(length, src)
		STD()

		//
		//  copy
		 //
		MOVQ(length, CX)
		SHRQ(U8(3), CX)
		ANDQ(U8(7), length)

		SUBQ(U8(8), dst)
		SUBQ(U8(8), src)
		// FIXME:
		//REP;	MOVSQ

		// FIXME:
		//CLD()

		ADDQ(U8(8), dst)
		ADDQ(U8(8), src)
		SUBQ(length, dst)
		SUBQ(length, src)
		JMP(LabelRef(name + "tail"))
	*/

	Label(name + "move_1or2")
	MOVB(Mem{Base: src}, AX.As8())
	MOVB(Mem{Base: src, Disp: -1, Index: length, Scale: 1}, CX.As8())
	MOVB(AX.As8(), Mem{Base: dst})
	MOVB(CX.As8(), Mem{Base: dst, Disp: -1, Index: length, Scale: 1})
	JMP(end)

	Label(name + "move_4")
	MOVL(Mem{Base: src}, AX.As32())
	MOVL(AX.As32(), Mem{Base: dst})
	JMP(end)

	Label(name + "move_3")
	MOVW(Mem{Base: src}, AX.As16())
	MOVB(Mem{Base: src, Disp: 2}, CX.As8())
	MOVW(AX.As16(), Mem{Base: dst})
	MOVB(CX.As8(), Mem{Base: dst, Disp: 2})
	JMP(end)

	Label(name + "move_5through7")
	MOVL(Mem{Base: src}, AX.As32())
	MOVL(Mem{Base: src, Disp: -4, Index: length, Scale: 1}, CX.As32())
	MOVL(AX.As32(), Mem{Base: dst})
	MOVL(CX.As32(), Mem{Base: dst, Disp: -4, Index: length, Scale: 1})
	JMP(end)

	Label(name + "move_8")
	// We need a separate case for 8 to make sure we write pointers atomically.
	MOVQ(Mem{Base: src}, AX)
	MOVQ(AX, Mem{Base: dst})
	JMP(end)

	Label(name + "move_9through16")
	MOVQ(Mem{Base: src}, AX)
	MOVQ(Mem{Base: src, Disp: -8, Index: length, Scale: 1}, CX)
	MOVQ(AX, Mem{Base: dst})
	MOVQ(CX, Mem{Base: dst, Disp: -8, Index: length, Scale: 1})
	JMP(end)

	Label(name + "move_17through32")
	X0, X1, X2, X3, X4, X5, X6, X7 := XMM(), XMM(), XMM(), XMM(), XMM(), XMM(), XMM(), XMM()
	X8, X9, X10, X11, X12, X13, X14, X15 := XMM(), XMM(), XMM(), XMM(), XMM(), XMM(), XMM(), XMM()

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

	Label(name + "move_256through2048")
	SUBQ(U32(256), length)
	MOVOU(Mem{Base: src}, X0)
	MOVOU(Mem{Base: src, Disp: 16}, X1)
	MOVOU(Mem{Base: src, Disp: 32}, X2)
	MOVOU(Mem{Base: src, Disp: 48}, X3)
	MOVOU(Mem{Base: src, Disp: 64}, X4)
	MOVOU(Mem{Base: src, Disp: 80}, X5)
	MOVOU(Mem{Base: src, Disp: 96}, X6)
	MOVOU(Mem{Base: src, Disp: 112}, X7)
	MOVOU(Mem{Base: src, Disp: 128}, X8)
	MOVOU(Mem{Base: src, Disp: 144}, X9)
	MOVOU(Mem{Base: src, Disp: 160}, X10)
	MOVOU(Mem{Base: src, Disp: 176}, X11)
	MOVOU(Mem{Base: src, Disp: 192}, X12)
	MOVOU(Mem{Base: src, Disp: 208}, X13)
	MOVOU(Mem{Base: src, Disp: 224}, X14)
	MOVOU(Mem{Base: src, Disp: 240}, X15)
	MOVOU(X0, Mem{Base: dst})
	MOVOU(X1, Mem{Base: dst, Disp: 16})
	MOVOU(X2, Mem{Base: dst, Disp: 32})
	MOVOU(X3, Mem{Base: dst, Disp: 48})
	MOVOU(X4, Mem{Base: dst, Disp: 64})
	MOVOU(X5, Mem{Base: dst, Disp: 80})
	MOVOU(X6, Mem{Base: dst, Disp: 96})
	MOVOU(X7, Mem{Base: dst, Disp: 112})
	MOVOU(X8, Mem{Base: dst, Disp: 128})
	MOVOU(X9, Mem{Base: dst, Disp: 144})
	MOVOU(X10, Mem{Base: dst, Disp: 160})
	MOVOU(X11, Mem{Base: dst, Disp: 176})
	MOVOU(X12, Mem{Base: dst, Disp: 192})
	MOVOU(X13, Mem{Base: dst, Disp: 208})
	MOVOU(X14, Mem{Base: dst, Disp: 224})
	MOVOU(X15, Mem{Base: dst, Disp: 240})
	CMPQ(length, U32(256))
	LEAQ(Mem{Base: src, Disp: 256}, src)
	LEAQ(Mem{Base: dst, Disp: 256}, dst)
	JGE(LabelRef(name + "move_256through2048"))
	JMP(LabelRef(name + "tail"))

	if avx {
		Label(name + "avxUnaligned")
		R8, R10 := GP64(), GP64()
		// There are two implementations of move algorithm.
		// The first one for non-overlapped memory regions. It uses forward copying.
		// We do not support overlapping input

		// Non-temporal copy would be better for big sizes.
		CMPQ(length, U32(0x100000))
		JAE(LabelRef(name + "gobble_big_data_fwd"))

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
		MOVB(U8(0x80), AX.As8())
		// Align destination address

		MOVQ(U64(0xffffffffffffffe0), R8)
		ANDQ(R8, dst)

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
		RET()

		Label(name + "gobble_big_data_fwd")
		// There is forward copying for big regions.
		// It uses non-temporal mov instructions.
		// Details of this algorithm are commented previously for small sizes.
		LEAQ(Mem{Base: src, Index: length, Scale: 1}, CX)
		MOVOU(Mem{Base: src, Index: length, Scale: 1, Disp: -0x80}, X5)
		MOVOU(Mem{Base: CX, Disp: -0x70}, X6)
		MOVOU(Mem{Base: CX, Disp: -0x60}, X7)
		MOVOU(Mem{Base: CX, Disp: -0x50}, X8)
		MOVOU(Mem{Base: CX, Disp: -0x40}, X9)
		MOVOU(Mem{Base: CX, Disp: -0x30}, X10)
		MOVOU(Mem{Base: CX, Disp: -0x20}, X11)
		MOVOU(Mem{Base: CX, Disp: -0x10}, X12)
		VMOVDQU(Mem{Base: src}, Y4)
		MOVQ(dst, R8)
		MOVQ(U64(0xffffffffffffffe0), R10)
		ANDQ(R10, dst)

		ADDQ(U8(32), dst)
		MOVQ(dst, R10)
		SUBQ(R8, R10)
		SUBQ(R10, length)
		ADDQ(R10, src)
		LEAQ(Mem{Base: dst, Index: length, Scale: 1}, CX)
		SUBQ(U8(0x80), length)

		Label(name + "gobble_mem_fwd_loop")
		PREFETCHNTA(Mem{Base: src, Disp: 0x1c0})
		PREFETCHNTA(Mem{Base: src, Disp: 0x280})
		// Prefetch values were chosen empirically.
		// Approach for prefetch usage as in 7.6.6 of [1]
		// [1] 64-ia-32-architectures-optimization-manual.pdf
		// https://www.intel.ru/content/dam/www/public/us/en/documents/manuals/64-ia-32-architectures-optimization-manual.pdf
		VMOVDQU(Mem{Base: src}, Y0)
		VMOVDQU(Mem{Base: src, Disp: 0x20}, Y1)
		VMOVDQU(Mem{Base: src, Disp: 0x40}, Y2)
		VMOVDQU(Mem{Base: src, Disp: 0x60}, Y3)

		ADDQ(U8(0x80), src)
		VMOVNTDQ(Y0, Mem{Base: dst})
		VMOVNTDQ(Y1, Mem{Base: dst, Disp: 0x20})
		VMOVNTDQ(Y2, Mem{Base: dst, Disp: 0x20})
		VMOVNTDQ(Y3, Mem{Base: dst, Disp: 0x60})
		ADDQ(U8(0x80), dst)
		SUBQ(U8(0x80), length)
		JA(LabelRef(name + "gobble_mem_fwd_loop"))
		// NT instructions don't follow the normal cache-coherency rules.
		// We need SFENCE there to make copied data available timely.
		SFENCE()
		VMOVDQU(Y4, Mem{Base: R8})
		VZEROUPPER()
		MOVOU(X5, Mem{Base: CX, Disp: -0x80})
		MOVOU(X6, Mem{Base: CX, Disp: -0x70})
		MOVOU(X7, Mem{Base: CX, Disp: -0x60})
		MOVOU(X8, Mem{Base: CX, Disp: -0x50})
		MOVOU(X9, Mem{Base: CX, Disp: -0x40})
		MOVOU(X10, Mem{Base: CX, Disp: -0x30})
		MOVOU(X11, Mem{Base: CX, Disp: -0x20})
		MOVOU(X12, Mem{Base: CX, Disp: -0x10})
		JMP(end)
	}
}

// genMatchLen generates standalone matchLen.
func genMatchLen() {
	TEXT("matchLen", NOSPLIT, "func(a, b []byte) int")
	Doc("matchLen returns how many bytes match in a and b", "",
		"It assumes that:",
		"  len(a) <= len(b)", "")
	Pragma("noescape")

	aBase, bBase, length := GP64(), GP64(), GP64()

	Load(Param("a").Base(), aBase)
	Load(Param("b").Base(), bBase)
	Load(Param("a").Len(), length)
	l := matchLen("standalone", Mem{Base: aBase}, Mem{Base: bBase}, length, LabelRef("gen_match_len_end"))
	Label("gen_match_len_end")
	Store(l, ReturnIndex(0))
	RET()
}

// matchLen returns the number of matching bytes of a and b.
// len is the maximum number of bytes to match.
// Will jump to end when done and returns the length.
func matchLen(name string, a, b Mem, len reg.GPVirtual, end LabelRef) reg.GPVirtual {
	tmp, matched := GP64(), GP64()
	XORQ(matched, matched)

	CMPQ(len, U8(8))
	JL(LabelRef("matchlen_single_" + name))

	Label("matchlen_loopback_" + name)
	MOVQ(Mem{Base: a.Base, Index: matched, Scale: 1}, tmp)
	XORQ(Mem{Base: b.Base, Index: matched, Scale: 1}, tmp)
	TESTQ(tmp, tmp)
	JZ(LabelRef("matchlen_loop_" + name))
	// Not all match.
	BSFQ(tmp, tmp)
	SARQ(U8(3), tmp)
	LEAQ(Mem{Base: matched, Index: tmp, Scale: 1}, matched)
	JMP(end)

	// All 8 byte matched, update and loop.
	Label("matchlen_loop_" + name)
	LEAQ(Mem{Base: len, Disp: -8}, len)
	LEAQ(Mem{Base: matched, Disp: 8}, matched)
	CMPQ(len, U8(8))
	JGE(LabelRef("matchlen_loopback_" + name))

	// Less than 8 bytes left.
	Label("matchlen_single_" + name)
	TESTQ(len, len)
	JZ(end)
	Label("matchlen_single_loopback_" + name)
	MOVB(Mem{Base: a.Base, Index: matched, Scale: 1}, tmp.As8())
	CMPB(Mem{Base: b.Base, Index: matched, Scale: 1}, tmp.As8())
	JNE(end)
	LEAQ(Mem{Base: matched, Disp: 1}, matched)
	DECQ(len)
	JNZ(LabelRef("matchlen_single_loopback_" + name))
	JMP(end)
	return matched
}
