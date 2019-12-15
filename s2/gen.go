//+build ignore

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
	// sLimit is when to stop looking for offset/length copies.
	sLimit := stack.Offset(tmpStack)
	tmpStack += 4
	// Bail if we can't compress to at least this.
	dstLimit := stack.Offset(tmpStack)
	tmpStack += 4
	nextEmit := stack.Offset(tmpStack)
	tmpStack += 4
	repeat := stack.Offset(tmpStack)
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

		Label("zeroLoop" + name)
		for i := 0; i < 8; i++ {
			MOVOU(zeroXmm, Mem{Base: tablePtr, Disp: i * 16})
		}
		ADDQ(U8(16*8), tablePtr)
		DECQ(iReg)
		JNZ(LabelRef("zeroLoop" + name))

		// nextEmit is where in src the next emitLiteral should start from.
		MOVL(iReg.As32(), nextEmit)
	}

	{
		const inputMargin = 8
		tmp, tmp2, tmp3 := GP64(), GP64(), GP64()
		MOVQ(lenSrc, tmp)
		LEAQ(Mem{Base: tmp, Disp: -5}, tmp2)
		// sLimit := len(src) - inputMargin
		LEAQ(Mem{Base: tmp, Disp: -inputMargin}, tmp3)
		// dstLimit := len(src) - len(src)>>5 - 5
		SHRQ(U8(5), tmp)
		SUBL(tmp2.As32(), tmp.As32())
		MOVL(tmp3.As32(), sLimit)
		MOVL(tmp.As32(), dstLimit)
	}

	// s = 1
	s := GP64()
	MOVB(U8(1), s.As8())
	// repeat = 1
	MOVL(s.As32(), repeat)

	src := GP64()
	Load(Param("src").Base(), src)

	// Load cv
	cv := GP64()
	MOVQ(Mem{Base: src, Index: s, Scale: 1}, cv)
	Label("mainLoop" + name)
	{
		Label("searchLoop" + name)
		candidate := GP64()
		{
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
				JGT(LabelRef("emitRemainder" + name))
			}
			candidate2 := GP64()
			hasher := hash6(tableBits)
			{
				hash0, hash1 := GP64(), GP64()
				MOVQ(cv, hash0)
				MOVQ(cv, hash1)
				SHRQ(U8(8), hash1)
				hasher.hash(hash0)
				hasher.hash(hash1)
				MOVL(table.Idx(hash0, 1), candidate.As32())
				MOVL(table.Idx(hash1, 1), candidate2.As32())
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
				JNE(LabelRef("noRepeatFound" + name))
				{
					base := GP64()
					LEAQ(Mem{Base: s, Disp: 1}, base)
					// Extend back
					{
						i := rep
						ne := GP64()
						MOVQ(nextEmit, ne)
						TESTQ(i.As64(), i.As64())
						JZ(LabelRef("extendBackEnd" + name))

						// I is tested when decremented, so we loop back here.
						Label("extendBackLoop" + name)
						CMPQ(base.As64(), ne.As64())
						JG(LabelRef("extendBackEnd" + name))
						// if src[i-1] == src[base-1]
						tmp, tmp2 := GP64(), GP64()
						MOVB(Mem{Base: src, Index: i, Scale: 1, Disp: -1}, tmp.As8())
						MOVB(Mem{Base: src, Index: base, Scale: 1, Disp: -1}, tmp2.As8())
						CMPB(tmp.As8(), tmp2.As8())
						JNE(LabelRef("extendBackEnd" + name))
						LEAQ(Mem{Base: base, Disp: -1}, base)
						DECQ(i)
						JZ(LabelRef("extendBackEnd" + name))
						JMP(LabelRef("extendBackLoop" + name))
					}
					Label("extendBackEnd" + name)
					// Base is now at start.
					// d += emitLiteral(dst[d:], src[nextEmit:base])
					{
						tmp1, tmp2, litLen, retval, dstBaseTmp, litBase := GP64(), GP64(), GP64(), GP64(), GP64(), GP64()
						MOVQ(nextEmit, litLen)
						MOVQ(base, tmp1)
						// litBase = src[nextEmit:]
						LEAQ(Mem{Base: src, Index: litLen, Scale: 1}, litBase)
						SUBQ(tmp1, litLen) // litlen = base - nextEmit
						MOVQ(dstBase, dstBaseTmp)
						XORQ(retval, retval)
						emitLiteral("Repeat", tmp1, tmp2, litLen, retval, dstBaseTmp, litBase, LabelRef("emitLiteralDone"+name))
						Label("emitLiteralDone" + name)
						MOVQ(dstBaseTmp, dstBase)
					}
				}
			}
			Label("noRepeatFound" + name)
			NOP()
		}
		_ = candidate
	}

	Label("emitRemainder" + name)
	RET()
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
	emitLiteral("Standalone", GP64(), GP64(), litLen, retval, dstBase, litBase, "emitLiteralEndStandalone")
	Label("emitLiteralEndStandalone")
	Store(retval, ReturnIndex(0))
	RET()
}

// emitLiteral can be used for inlining an emitLiteral call.
// stack must have at least 32 bytes
func emitLiteral(name string, tmp1, tmp2, litLen, retval, dstBase, litBase reg.GPVirtual, end LabelRef) {
	n := tmp1
	n16 := tmp2

	// We always add litLen bytes
	MOVQ(litLen, retval)
	MOVQ(litLen, n)

	SUBL(U8(1), n.As32())
	// Return if AX was 0
	JC(end)

	// Find number of bytes to emit for tag.
	CMPL(n.As32(), U8(60))
	JLT(LabelRef("oneByte" + name))
	CMPL(n.As32(), U32(1<<8))
	JLT(LabelRef("twoBytes" + name))
	CMPL(n.As32(), U32(1<<16))
	JLT(LabelRef("threeBytes" + name))
	CMPL(n.As32(), U32(1<<24))
	JLT(LabelRef("fourBytes" + name))

	Label("fiveBytes" + name)
	MOVB(U8(252), Mem{Base: dstBase})
	MOVL(n.As32(), Mem{Base: dstBase, Disp: 1})
	ADDQ(U8(5), retval)
	ADDQ(U8(5), dstBase)
	JMP(LabelRef("memmove" + name))

	Label("fourBytes" + name)
	MOVQ(n, n16)
	SHRL(U8(16), n16.As32())
	MOVB(U8(248), Mem{Base: dstBase})
	MOVW(n.As16(), Mem{Base: dstBase, Disp: 1})
	MOVB(n16.As8(), Mem{Base: dstBase, Disp: 3})
	ADDQ(U8(4), retval)
	ADDQ(U8(4), dstBase)
	JMP(LabelRef("memmove" + name))

	Label("threeBytes" + name)
	MOVB(U8(0xf4), Mem{Base: dstBase})
	MOVW(n.As16(), Mem{Base: dstBase, Disp: 1})
	ADDQ(U8(3), retval)
	ADDQ(U8(3), dstBase)
	JMP(LabelRef("memmove" + name))

	Label("twoBytes" + name)
	MOVB(U8(0xf0), Mem{Base: dstBase})
	MOVB(n.As8(), Mem{Base: dstBase, Disp: 1})
	ADDQ(U8(2), retval)
	ADDQ(U8(2), dstBase)
	JMP(LabelRef("memmove" + name))

	Label("oneByte" + name)
	SHLB(U8(2), n.As8())
	MOVB(n.As8(), Mem{Base: dstBase})
	ADDQ(U8(1), retval)
	ADDQ(U8(1), dstBase)

	Label("memmove" + name)

	// copy(dst[i:], lit)
	genMemMove("EmitLitMemMove"+name, dstBase, litBase, litLen, end)
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
	emitRepeat("Standalone", length, offset, retval, dstBase, LabelRef("genEmitRepeatEnd"))
	Label("genEmitRepeatEnd")
	Store(retval, ReturnIndex(0))
	RET()
}

// emitRepeat can be used for inlining an emitRepeat call.
// length >= 4 and < 1<<32
// length is modified. dstBase is updated. retval is added to input.
// Will jump to end label when finished.
func emitRepeat(name string, length, offset, retval, dstBase reg.GPVirtual, end LabelRef) {
	Label("emit_repeat_again" + name)
	tmp := GP64()
	MOVQ(length, tmp) // Copy length
	// length -= 4
	LEAQ(Mem{Base: length, Disp: -4}, length)

	// if length <= 4 (use copied value)
	CMPL(tmp.As32(), U8(8))
	JLE(LabelRef("repeat_two" + name))

	// length < 8 && offset < 2048
	CMPL(tmp.As32(), U8(12))
	JGE(LabelRef("cant_repeat_two_offset" + name))
	CMPL(offset.As32(), U32(2048))
	JLT(LabelRef("repeat_two_offset" + name))

	const maxRepeat = ((1 << 24) - 1) + 65536
	Label("cant_repeat_two_offset" + name)
	CMPL(length.As32(), U32((1<<8)+4))
	JLT(LabelRef("repeat_three" + name)) // if length < (1<<8)+4
	CMPL(length.As32(), U32((1<<16)+(1<<8)))
	JLT(LabelRef("repeat_four" + name)) // if length < (1 << 16) + (1 << 8)
	CMPL(length.As32(), U32(maxRepeat))
	JLT(LabelRef("repeat_five" + name)) // If less than 24 bits to represent.

	// We have have more than 24 bits
	// Emit so we have at least 4 bytes left.
	LEAQ(Mem{Base: length, Disp: -(maxRepeat - 4)}, length) // length -= (maxRepeat - 4)
	MOVW(U16(7<<2|tagCopy1), Mem{Base: dstBase})            // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	MOVW(U16(65531), Mem{Base: dstBase, Disp: 2})           // 0xfffb
	MOVB(U8(255), Mem{Base: dstBase, Disp: 4})
	ADDQ(U8(5), dstBase)
	ADDQ(U8(5), retval)
	JMP(LabelRef("emit_repeat_again" + name))

	// Must be able to be within 5 bytes.
	Label("repeat_five" + name)
	LEAQ(Mem{Base: length, Disp: -65536}, length) // length -= 65536
	MOVQ(length, offset)
	MOVW(U16(7<<2|tagCopy1), Mem{Base: dstBase})     // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	MOVW(length.As16(), Mem{Base: dstBase, Disp: 2}) // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	SARQ(U8(16), offset)                             // offset = length >> 16
	MOVB(offset.As8(), Mem{Base: dstBase, Disp: 4})  // dst[4] = length >> 16
	ADDQ(U8(5), retval)                              // i += 5
	ADDQ(U8(5), dstBase)                             // dst += 5
	JMP(end)

	Label("repeat_four" + name)
	LEAQ(Mem{Base: length, Disp: -256}, length)      // length -= 256
	MOVW(U16(6<<2|tagCopy1), Mem{Base: dstBase})     // dst[0] = 6<<2 | tagCopy1, dst[1] = 0
	MOVW(length.As16(), Mem{Base: dstBase, Disp: 2}) // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	ADDQ(U8(4), retval)                              // i += 4
	ADDQ(U8(4), dstBase)                             // dst += 4
	JMP(end)

	Label("repeat_three" + name)
	LEAQ(Mem{Base: length, Disp: -4}, length)       // length -= 4
	MOVW(U16(5<<2|tagCopy1), Mem{Base: dstBase})    // dst[0] = 5<<2 | tagCopy1, dst[1] = 0
	MOVB(length.As8(), Mem{Base: dstBase, Disp: 2}) // dst[2] = uint8(length)
	ADDQ(U8(3), retval)                             // i += 3
	ADDQ(U8(3), dstBase)                            // dst += 3
	JMP(end)

	Label("repeat_two" + name)
	// dst[0] = uint8(length)<<2 | tagCopy1, dst[1] = 0
	SHLL(U8(2), length.As32())
	ORL(U8(tagCopy1), length.As32())
	MOVW(length.As16(), Mem{Base: dstBase}) // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	ADDQ(U8(2), retval)                     // i += 2
	ADDQ(U8(2), dstBase)                    // dst += 2
	JMP(end)

	Label("repeat_two_offset" + name)
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
	ADDQ(U8(2), retval)  // i += 2
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
	emitCopy("Standalone", length, offset, retval, dstBase, LabelRef("genEmitCopyEnd"))
	Label("genEmitCopyEnd")
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
// Will jump to end label when finished.
func emitCopy(name string, length, offset, retval, dstBase reg.GPVirtual, end LabelRef) {
	//if offset >= 65536 {
	CMPL(offset.As32(), U32(65536))
	JL(LabelRef("twoByteOffset" + name))
	//	if length > 64 {
	CMPL(length.As32(), U8(64))
	JLE(LabelRef("fourBytesRemain" + name))
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
	ADDQ(U8(5), retval)  // i+=5
	ADDQ(U8(5), dstBase) // dst+=5
	//		if length >= 4 {
	CMPL(length.As32(), U8(4))
	JL(LabelRef("fourBytesRemain" + name))

	// Emit remaining as repeats
	//	return 5 + emitRepeat(dst[5:], offset, length)
	// Inline call to emitRepeat. Will jump to end
	emitRepeat(name+"EmitCopy", length, offset, retval, dstBase, end)

	// Relies on flags being set before call to here.
	Label("fourBytesRemain" + name)
	//	if length == 0 {
	//		return i
	//	}
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
	ADDQ(U8(5), retval)
	ADDQ(U8(5), dstBase)
	JMP(end)

	Label("twoByteOffset" + name)
	// Offset no more than 2 bytes.

	//if length > 64 {
	CMPL(length.As32(), U8(64))
	JLE(LabelRef("twoByteOffsetShort" + name))
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
	ADDQ(U8(3), retval)
	// Inline call to emitRepeat. Will jump to end
	emitRepeat(name+"EmitCopyShort", length, offset, retval, dstBase, end)

	Label("twoByteOffsetShort" + name)
	//if length >= 12 || offset >= 2048 {
	CMPL(length.As32(), U8(12))
	JGE(LabelRef("emitCopyThree" + name))
	CMPL(offset.As32(), U32(2048))
	JGE(LabelRef("emitCopyThree" + name))

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
	ADDQ(U8(2), retval)  // i += 2
	ADDQ(U8(2), dstBase) // dst += 2
	// return 2
	JMP(end)

	Label("emitCopyThree" + name)
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
	ADDQ(U8(3), retval)  // i += 3
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
	JZ(LabelRef("Done128" + name))
	Label("Loop128" + name)
	var xmmregs [8]reg.VecVirtual
	for i := 0; i < 8; i++ {
		xmmregs[i] = XMM()
		MOVOU(Mem{Base: from}.Offset(i*16), xmmregs[i])
	}
	for i := 0; i < 8; i++ {
		MOVOU(xmmregs[i], Mem{Base: to}.Offset(i*16))
	}
	LEAQ(Mem{Base: from, Disp: 8 * 16}, from)
	LEAQ(Mem{Base: to, Disp: 8 * 16}, to)
	LEAQ(Mem{Base: n, Disp: -128}, n)
	DECQ(tmp)
	JNZ(LabelRef("Loop128" + name))

	Label("Done128" + name)
	MOVQ(n, tmp)
	// tmp = n/16
	SHRQ(U8(4), tmp)
	TESTQ(tmp, tmp)
	JZ(LabelRef("Done16" + name))

	Label("Loop16" + name)
	xmm := XMM()
	MOVOU(Mem{Base: from}, xmm)
	MOVOU(xmm, Mem{Base: to})
	LEAQ(Mem{Base: from, Disp: 16}, from)
	LEAQ(Mem{Base: to, Disp: 16}, to)
	LEAQ(Mem{Base: n, Disp: -16}, n)
	DECQ(tmp)
	JNZ(LabelRef("Loop16" + name))
	Label("Done16" + name)

	// TODO: Use REP; MOVSB somehow.
	TESTQ(n, n)
	JZ(end)
	Label("Loop1" + name)
	MOVB(Mem{Base: from}, tmp.As8())
	MOVB(tmp.As8(), Mem{Base: to})
	LEAQ(Mem{Base: from, Disp: 1}, from)
	LEAQ(Mem{Base: to, Disp: 1}, to)
	DECQ(n)
	JNZ(LabelRef("Loop1" + name))
}
