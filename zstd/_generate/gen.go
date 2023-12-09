package main

//go:generate go run gen.go -out ../seqdec_amd64.s -pkg=zstd

import (
	"flag"
	"fmt"
	"runtime"

	_ "github.com/klauspost/compress"

	. "github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/buildtags"
	"github.com/mmcloughlin/avo/gotypes"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

// insert extra checks here and there.
const debug = false

// error reported when mo == 0 && ml > 0
const errorMatchLenOfsMismatch = 1

// error reported when ml > maxMatchLen
const errorMatchLenTooBig = 2

// error reported when mo > t or mo > s.windowSize
const errorMatchOffTooBig = 3

// error reported when the sum of literal lengths exceeds the literal buffer size
const errorNotEnoughLiterals = 4

// error reported when capacity of `out` is too small
const errorNotEnoughSpace = 5

// error reported when bits are overread.
const errorOverread = 6

const maxMatchLen = 131074

// size of struct seqVals
const seqValsSize = 24

func main() {
	flag.Parse()

	Constraint(buildtags.Not("appengine").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())
	Constraint(buildtags.Term("gc").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())

	o := options{
		bmi2:     false,
		fiftysix: false,
		useSeqs:  true,
	}
	o.genDecodeSeqAsm("sequenceDecs_decode_amd64")
	o.fiftysix = true
	o.genDecodeSeqAsm("sequenceDecs_decode_56_amd64")
	o.bmi2 = true
	o.fiftysix = false
	o.genDecodeSeqAsm("sequenceDecs_decode_bmi2")
	o.fiftysix = true
	o.genDecodeSeqAsm("sequenceDecs_decode_56_bmi2")

	exec := executeSimple{
		useSeqs: true,
		safeMem: false,
	}
	exec.generateProcedure("sequenceDecs_executeSimple_amd64")
	exec.safeMem = true
	exec.generateProcedure("sequenceDecs_executeSimple_safe_amd64")

	decodeSync := decodeSync{}
	decodeSync.setBMI2(false)
	decodeSync.generateProcedure("sequenceDecs_decodeSync_amd64")
	decodeSync.setBMI2(true)
	decodeSync.generateProcedure("sequenceDecs_decodeSync_bmi2")

	decodeSync.execute.safeMem = true
	decodeSync.setBMI2(false)
	decodeSync.generateProcedure("sequenceDecs_decodeSync_safe_amd64")
	decodeSync.setBMI2(true)
	decodeSync.generateProcedure("sequenceDecs_decodeSync_safe_bmi2")

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
	bmi2     bool
	fiftysix bool // Less than max 56 bits/loop
	useSeqs  bool // Generate code that uses the `seqs` auxiliary table
}

func (o options) genDecodeSeqAsm(name string) {
	Package("github.com/klauspost/compress/zstd")
	TEXT(name, 0, "func(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int")
	Doc(name+" decodes a sequence", "")
	Pragma("noescape")

	nop := func(ctx *executeSingleTripleContext, handleLoop func()) {}

	o.generateBody(name, nop)
}

func (o options) generateBody(name string, executeSingleTriple func(ctx *executeSingleTripleContext, handleLoop func())) {
	// registers used by `decode`
	brValue := GP64()
	brBitsRead := GP64()
	brOffset := GP64()
	llState := GP64()
	mlState := GP64()
	ofState := GP64()
	var seqBase reg.GPVirtual // allocated only when o.useSeqs is true

	// values used by `execute` (allocated only when o.useSeqs is false)
	ec := executeSingleTripleContext{}

	// 1. load bitReader (done once)
	brPointerStash := AllocLocal(8)
	{
		br := Dereference(Param("br"))
		brPointer := GP64()
		Load(br.Field("value"), brValue)
		Load(br.Field("bitsRead"), brBitsRead)
		Load(br.Field("in").Base(), brPointer)
		Load(br.Field("in").Len(), brOffset)
		ADDQ(brOffset, brPointer) // Add current offset to read pointer.
		MOVQ(brPointer, brPointerStash)
	}

	// 2. load states (done once)
	var moP Mem
	var mlP Mem
	var llP Mem

	{
		ctx := Dereference(Param("ctx"))
		Load(ctx.Field("llState"), llState)
		Load(ctx.Field("mlState"), mlState)
		Load(ctx.Field("ofState"), ofState)

		if o.useSeqs {
			seqBase = GP64()
			Load(ctx.Field("seqs").Base(), seqBase)
			moP = Mem{Base: seqBase, Disp: 2 * 8} // Pointer to current mo
			mlP = Mem{Base: seqBase, Disp: 1 * 8} // Pointer to current ml
			llP = Mem{Base: seqBase, Disp: 0 * 8} // Pointer to current ll
		} else {
			moP = AllocLocal(8)
			mlP = AllocLocal(8)
			llP = AllocLocal(8)
			ec.moPtr = moP
			ec.mlPtr = mlP
			ec.llPtr = llP
			zero := GP64()
			XORQ(zero, zero)
			MOVQ(zero, moP)
			MOVQ(zero, mlP)
			MOVQ(zero, llP)

			ec.outBase = GP64()
			ec.outEndPtr = AllocLocal(8)
			ec.literals = GP64()
			ec.outPosition = GP64()
			ec.histLenPtr = AllocLocal(8)
			ec.histBasePtr = AllocLocal(8)
			ec.windowSizePtr = AllocLocal(8)

			loadField := func(field gotypes.Component, target Mem) {
				tmp := GP64()
				Load(field, tmp)
				MOVQ(tmp, target)
			}

			Load(ctx.Field("out").Base(), ec.outBase)
			loadField(ctx.Field("out").Cap(), ec.outEndPtr)
			Load(ctx.Field("literals").Base(), ec.literals)
			Load(ctx.Field("outPosition"), ec.outPosition)
			loadField(ctx.Field("windowSize"), ec.windowSizePtr)
			loadField(ctx.Field("history").Base(), ec.histBasePtr)
			loadField(ctx.Field("history").Len(), ec.histLenPtr)

			{
				tmp := GP64()
				MOVQ(ec.histLenPtr, tmp)
				ADDQ(tmp, ec.histBasePtr) // Note: we always copy from &hist[len(hist) - v]
			}

			Comment("Calculate poiter to s.out[cap(s.out)] (a past-end pointer)")
			ADDQ(ec.outBase, ec.outEndPtr)

			Comment("outBase += outPosition")
			ADDQ(ec.outPosition, ec.outBase)

		}
	}

	// Store previous offsets in registers.
	var offsets [3]reg.GPVirtual
	if o.useSeqs {
		s := Dereference(Param("s"))
		for i := range offsets {
			offsets[i] = GP64()
			po, err := s.Field("prevOffset").Index(i).Resolve()
			if err != nil {
				panic(err)
			}

			MOVQ(po.Addr, offsets[i])
		}
	}

	// MAIN LOOP:
	Label(name + "_main_loop")

	{
		brPointer := GP64()
		MOVQ(brPointerStash, brPointer)

		Comment("Fill bitreader to have enough for the offset and match length.")
		o.bitreaderFill(name+"_fill", brValue, brBitsRead, brOffset, brPointer, LabelRef("error_overread"))

		Comment("Update offset")
		// Up to 32 extra bits
		o.updateLength(name+"_of_update", brValue, brBitsRead, ofState, moP)

		Comment("Update match length")
		// Up to 16 extra bits
		o.updateLength(name+"_ml_update", brValue, brBitsRead, mlState, mlP)

		// If we need more than 56 in total, we must refill here.
		if !o.fiftysix {
			Comment("Fill bitreader to have enough for the remaining")
			o.bitreaderFill(name+"_fill_2", brValue, brBitsRead, brOffset, brPointer, LabelRef("error_overread"))
		}

		Comment("Update literal length")
		// Up to 16 bits
		o.updateLength(name+"_ll_update", brValue, brBitsRead, llState, llP)

		Comment("Fill bitreader for state updates")
		MOVQ(brPointer, brPointerStash)
	}

	R14 := GP64()
	if o.bmi2 {
		tmp := GP64()
		MOVQ(U32(8|(8<<8)), tmp)
		BEXTRQ(tmp, ofState, R14)
	} else {
		MOVQ(ofState, R14) // copy ofState, its current value is needed below
		SHRQ(U8(8), R14)   // moB (from the ofState before its update)
		MOVBQZX(R14.As8(), R14)
	}

	// Reload ctx
	ctx := Dereference(Param("ctx"))
	iteration, err := ctx.Field("iteration").Resolve()
	if err != nil {
		panic(err)
	}
	// if ctx.iteration != 0, do update
	CMPQ(iteration.Addr, U8(0))
	JZ(LabelRef(name + "_skip_update"))

	// Update states, max tablelog 28
	{
		if o.bmi2 {
			// Get total number of bits (it is safe, as nBits is <= 9, thus 3*9 < 255)
			total := GP64()
			LEAQ(Mem{Base: llState, Index: mlState, Scale: 1}, total)
			ADDQ(ofState, total)
			MOVBQZX(total.As8(), total) // total = llState.As8() + mlState.As8() + ofState.As8()

			// Read `total` bits
			bits := o.getBits(total, brValue, brBitsRead)

			// Update states
			Comment("Update Offset State")
			{
				nBits := ofState // Note: SHRXQ uses lower 6 bits of shift amount and BZHIQ lower 8 bits of count
				lowBits := GP64()
				BZHIQ(nBits, bits, lowBits) // lowBits = bits & ((1 << nBits) - 1))
				SHRXQ(nBits, bits, bits)    // bits >>= nBits
				o.nextState(ofState, lowBits, "ofTable")
			}
			Comment("Update Match Length State")
			{
				nBits := mlState
				lowBits := GP64()
				BZHIQ(nBits, bits, lowBits) // lowBits = bits & ((1 << nBits) - 1))
				SHRXQ(nBits, bits, bits)    // lowBits >>= nBits
				o.nextState(mlState, lowBits, "mlTable")
			}
			Comment("Update Literal Length State")
			{
				nBits := llState
				lowBits := GP64()
				BZHIQ(nBits, bits, lowBits) // lowBits = bits & ((1 << nBits) - 1))
				o.nextState(llState, lowBits, "llTable")
			}
		} else {
			Comment("Update Literal Length State")
			o.updateState(llState, brValue, brBitsRead, "llTable")
			Comment("Update Match Length State")
			o.updateState(mlState, brValue, brBitsRead, "mlTable")
			Comment("Update Offset State")
			o.updateState(ofState, brValue, brBitsRead, "ofTable")
		}
	}
	Label(name + "_skip_update")

	Comment("Adjust offset")

	var offset reg.GPVirtual
	end := LabelRef(name + "_after_adjust")
	if o.useSeqs {
		offset = o.adjustOffset(name+"_adjust", moP, llP, R14, &offsets, end)
	} else {
		offset = o.adjustOffsetInMemory(name+"_adjust", moP, llP, R14, end)
	}
	Label(name + "_after_adjust")

	MOVQ(offset, moP) // Store offset

	Comment("Check values")
	ml := GP64()
	MOVQ(mlP, ml)
	ll := GP64()
	MOVQ(llP, ll)

	// Update length
	{
		length := GP64()
		LEAQ(Mem{Base: ml, Index: ll, Scale: 1}, length)
		s := Dereference(Param("s"))
		seqSizeP, err := s.Field("seqSize").Resolve()
		if err != nil {
			panic(err)
		}
		ADDQ(length, seqSizeP.Addr) // s.seqSize += ml + ll
	}

	// Reload ctx
	ctx = Dereference(Param("ctx"))
	litRemainP, err := ctx.Field("litRemain").Resolve()
	if err != nil {
		panic(err)
	}
	SUBQ(ll, litRemainP.Addr) // ctx.litRemain -= ll
	JS(LabelRef("error_not_enough_literals"))
	{
		// 	if ml > maxMatchLen {
		//		return fmt.Errorf("match len (%d) bigger than max allowed length", ml)
		//	}
		CMPQ(ml, U32(maxMatchLen))
		JA(LabelRef(name + "_error_match_len_too_big"))
	}
	{
		// 	if mo == 0 && ml > 0 {
		//		return fmt.Errorf("zero matchoff and matchlen (%d) > 0", ml)
		//	}
		TESTQ(offset, offset)
		JNZ(LabelRef(name + "_match_len_ofs_ok")) // mo != 0
		TESTQ(ml, ml)
		JNZ(LabelRef(name + "_error_match_len_ofs_mismatch"))
	}

	Label(name + "_match_len_ofs_ok")

	if !o.useSeqs {
		handleLoop := func() {
			JMP(LabelRef("handle_loop"))
		}

		executeSingleTriple(&ec, handleLoop)
	}

	Label("handle_loop")
	if o.useSeqs {
		ADDQ(U8(seqValsSize), seqBase)
	}
	ctx = Dereference(Param("ctx"))
	iterationP, err := ctx.Field("iteration").Resolve()
	if err != nil {
		panic(err)
	}

	DECQ(iterationP.Addr)
	JNS(LabelRef(name + "_main_loop"))

	Label("loop_finished")

	// Store offsets
	if o.useSeqs {
		s := Dereference(Param("s"))
		for i := range offsets {
			po, _ := s.Field("prevOffset").Index(i).Resolve()
			MOVQ(offsets[i], po.Addr)
		}
	}

	// update bitreader state before returning
	br := Dereference(Param("br"))
	Store(brValue, br.Field("value"))
	Store(brBitsRead.As8(), br.Field("bitsRead"))
	Store(brOffset, br.Field("in").Len())

	if !o.useSeqs {
		Comment("Update the context")
		ctx := Dereference(Param("ctx"))
		Store(ec.outPosition, ctx.Field("outPosition"))

		// compute litPosition
		tmp := GP64()
		Load(ctx.Field("literals").Base(), tmp)
		SUBQ(tmp, ec.literals) // litPosition := current - initial literals pointer
		Store(ec.literals, ctx.Field("litPosition"))
	}

	Comment("Return success")
	o.returnWithCode(0)

	Comment("Return with match length error")
	{
		Label(name + "_error_match_len_ofs_mismatch")
		if !o.useSeqs {
			tmp := GP64()
			MOVQ(mlP, tmp)
			ctx := Dereference(Param("ctx"))
			Store(tmp, ctx.Field("ml"))
		}
		o.returnWithCode(errorMatchLenOfsMismatch)
	}

	Comment("Return with match too long error")
	{
		Label(name + "_error_match_len_too_big")
		if !o.useSeqs {
			ctx := Dereference(Param("ctx"))
			tmp := GP64()
			MOVQ(mlP, tmp)
			Store(tmp, ctx.Field("ml"))
		}
		o.returnWithCode(errorMatchLenTooBig)
	}

	Comment("Return with match offset too long error")
	{
		Label("error_match_off_too_big")
		if !o.useSeqs {
			ctx := Dereference(Param("ctx"))
			tmp := GP64()
			MOVQ(moP, tmp)
			Store(tmp, ctx.Field("mo"))
			Store(ec.outPosition, ctx.Field("outPosition"))
		}
		o.returnWithCode(errorMatchOffTooBig)
	}

	Comment("Return with not enough literals error")
	{
		Label("error_not_enough_literals")
		if !o.useSeqs {
			ctx := Dereference(Param("ctx"))
			tmp := GP64()
			MOVQ(llP, tmp)
			Store(tmp, ctx.Field("ll"))
		}
		// Note: the `litRemain` field is updated in-place (for both useSeqs values)

		o.returnWithCode(errorNotEnoughLiterals)
	}

	Comment("Return with overread error")
	{
		Label("error_overread")
		o.returnWithCode(errorOverread)
	}

	if !o.useSeqs {
		Comment("Return with not enough output space error")
		Label("error_not_enough_space")
		{
			ctx := Dereference(Param("ctx"))
			tmp := GP64()
			MOVQ(llP, tmp)
			Store(tmp, ctx.Field("ll"))
			MOVQ(mlP, tmp)
			Store(tmp, ctx.Field("ml"))
			Store(ec.outPosition, ctx.Field("outPosition"))

			o.returnWithCode(errorNotEnoughSpace)
		}
	}
}

func (o options) returnWithCode(returnCode uint32) {
	a, err := ReturnIndex(0).Resolve()
	if err != nil {
		panic(err)
	}
	MOVQ(U32(returnCode), a.Addr)
	RET()
}

// bitreaderFill will make sure at least 56 bits are available.
func (o options) bitreaderFill(name string, brValue, brBitsRead, brOffset, brPointer reg.GPVirtual, overread LabelRef) {
	// bitreader_fill begin
	CMPQ(brOffset, U8(8)) //  b.off >= 8
	JL(LabelRef(name + "_byte_by_byte"))

	off := GP64()
	MOVQ(brBitsRead, off)
	SHRQ(U8(3), off)                    // off = brBitsRead / 8
	SUBQ(off, brPointer)                // brPointer = brPointer - off
	MOVQ(Mem{Base: brPointer}, brValue) // brValue = brPointer[0]
	SUBQ(off, brOffset)                 // brOffset = brOffset - off
	ANDQ(U8(7), brBitsRead)             // brBitsRead = brBitsRead & 7
	JMP(LabelRef(name + "_end"))

	Label(name + "_byte_by_byte")
	CMPQ(brOffset, U8(0)) /* for b.off > 0 */
	JLE(LabelRef(name + "_check_overread"))

	CMPQ(brBitsRead, U8(7)) /* for brBitsRead > 7 */
	JLE(LabelRef(name + "_end"))

	SHLQ(U8(8), brValue) /* b.value << 8 | uint8(mem) */
	SUBQ(U8(1), brPointer)
	SUBQ(U8(1), brOffset)
	SUBQ(U8(8), brBitsRead)

	if false {
		// Appears slightly worse (AMD Zen2)
		MOVB(Mem{Base: brPointer}, brValue.As8L())
	} else {
		tmp := GP64()
		MOVBQZX(Mem{Base: brPointer}, tmp)
		ORQ(tmp, brValue)
	}
	JMP(LabelRef(name + "_byte_by_byte"))

	Label(name + "_check_overread")
	CMPQ(brBitsRead, U8(64))
	JA(overread)

	Label(name + "_end")
}

func (o options) updateLength(name string, brValue, brBitsRead, state reg.GPVirtual, out Mem) {
	if o.bmi2 {
		DX := GP64()
		extr := GP64()
		MOVQ(U32(8|(8<<8)), extr)
		BEXTRQ(extr, state, DX) // addBits = (state >> 8) &xff
		BX := GP64()
		MOVQ(brValue, BX)
		// TODO: We should be able to extra bits with BEXTRQ
		CX := reg.CL
		LEAQ(Mem{Base: brBitsRead, Index: DX, Scale: 1}, CX.As64()) // CX: shift = r.bitsRead + n
		ROLQ(CX, BX)
		BZHIQ(DX.As64(), BX, BX)
		MOVQ(CX.As64(), brBitsRead) // br.bitsRead += moB
		res := GP64()               // AX
		MOVQ(state, res)
		SHRQ(U8(32), res) // AX = mo (ofState.baselineInt(), that's the higher dword of moState)
		ADDQ(BX, res)     // AX - mo + br.getBits(moB)
		MOVQ(res, out)
	} else {
		BX := GP64()
		CX := reg.CL
		AX := reg.RAX
		MOVQ(state, AX.As64()) // So we can grab high bytes.
		MOVQ(brBitsRead, CX.As64())
		MOVQ(brValue, BX)
		SHLQ(CX, BX)               // BX = br.value << br.bitsRead (part of getBits)
		MOVB(AX.As8H(), CX.As8L()) // CX = moB  (ofState.addBits(), that is byte #1 of moState)
		SHRQ(U8(32), AX)           // AX = mo (ofState.baselineInt(), that's the higher dword of moState)
		// If addBits == 0, skip
		TESTQ(CX.As64(), CX.As64())
		JZ(LabelRef(name + "_zero"))

		ADDQ(CX.As64(), brBitsRead) // br.bitsRead += n (part of getBits)
		// If overread, skip
		CMPQ(brBitsRead, U8(64))
		JA(LabelRef(name + "_zero"))
		CMPQ(CX.As64(), U8(64))
		JAE(LabelRef(name + "_zero"))

		NEGQ(CX.As64()) // CX = 64 - n
		SHRQ(CX, BX)    // BX = (br.value << br.bitsRead) >> (64 - n) -- getBits() result
		ADDQ(BX, AX)    // AX - mo + br.getBits(moB)

		Label(name + "_zero")
		MOVQ(AX, out) // Store result
	}
}

func (o options) updateState(state, brValue, brBitsRead reg.GPVirtual, table string) {
	AX := GP64()
	MOVBQZX(state.As8(), AX) // AX = nBits
	// Check we have a reasonable nBits
	assert(func(ok LabelRef) {
		CMPQ(AX, U8(9))
		JBE(ok)
	})

	DX := GP64()
	MOVL(state.As32(), DX.As32()) // Clear the top 32 bits.
	SHRL(U8(16), DX.As32())

	{
		lowBits := o.getBits(AX, brValue, brBitsRead)
		// Check if below tablelog
		assert(func(ok LabelRef) {
			CMPQ(lowBits, U32(512))
			JB(ok)
		})
		ADDQ(lowBits, DX)
	}

	// Load table pointer
	tablePtr := GP64()
	Comment("Load ctx." + table)
	ctx := Dereference(Param("ctx"))
	tableA, err := ctx.Field(table).Base().Resolve()
	if err != nil {
		panic(err)
	}
	MOVQ(tableA.Addr, tablePtr)

	// Check if below tablelog
	assert(func(ok LabelRef) {
		CMPQ(DX, U32(512))
		JB(ok)
	})
	// Load new state
	MOVQ(Mem{Base: tablePtr, Index: DX, Scale: 8}, state)
}

func (o options) nextState(state, lowBits reg.GPVirtual, table string) {
	DX := GP64()
	MOVL(state.As32(), DX.As32()) // Clear the top 32 bits.
	SHRL(U8(16), DX.As32())

	ADDQ(lowBits, DX)

	// Load table pointer
	tablePtr := GP64()
	Comment("Load ctx." + table)
	ctx := Dereference(Param("ctx"))
	tableA, err := ctx.Field(table).Base().Resolve()
	if err != nil {
		panic(err)
	}
	MOVQ(tableA.Addr, tablePtr)

	// Check if below tablelog
	assert(func(ok LabelRef) {
		CMPQ(DX, U32(512))
		JB(ok)
	})
	// Load new state
	MOVQ(Mem{Base: tablePtr, Index: DX, Scale: 8}, state)
}

// getBits will return nbits bits from brValue.
func (o options) getBits(nBits, brValue, brBitsRead reg.GPVirtual) reg.GPVirtual {
	BX := GP64()
	CX := reg.CL

	LEAQ(Mem{Base: brBitsRead, Index: nBits, Scale: 1}, CX.As64())
	MOVQ(brValue, BX)
	MOVQ(CX.As64(), brBitsRead)
	ROLQ(CX, BX)

	// BX &= (1<<nBits) - 1
	if o.bmi2 {
		BZHIQ(nBits, BX, BX)
	} else {
		mask := GP32()
		MOVL(U32(1), mask)
		MOVB(nBits.As8(), CX)
		SHLL(CX, mask)
		DECL(mask)
		ANDQ(mask.As64(), BX)
	}
	return BX
}

func (o options) adjustOffset(name string, moP, llP Mem, offsetB reg.GPVirtual, offsets *[3]reg.GPVirtual, end LabelRef) (offset reg.GPVirtual) {
	offset = GP64()
	MOVQ(moP, offset)
	{
		// if offsetB > 1 {
		//     s.prevOffset[2] = s.prevOffset[1]
		//     s.prevOffset[1] = s.prevOffset[0]
		//     s.prevOffset[0] = offset
		//     return offset
		// }
		CMPQ(offsetB, U8(1))
		JBE(LabelRef(name + "_offsetB_1_or_0"))

		MOVQ(offsets[1], offsets[2]) //  s.prevOffset[2] = s.prevOffset[1]
		MOVQ(offsets[0], offsets[1]) // s.prevOffset[1] = s.prevOffset[0]
		MOVQ(offset, offsets[0])     // s.prevOffset[0] = offset
		JMP(end)
	}

	Label(name + "_offsetB_1_or_0")
	// if litLen == 0 {
	//     offset++
	// }
	{
		if true {
			CMPQ(llP, U32(0))
			JNE(LabelRef(name + "_offset_maybezero"))
			INCQ(offset)
			JMP(LabelRef(name + "_offset_nonzero"))
		} else {
			// No idea why this doesn't work:
			tmp := GP64()
			LEAQ(Mem{Base: offset, Disp: 1}, tmp)
			CMPQ(llP, U32(0))
			CMOVQEQ(tmp, offset)
		}

		// if offset == 0 {
		//     return s.prevOffset[0]
		// }
		{
			Label(name + "_offset_maybezero")
			TESTQ(offset, offset)
			JNZ(LabelRef(name + "_offset_nonzero"))
			MOVQ(offsets[0], offset)
			JMP(end)
		}
	}
	Label(name + "_offset_nonzero")
	{
		// if offset == 3 {
		//     temp = s.prevOffset[0] - 1
		// } else {
		//     temp = s.prevOffset[offset]
		// }
		temp := GP64()
		CMPQ(offset, U8(1))
		JB(LabelRef(name + "_zero"))
		JEQ(LabelRef(name + "_one"))
		CMPQ(offset, U8(2))
		JA(LabelRef(name + "_three"))
		JMP(LabelRef(name + "_two"))

		Label(name + "_zero")
		MOVQ(offsets[0], temp)
		JMP(LabelRef(name + "_test_temp_valid"))

		Label(name + "_one")
		MOVQ(offsets[1], temp)
		JMP(LabelRef(name + "_test_temp_valid"))

		Label(name + "_two")
		MOVQ(offsets[2], temp)
		JMP(LabelRef(name + "_test_temp_valid"))

		Label(name + "_three")
		LEAQ(Mem{Base: offsets[0], Disp: -1}, temp)

		Label(name + "_test_temp_valid")
		// if temp == 0 {
		//     temp = 1
		// }
		TESTQ(temp, temp)
		JNZ(LabelRef(name + "_temp_valid"))
		MOVQ(U32(1), temp)

		Label(name + "_temp_valid")
		// if offset != 1 {
		//     s.prevOffset[2] = s.prevOffset[1]
		// }
		CMPQ(offset, U8(1))
		if false {
			JZ(LabelRef(name + "_skip"))
			MOVQ(offsets[1], offsets[2]) // s.prevOffset[2] = s.prevOffset[1]
			Label(name + "_skip")
		} else {
			CMOVQNE(offsets[1], offsets[2])
		}
		// s.prevOffset[1] = s.prevOffset[0]
		// s.prevOffset[0] = temp
		MOVQ(offsets[0], offsets[1])
		MOVQ(temp, offsets[0])
		MOVQ(temp, offset) // return temp
	}
	JMP(end)
	return offset
}

// adjustOffsetInMemory is an adjustOffset version that does not cache prevOffset values in registers.
// It fetches and stores values directly into the fields of `sequenceDecs` structure.
func (o options) adjustOffsetInMemory(name string, moP, llP Mem, offsetB reg.GPVirtual, end LabelRef) (offset reg.GPVirtual) {
	s := Dereference(Param("s"))

	po0, _ := s.Field("prevOffset").Index(0).Resolve()
	po1, _ := s.Field("prevOffset").Index(1).Resolve()
	po2, _ := s.Field("prevOffset").Index(2).Resolve()
	offset = GP64()
	MOVQ(moP, offset)
	{
		// if offsetB > 1 {
		//     s.prevOffset[2] = s.prevOffset[1]
		//     s.prevOffset[1] = s.prevOffset[0]
		//     s.prevOffset[0] = offset
		//     return offset
		// }
		CMPQ(offsetB, U8(1))
		JBE(LabelRef(name + "_offsetB_1_or_0"))

		tmp := XMM()
		MOVUPS(po0.Addr, tmp)  // tmp = (s.prevOffset[0], s.prevOffset[1])
		MOVQ(offset, po0.Addr) // s.prevOffset[0] = offset
		MOVUPS(tmp, po1.Addr)  // s.prevOffset[1], s.prevOffset[2] = s.prevOffset[0], s.prevOffset[1]
		JMP(end)
	}

	Label(name + "_offsetB_1_or_0")
	// if litLen == 0 {
	//     offset++
	// }

	{
		CMPQ(llP, U32(0))
		JNE(LabelRef(name + "_offset_maybezero"))
		INCQ(offset)
		JMP(LabelRef(name + "_offset_nonzero"))

		// if offset == 0 {
		//     return s.prevOffset[0]
		// }
		{
			Label(name + "_offset_maybezero")
			TESTQ(offset, offset)
			JNZ(LabelRef(name + "_offset_nonzero"))
			MOVQ(po0.Addr, offset)
			JMP(end)
		}
	}
	Label(name + "_offset_nonzero")
	{
		// Offset must be 1 -> 3
		assert(func(ok LabelRef) {
			// Test is above or equal (shouldn't be equal)
			CMPQ(offset, U32(0))
			JAE(ok)
		})
		assert(func(ok LabelRef) {
			// Check if Above 0.
			CMPQ(offset, U32(0))
			JA(ok)
		})
		assert(func(ok LabelRef) {
			// Check if Below or Equal to 3.
			CMPQ(offset, U32(3))
			JBE(ok)
		})
		// if offset == 3 {
		//     temp = s.prevOffset[0] - 1
		// } else {
		//     temp = s.prevOffset[offset]
		// }
		//
		// this if got transformed into:
		//
		// ofs   := offset
		// shift := 0
		// if offset == 3 {
		//     ofs   = 0
		//     shift = -1
		// }
		// temp := s.prevOffset[ofs] + shift
		// TODO: This should be easier...
		CX, DX, R15 := GP64(), GP64(), GP64()
		MOVQ(offset, CX)
		XORQ(DX, DX)
		MOVQ(I32(-1), R15)
		CMPQ(offset, U8(3))
		CMOVQEQ(DX, CX)
		CMOVQEQ(R15, DX)
		assert(func(ok LabelRef) {
			CMPQ(CX, U32(0))
			JAE(ok)
		})
		assert(func(ok LabelRef) {
			CMPQ(CX, U32(3))
			JB(ok)
		})
		if po0.Addr.Index != nil {
			// Use temporary (not currently needed)
			prevOffset := GP64()
			LEAQ(po0.Addr, prevOffset) // &prevOffset[0]
			ADDQ(Mem{Base: prevOffset, Index: CX, Scale: 8}, DX)
		} else {
			ADDQ(Mem{Base: po0.Addr.Base, Disp: po0.Addr.Disp, Index: CX, Scale: 8}, DX)
		}

		temp := DX
		// if temp == 0 {
		//     temp = 1
		// }
		JNZ(LabelRef(name + "_temp_valid"))
		MOVQ(U32(1), temp)

		Label(name + "_temp_valid")
		// if offset != 1 {
		//     s.prevOffset[2] = s.prevOffset[1]
		// }
		CMPQ(offset, U8(1))
		JZ(LabelRef(name + "_skip"))
		tmp := GP64()
		MOVQ(po1.Addr, tmp)
		MOVQ(tmp, po2.Addr) // s.prevOffset[2] = s.prevOffset[1]

		Label(name + "_skip")
		// s.prevOffset[1] = s.prevOffset[0]
		// s.prevOffset[0] = temp
		tmp = GP64()
		MOVQ(po0.Addr, tmp)
		MOVQ(tmp, po1.Addr)  // s.prevOffset[1] = s.prevOffset[0]
		MOVQ(temp, po0.Addr) // s.prevOffset[0] = temp
		MOVQ(temp, offset)   // return temp
	}
	JMP(end)
	return offset
}

type executeSimple struct {
	useSeqs bool // Generate code that uses the `seqs` auxiliary table
	safeMem bool
}

func (e executeSimple) generateProcedure(name string) {
	Package("github.com/klauspost/compress/zstd")
	TEXT(name, 0, "func (ctx *executeAsmContext) bool")
	Doc(name+" implements the main loop of sequenceDecs.decode in x86 asm", "")
	Pragma("noescape")

	seqsBase := GP64()
	seqsLen := GP64()
	seqIndex := GP64()
	outBase := GP64()
	literals := GP64()
	outPosition := GP64()
	windowSize := GP64()
	histBase := GP64()
	histLen := GP64()

	{
		ctx := Dereference(Param("ctx"))
		tmp := GP64()
		Load(ctx.Field("seqs").Len(), seqsLen)
		TESTQ(seqsLen, seqsLen)
		JZ(LabelRef("empty_seqs"))
		Load(ctx.Field("seqs").Base(), seqsBase)
		Load(ctx.Field("seqIndex"), seqIndex)
		Load(ctx.Field("out").Base(), outBase)
		Load(ctx.Field("literals").Base(), literals)
		Load(ctx.Field("outPosition"), outPosition)
		Load(ctx.Field("windowSize"), windowSize)
		Load(ctx.Field("history").Base(), histBase)
		Load(ctx.Field("history").Len(), histLen)

		ADDQ(histLen, histBase) // Note: we always copy from &hist[len(hist) - v]

		Comment("seqsBase += 24 * seqIndex")
		LEAQ(Mem{Base: seqIndex, Index: seqIndex, Scale: 2}, tmp) // * 3
		SHLQ(U8(3), tmp)                                          // * 8
		ADDQ(tmp, seqsBase)

		Comment("outBase += outPosition")
		ADDQ(outPosition, outBase)
	}

	Label("main_loop")

	moPtr := Mem{Base: seqsBase, Disp: 2 * 8}
	mlPtr := Mem{Base: seqsBase, Disp: 1 * 8}
	llPtr := Mem{Base: seqsBase, Disp: 0 * 8}

	// generates the loop tail
	handleLoop := func() {
		ADDQ(U8(seqValsSize), seqsBase) // seqs += sizeof(seqVals)
		INCQ(seqIndex)
		CMPQ(seqIndex, seqsLen)
		JB(LabelRef("main_loop"))
	}

	ctx := executeSingleTripleContext{
		llPtr:       llPtr,
		moPtr:       moPtr,
		mlPtr:       mlPtr,
		literals:    literals,
		outBase:     outBase,
		outPosition: outPosition,
		histBase:    histBase,
		histLen:     histLen,
		windowSize:  windowSize,
	}

	e.executeSingleTriple(&ctx, handleLoop)

	Label("handle_loop")
	handleLoop()

	ret, err := ReturnIndex(0).Resolve()
	if err != nil {
		panic(err)
	}

	returnValue := func(val int) {

		Comment("Return value")
		MOVB(U8(val), ret.Addr)

		Comment("Update the context")
		ctx := Dereference(Param("ctx"))
		Store(seqIndex, ctx.Field("seqIndex"))
		Store(outPosition, ctx.Field("outPosition"))

		// litPosition := current - initial literals pointer
		litField, _ := ctx.Field("literals").Base().Resolve()
		SUBQ(litField.Addr, literals)
		Store(literals, ctx.Field("litPosition"))
	}
	Label("loop_finished")
	returnValue(1)
	RET()

	Label("error_match_off_too_big")
	returnValue(0)
	RET()

	Label("empty_seqs")
	Comment("Return value")
	MOVB(U8(1), ret.Addr)
	RET()
}

type executeSingleTripleContext struct {
	// common values
	llPtr Mem
	moPtr Mem
	mlPtr Mem

	literals    reg.GPVirtual
	outBase     reg.GPVirtual
	outPosition reg.GPVirtual

	// values used when useSeqs is true
	histBase   reg.GPVirtual
	histLen    reg.GPVirtual
	windowSize reg.GPVirtual

	// values used when useSeqs is false
	outEndPtr     Mem // pointer to s.out[cap(s.out)]
	histBasePtr   Mem
	histLenPtr    Mem
	windowSizePtr Mem
}

// executeSingleTriple performs copy from literals and history according
// to the decoded values ll, mo and ml.
func (e executeSimple) executeSingleTriple(c *executeSingleTripleContext, handleLoop func()) {
	ll := GP64()
	MOVQ(c.llPtr, ll)
	mo := GP64()
	MOVQ(c.moPtr, mo)
	ml := GP64()
	MOVQ(c.mlPtr, ml)

	if !e.useSeqs {
		Comment("Check if we have enough space in s.out")
		{
			// baseAfterCopy = ll + ml + c.outBase
			baseAfterCopy := GP64()
			LEAQ(Mem{Base: ll, Index: ml, Scale: 1}, baseAfterCopy)
			ADDQ(c.outBase, baseAfterCopy)
			CMPQ(baseAfterCopy, c.outEndPtr)
			JA(LabelRef("error_not_enough_space"))
		}
	}

	Comment("Copy literals")
	Label("copy_literals")
	{
		TESTQ(ll, ll)
		JZ(LabelRef("check_offset"))
		// TODO: Investigate if it is possible to consistently overallocate literals.
		if e.safeMem {
			e.copyMemoryPrecise("1", c.literals, c.outBase, ll, 1)
		} else {
			e.copyMemoryND("1", c.literals, c.outBase, ll)
			ADDQ(ll, c.literals)
			ADDQ(ll, c.outBase)
		}
		ADDQ(ll, c.outPosition)
	}

	Comment("Malformed input if seq.mo > t+len(hist) || seq.mo > s.windowSize)")
	{
		Label("check_offset")

		tmp := GP64()
		if e.useSeqs {
			LEAQ(Mem{Base: c.outPosition, Index: c.histLen, Scale: 1}, tmp)
		} else {
			MOVQ(c.outPosition, tmp)
			ADDQ(c.histLenPtr, tmp)
		}
		CMPQ(mo, tmp)
		JG(LabelRef("error_match_off_too_big"))

		if e.useSeqs {
			CMPQ(mo, c.windowSize)
		} else {
			CMPQ(mo, c.windowSizePtr)
		}
		JG(LabelRef("error_match_off_too_big"))
	}

	Comment("Copy match from history")
	{
		v := GP64()
		MOVQ(mo, v)

		// v := seq.mo - outPosition
		SUBQ(c.outPosition, v)
		JLS(LabelRef("copy_match")) // do nothing if v <= 0

		// v := seq.mo - t; v > 0 {
		//     start := len(hist) - v
		//     ...
		// }
		assert(func(ok LabelRef) {
			if e.useSeqs {
				TESTQ(c.histLen, c.histLen)
			} else {
				t := GP64()
				MOVQ(c.histLenPtr, t)
				TESTQ(t, t)
			}
			JNZ(ok)
		})
		ptr := GP64()
		if e.useSeqs {
			MOVQ(c.histBase, ptr)
		} else {
			MOVQ(c.histBasePtr, ptr)
		}
		SUBQ(v, ptr) // ptr := &hist[len(hist) - v]
		CMPQ(ml, v)
		JG(LabelRef("copy_all_from_history"))
		/*  if ml <= v {
		        copy(out[outPosition:], hist[start:start+seq.ml])
		        t += seq.ml
		        continue
		    }
		*/
		// We know ml will be at least 3, since we didn't copy anything yet.
		e.copyMemoryPrecise("4", ptr, c.outBase, ml, 3)
		ADDQ(ml, c.outPosition)
		// Note: for the current go tests this branch is taken in 99.53% cases,
		//       this is why we repeat a little code here.
		handleLoop()
		JMP(LabelRef("loop_finished"))

		Label("copy_all_from_history")
		/*  if seq.ml > v {
		        // Some goes into current block.
		        // Copy remainder of history
		        copy(out[outPosition:], hist[start:])
		        outPosition += v
		        seq.ml -= v
		    }
		*/
		e.copyMemoryPrecise("5", ptr, c.outBase, v, 1)
		ADDQ(v, c.outPosition)
		SUBQ(v, ml)
		// ml cannot be 0, since we only jump here is ml > v.
		// Copy rest from current block.
	}

	Comment("Copy match from the current buffer")
	Label("copy_match")
	{
		src := GP64()
		MOVQ(c.outBase, src)
		SUBQ(mo, src) // src = &s.out[t - mo]

		// start := t - mo
		// if ml <= t-start {
		//     // no overlap
		// } else {
		//     // overlapping copy
		// }
		//
		// Note: ml <= t - start
		//       ml <= t - (t - mo)
		//       ml <= mo
		Comment("ml <= mo")
		CMPQ(ml, mo)
		JA(LabelRef("copy_overlapping_match"))

		Comment("Copy non-overlapping match")
		{
			ADDQ(ml, c.outPosition)
			if e.safeMem {
				e.copyMemoryPrecise("2", src, c.outBase, ml, 1)
			} else {
				dst := GP64()
				MOVQ(c.outBase, dst)
				ADDQ(ml, c.outBase)
				e.copyMemory("2", src, dst, ml)
			}

			JMP(LabelRef("handle_loop"))
		}

		Comment("Copy overlapping match")
		Label("copy_overlapping_match")
		{
			ADDQ(ml, c.outPosition)
			e.copyOverlappedMemory("3", src, c.outBase, ml)
		}
	}
}

// copyMemory will copy memory in blocks of 16 bytes,
// overwriting up to 15 extra bytes.
// src and dst are updated. length will be zero or less.
func (e executeSimple) copyMemory(suffix string, src, dst, length reg.GPVirtual) {
	label := "copy_" + suffix

	Label(label)
	t := XMM()
	MOVUPS(Mem{Base: src}, t)
	MOVUPS(t, Mem{Base: dst})
	ADDQ(U8(16), src)
	ADDQ(U8(16), dst)
	SUBQ(U8(16), length)
	// jump if (CF == 0 and ZF == 0).
	JHI(LabelRef(label))
}

// copyMemoryND will copy memory in blocks of 16 bytes,
// overwriting up to 15 extra bytes.
// All parameters are preserved.
func (e executeSimple) copyMemoryND(suffix string, src, dst, length reg.GPVirtual) {
	label := "copy_" + suffix

	ofs := GP64()
	s := Mem{Base: src, Index: ofs, Scale: 1}
	d := Mem{Base: dst, Index: ofs, Scale: 1}

	XORQ(ofs, ofs)
	Label(label)
	t := XMM()
	MOVUPS(s, t)
	MOVUPS(t, d)
	ADDQ(U8(16), ofs)
	CMPQ(ofs, length)
	JB(LabelRef(label))
}

// copyMemoryPrecise will copy memory in blocks of 16 bytes,
// without overreading. It adds length to src and dst,
// preserving length.
func (e executeSimple) copyMemoryPrecise(suffix string, src, dst, length reg.GPVirtual, minLength int) {
	assert(func(ok LabelRef) {
		// if length >= minLength, ok
		CMPQ(length, U8(minLength))
		JAE(ok)
	})
	if minLength == 0 {
		TESTQ(length, length)
		JZ(LabelRef("copy_" + suffix + "_end"))
	}
	n := GP64()
	MOVQ(length, n)
	SUBQ(U8(16), n)
	JB(LabelRef("copy_" + suffix + "_small"))

	// If length >= 16, copy blocks of 16 bytes and handle any remainder
	// by a block copy that overlaps with the last full block.
	{
		t := XMM()

		loop := "copy_" + suffix + "_loop"
		Label(loop)
		{
			MOVUPS(Mem{Base: src}, t)
			MOVUPS(t, Mem{Base: dst})
			ADDQ(U8(16), src)
			ADDQ(U8(16), dst)
			SUBQ(U8(16), n)
			JAE(LabelRef(loop))
		}

		// n is now the range [-16,-1].
		// -16 means we copy the entire last block again.
		// That should happen about 1/16th of the time,
		// so we don't bother to check for it.
		LEAQ(Mem{Base: src, Index: n, Disp: 16, Scale: 1}, src)
		LEAQ(Mem{Base: dst, Index: n, Disp: 16, Scale: 1}, dst)
		MOVUPS(Mem{Base: src, Disp: -16}, t)
		MOVUPS(t, Mem{Base: dst, Disp: -16})

		JMP(LabelRef("copy_" + suffix + "_end"))
	}

	Label("copy_" + suffix + "_small")
	{
		name := "copy_" + suffix + "_"
		end := LabelRef("copy_" + suffix + "_end")
		CMPQ(length, U8(3))
		JE(LabelRef(name + "move_3"))
		if minLength < 3 {
			JB(LabelRef(name + "move_1or2"))
		}
		CMPQ(length, U8(8))
		JB(LabelRef(name + "move_4through7"))
		JMP(LabelRef(name + "move_8through16"))
		AX, CX := GP64(), GP64()

		if minLength < 3 {
			Label(name + "move_1or2")
			MOVB(Mem{Base: src}, AX.As8())
			MOVB(Mem{Base: src, Disp: -1, Index: length, Scale: 1}, CX.As8())
			MOVB(AX.As8(), Mem{Base: dst})
			MOVB(CX.As8(), Mem{Base: dst, Disp: -1, Index: length, Scale: 1})
			ADDQ(length, src)
			ADDQ(length, dst)
			JMP(end)
		}

		Label(name + "move_3")
		MOVW(Mem{Base: src}, AX.As16())
		MOVB(Mem{Base: src, Disp: 2}, CX.As8())
		MOVW(AX.As16(), Mem{Base: dst})
		MOVB(CX.As8(), Mem{Base: dst, Disp: 2})
		ADDQ(length, src)
		ADDQ(length, dst)
		JMP(end)

		Label(name + "move_4through7")
		MOVL(Mem{Base: src}, AX.As32())
		MOVL(Mem{Base: src, Disp: -4, Index: length, Scale: 1}, CX.As32())
		MOVL(AX.As32(), Mem{Base: dst})
		MOVL(CX.As32(), Mem{Base: dst, Disp: -4, Index: length, Scale: 1})
		ADDQ(length, src)
		ADDQ(length, dst)
		JMP(end)

		Label(name + "move_8through16")
		MOVQ(Mem{Base: src}, AX)
		MOVQ(Mem{Base: src, Disp: -8, Index: length, Scale: 1}, CX)
		MOVQ(AX, Mem{Base: dst})
		MOVQ(CX, Mem{Base: dst, Disp: -8, Index: length, Scale: 1})
		ADDQ(length, src)
		ADDQ(length, dst)
		JMP(end)
	}
	Label("copy_" + suffix + "_end")
}

// copyOverlappedMemory will copy one byte at the time from src to dst.
// src and dst are updated. length will be zero.
func (e executeSimple) copyOverlappedMemory(suffix string, src, dst, length reg.GPVirtual) {
	label := "copy_slow_" + suffix
	tmp := GP64()

	Label(label)
	MOVB(Mem{Base: src}, tmp.As8())
	MOVB(tmp.As8(), Mem{Base: dst})
	INCQ(src)
	INCQ(dst)
	DECQ(length)
	JNZ(LabelRef(label))
}

type decodeSync struct {
	decode  options
	execute executeSimple
}

func (d *decodeSync) setBMI2(flag bool) {
	d.decode.bmi2 = flag
}

func (d *decodeSync) generateProcedure(name string) {
	Package("github.com/klauspost/compress/zstd")
	TEXT(name, 0, "func (s *sequenceDecs, br *bitReader, ctx *decodeSyncAsmContext) int")
	Doc(name+" implements the main loop of sequenceDecs.decodeSync in x86 asm", "")
	Pragma("noescape")

	d.decode.generateBody(name, d.execute.executeSingleTriple)
}
