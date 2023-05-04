package main

//go:generate go run gen.go -out ../decompress_amd64.s -pkg=huff0
//go:generate gofmt -w ../decompress_amd64.go

import (
	"flag"
	"fmt"
	"strconv"

	_ "github.com/klauspost/compress"

	. "github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/gotypes"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

func main() {
	flag.Parse()

	ConstraintExpr("amd64,!appengine,!noasm,gc")

	{
		decompress := decompress4x{}
		decompress.generateProcedure("decompress4x_main_loop_amd64")
		decompress.generateProcedure4x8bit("decompress4x_8b_main_loop_amd64")
	}

	{
		decompress := decompress1x{}
		decompress.generateProcedure("decompress1x_main_loop_amd64")

		decompress.bmi2 = true
		decompress.generateProcedure("decompress1x_main_loop_bmi2")
	}

	Generate()
}

type decompress4x struct {
}

func (d decompress4x) generateProcedure(name string) {
	Package("github.com/klauspost/compress/huff0")
	TEXT(name, 0, "func(ctx* decompress4xContext)")
	Doc(name+" is an x86 assembler implementation of Decompress4X when tablelog > 8.decodes a sequence", "")
	Pragma("noescape")

	exhausted := GP8()
	buffer := GP64()
	limit := GP64()

	peekBits := GP64()
	dstEvery := GP64()
	table := GP64()

	br := GP64()

	Comment("Preload values")
	{
		ctx := Dereference(Param("ctx"))
		Load(ctx.Field("peekBits"), peekBits)
		Load(ctx.Field("out"), buffer)
		Load(ctx.Field("limit"), limit)
		Load(ctx.Field("dstEvery"), dstEvery)
		Load(ctx.Field("tbl"), table)
		Load(ctx.Field("pbr"), br)
	}

	Comment("Main loop")
	Label("main_loop")

	// Check if we have space. We could zero exhausted outside the loop,
	// but doing it here is a hint to the CPU that there's no dependency
	// on the previous iteration's value.
	XORL(exhausted.As32(), exhausted.As32())
	CMPQ(buffer, limit)
	SETGE(exhausted.As8())
	d.decodeTwoValues(0, br, peekBits, table, buffer, dstEvery, exhausted)
	d.decodeTwoValues(1, br, peekBits, table, buffer, dstEvery, exhausted)
	d.decodeTwoValues(2, br, peekBits, table, buffer, dstEvery, exhausted)
	d.decodeTwoValues(3, br, peekBits, table, buffer, dstEvery, exhausted)

	ADDQ(U8(2), buffer) // off += 2

	TESTB(exhausted, exhausted) // any br[i].ofs < 4?
	JZ(LabelRef("main_loop"))

	{
		ctx := Dereference(Param("ctx"))
		ctxout, _ := ctx.Field("out").Resolve()
		decoded := buffer
		SUBQ(ctxout.Addr, decoded)
		SHLQ(U8(2), decoded) // decoded *= 4

		Store(decoded, ctx.Field("decoded"))
	}

	RET()
}

// TODO [wmu]: I believe it's doable in avo, but can't figure out how to deal
// with arbitrary pointers to a given type
const bitReader_in = 0
const bitReader_off = bitReader_in + 3*8 // {ptr, len, cap}
const bitReader_value = bitReader_off + 8
const bitReader_bitsRead = bitReader_value + 8
const bitReader__size = bitReader_bitsRead + 8

func (d decompress4x) decodeTwoValues(id int, br, peekBits, table, buffer, dstEvery, exhausted reg.GPVirtual) {
	brValue, brBitsRead := d.fillFast32(id, 32, br, exhausted)

	val := GP64()
	Commentf("val0 := br%d.peekTopBits(peekBits)", id)
	CX := reg.CL
	MOVQ(brValue, val.As64())
	MOVQ(peekBits, CX.As64())
	SHRQ(CX, val.As64()) // val = (value >> peek_bits) & mask

	Comment("v0 := table[val0&mask]")
	MOVW(Mem{Base: table, Index: val.As64(), Scale: 2}, CX.As16())

	Commentf("br%d.advance(uint8(v0.entry)", id)
	out := reg.RAX             // Fixed since we need 8H
	MOVB(CX.As8H(), out.As8()) // AL = uint8(v0.entry >> 8)

	SHLQ(CX, brValue)                // value <<= n
	ADDB(CX.As8(), brBitsRead.As8()) // bits_read += n

	Commentf("val1 := br%d.peekTopBits(peekBits)", id)
	MOVQ(peekBits, CX.As64())
	MOVQ(brValue, val.As64())
	SHRQ(CX, val.As64()) // val = (value >> peek_bits) & mask

	Comment("v1 := table[val1&mask]")
	MOVW(Mem{Base: table, Index: val.As64(), Scale: 2}, CX.As16()) // tmp - v1

	Commentf("br%d.advance(uint8(v1.entry))", id)
	MOVB(CX.As8H(), out.As8H())      // AH = uint8(v0.entry >> 8)
	SHLQ(CX, brValue)                // value <<= n
	ADDB(CX.As8(), brBitsRead.As8()) // bits_read += n

	Comment("these two writes get coalesced")
	Comment("out[id * dstEvery + 0] = uint8(v0.entry >> 8)")
	Comment("out[id * dstEvery + 1] = uint8(v1.entry >> 8)")
	MOVW(out.As16(), bufferIndex(id, buffer, dstEvery))

	Comment("update the bitreader structure")
	offset := id * bitReader__size
	MOVQ(brValue, Mem{Base: br, Disp: offset + bitReader_value})
	MOVB(brBitsRead.As8(), Mem{Base: br, Disp: offset + bitReader_bitsRead})
}

func (d decompress4x) generateProcedure4x8bit(name string) {
	Package("github.com/klauspost/compress/huff0")
	TEXT(name, 0, "func(ctx* decompress4xContext)")
	Doc(name+" is an x86 assembler implementation of Decompress4X when tablelog > 8.decodes a sequence", "")
	Pragma("noescape")

	exhausted := GP8()
	buffer := GP64()
	limit := GP64()

	peekBits := GP64()
	dstEvery := GP64()
	table := GP64()

	br := GP64()

	Comment("Preload values")
	{
		ctx := Dereference(Param("ctx"))
		Load(ctx.Field("peekBits"), peekBits)
		Load(ctx.Field("out"), buffer)
		Load(ctx.Field("limit"), limit)
		Load(ctx.Field("dstEvery"), dstEvery)
		Load(ctx.Field("tbl"), table)
		Load(ctx.Field("pbr"), br)
	}

	Comment("Main loop")
	Label("main_loop")

	// Check if we have space. We could zero exhausted outside the loop,
	// but doing it here is a hint to the CPU that there's no dependency
	// on the previous iteration's value.
	XORL(exhausted.As32(), exhausted.As32())
	CMPQ(buffer, limit)
	SETGE(exhausted)
	d.decodeFourValues(0, br, peekBits, table, buffer, dstEvery, exhausted)
	d.decodeFourValues(1, br, peekBits, table, buffer, dstEvery, exhausted)
	d.decodeFourValues(2, br, peekBits, table, buffer, dstEvery, exhausted)
	d.decodeFourValues(3, br, peekBits, table, buffer, dstEvery, exhausted)

	ADDQ(U8(4), buffer) // off += 4

	TESTB(exhausted, exhausted) // any br[i].ofs < 4?
	JZ(LabelRef("main_loop"))

	{
		ctx := Dereference(Param("ctx"))
		ctxout, _ := ctx.Field("out").Resolve()
		decoded := buffer
		SUBQ(ctxout.Addr, decoded)
		SHLQ(U8(2), decoded) // decoded *= 4

		Store(decoded, ctx.Field("decoded"))
	}
	RET()
}

func (d decompress4x) decodeFourValues(id int, br, peekBits, table, buffer, dstEvery, exhausted reg.GPVirtual) {
	brValue, brBitsRead := d.fillFast32(id, 32, br, exhausted)

	decompress := func(valID int, outByte reg.Register) {
		CX := reg.CL
		val := GP64()
		Commentf("val%d := br%d.peekTopBits(peekBits)", valID, id)
		MOVQ(brValue, val.As64())
		MOVQ(peekBits, CX.As64())
		SHRQ(CX, val.As64()) // val = (value >> peek_bits) & mask

		Commentf("v%d := table[val0&mask]", valID)
		MOVW(Mem{Base: table, Index: val.As64(), Scale: 2}, CX.As16())

		Commentf("br%d.advance(uint8(v%d.entry)", id, valID)
		MOVB(CX.As8H(), outByte) // outByte = uint8(v0.entry >> 8)

		SHLQ(CX, brValue)          // value <<= n
		ADDB(CX, brBitsRead.As8()) // bits_read += n
	}

	out := reg.RAX // Fixed since we need 8H
	decompress(0, out.As8L())
	decompress(1, out.As8H())
	BSWAPL(out.As32())
	decompress(2, out.As8H())
	decompress(3, out.As8L())
	BSWAPL(out.As32())

	Comment("these four writes get coalesced")
	Comment("out[id * dstEvery + 0] = uint8(v0.entry >> 8)")
	Comment("out[id * dstEvery + 1] = uint8(v1.entry >> 8)")
	Comment("out[id * dstEvery + 3] = uint8(v2.entry >> 8)")
	Comment("out[id * dstEvery + 4] = uint8(v3.entry >> 8)")
	MOVL(out.As32(), bufferIndex(id, buffer, dstEvery))

	Comment("update the bitreader structure")
	offset := id * bitReader__size
	MOVQ(brValue, Mem{Base: br, Disp: offset + bitReader_value})
	MOVB(brBitsRead.As8(), Mem{Base: br, Disp: offset + bitReader_bitsRead})
}

func bufferIndex(id int, buffer, dstEvery reg.GPVirtual) Mem {
	switch id {
	case 0:
		return Mem{Base: buffer}
	case 1, 2:
		return Mem{Base: buffer, Index: dstEvery, Scale: byte(id)}
	case 3:
		stride3 := GP64() // stride3 := 3*dstEvery
		LEAQ(Mem{Base: dstEvery, Index: dstEvery, Scale: 2}, stride3)
		return Mem{Base: buffer, Index: stride3, Scale: 1}
	default:
		panic("id must be >=0, <4")
	}
}

func (d decompress4x) fillFast32(id, atLeast int, br, exhausted reg.GPVirtual) (brValue, brBitsRead reg.GPVirtual) {
	if atLeast > 32 {
		panic(fmt.Sprintf("at least (%d) cannot be >32", atLeast))
	}
	Commentf("br%d.fillFast32()", id)
	brValue = GP64()
	brBitsRead = GP64()
	offset := bitReader__size * id
	MOVQ(Mem{Base: br, Disp: offset + bitReader_value}, brValue)
	MOVBQZX(Mem{Base: br, Disp: offset + bitReader_bitsRead}, brBitsRead)

	// We must have at least 2 * max tablelog left
	CMPQ(brBitsRead, U8(64-atLeast))
	JBE(LabelRef("skip_fill" + strconv.Itoa(id)))
	brOffset := GP64()
	MOVQ(Mem{Base: br, Disp: offset + bitReader_off}, brOffset)

	SUBQ(U8(32), brBitsRead) // b.bitsRead -= 32
	SUBQ(U8(4), brOffset)    // b.off -= 4

	// v := b.in[b.off-4 : b.off]
	// v = v[:4]
	// low := (uint32(v[0])) | (uint32(v[1]) << 8) | (uint32(v[2]) << 16) | (uint32(v[3]) << 24)
	tmp := GP64()
	MOVQ(Mem{Base: br, Disp: offset + bitReader_in}, tmp)

	Comment("b.value |= uint64(low) << (b.bitsRead & 63)")
	addr := Mem{Base: brOffset, Index: tmp.As64(), Scale: 1}
	CX := reg.CL
	MOVL(addr, tmp.As32()) // tmp = uint32(b.in[b.off:b.off+4])
	MOVQ(brBitsRead, CX.As64())
	SHLQ(CX, tmp.As64())

	MOVQ(brOffset, Mem{Base: br, Disp: offset + bitReader_off})
	ORQ(tmp.As64(), brValue)
	{
		Commentf("exhausted += (br%d.off < 4)", id)
		CMPQ(brOffset, U8(4))
		// Add carry from brOffset-4. We do this at most four times per iteration,
		// and every iteration resets exhausted's lower byte, so it doesn't overflow.
		ADCB(I8(0), exhausted)
	}

	Label("skip_fill" + strconv.Itoa(id))
	return
}

type bitReader struct {
	in       reg.GPVirtual
	off      reg.GPVirtual
	value    reg.GPVirtual
	bitsRead reg.GPVirtual
	id       int
	bmi2     bool
}

func (b *bitReader) uniqId() string {
	b.id += 1
	return strconv.Itoa(b.id)
}

func (b *bitReader) load(pointer gotypes.Component) {
	b.in = GP64()
	b.off = GP64()
	b.value = GP64()
	b.bitsRead = GP64()

	Load(pointer.Field("in").Base(), b.in)
	Load(pointer.Field("off"), b.off)
	Load(pointer.Field("value"), b.value)
	Load(pointer.Field("bitsRead"), b.bitsRead)
}

func (b *bitReader) store(pointer gotypes.Component) {
	Store(b.off, pointer.Field("off"))
	Store(b.value, pointer.Field("value"))
	// Note: explicit As8(), without this avo reports: "could not deduce mov instruction"
	Store(b.bitsRead.As8(), pointer.Field("bitsRead"))
}

func (b *bitReader) fillFast() {
	label := "bitReader_fillFast_" + b.uniqId() + "_end"
	CMPQ(b.bitsRead, U8(32))
	JL(LabelRef(label))

	SUBQ(U8(32), b.bitsRead)
	SUBQ(U8(4), b.off)

	tmp := GP64()
	MOVL(Mem{Base: b.in, Index: b.off, Scale: 1}, tmp.As32())
	if b.bmi2 {
		SHLXQ(b.bitsRead, tmp, tmp)
	} else {
		MOVQ(b.bitsRead, reg.RCX)
		SHLQ(reg.CL, tmp)
	}
	ORQ(tmp, b.value)
	Label(label)
}

func (b *bitReader) peekTopBits(n reg.GPVirtual) reg.GPVirtual {
	res := GP64()
	if b.bmi2 {
		SHRXQ(n, b.value, res)
	} else {
		MOVQ(n, reg.RCX)
		MOVQ(b.value, res)
		SHRQ(reg.CL, res)
	}

	return res
}

func (b *bitReader) advance(n reg.Register) {
	ADDQ(n, b.bitsRead)
	if b.bmi2 {
		SHLXQ(n, b.value, b.value)
	} else {
		MOVQ(n, reg.RCX)
		SHLQ(reg.CL, b.value)
	}
}

type decompress1x struct {
	bmi2 bool
}

func (d decompress1x) generateProcedure(name string) {
	Package("github.com/klauspost/compress/huff0")
	TEXT(name, 0, "func(ctx* decompress1xContext)")
	Doc(name+" is an x86 assembler implementation of Decompress1X", "")
	Pragma("noescape")

	br := bitReader{}
	br.bmi2 = d.bmi2

	buffer := GP64()
	bufferEnd := GP64() // the past-end address of buffer
	dt := GP64()
	peekBits := GP64()

	{
		ctx := Dereference(Param("ctx"))
		Load(ctx.Field("out"), buffer)

		outCap := GP64()
		Load(ctx.Field("outCap"), outCap)
		CMPQ(outCap, U8(4))
		JB(LabelRef("error_max_decoded_size_exceeded"))

		LEAQ(Mem{Base: buffer, Index: outCap, Scale: 1}, bufferEnd)

		// load bitReader struct
		pbr := Dereference(ctx.Field("pbr"))
		br.load(pbr)

		Load(ctx.Field("tbl"), dt)
		Load(ctx.Field("peekBits"), peekBits)
	}

	JMP(LabelRef("loop_condition"))

	Label("main_loop")

	out := reg.AX // Fixed, as we need an 8H part

	Comment("Check if we have room for 4 bytes in the output buffer")
	{
		tmp := GP64()
		LEAQ(Mem{Base: buffer, Disp: 4}, tmp)
		CMPQ(tmp, bufferEnd)
		JGE(LabelRef("error_max_decoded_size_exceeded"))
	}

	decompress := func(id int, out reg.Register) {
		d.decompress(id, &br, peekBits, dt, out)
	}

	Comment("Decode 4 values")
	br.fillFast()
	decompress(0, out.As8L())
	decompress(1, out.As8H())
	BSWAPL(out.As32())

	br.fillFast()
	decompress(2, out.As8H())
	decompress(3, out.As8L())
	BSWAPL(out.As32())

	Comment("Store the decoded values")
	MOVL(out.As32(), Mem{Base: buffer})
	ADDQ(U8(4), buffer)

	Label("loop_condition")
	CMPQ(br.off, U8(8))
	JGE(LabelRef("main_loop"))

	Comment("Update ctx structure")
	{
		// calculate decoded as current `out` - initial `out`
		ctx := Dereference(Param("ctx"))
		ctxout, _ := ctx.Field("out").Resolve()
		decoded := buffer
		SUBQ(ctxout.Addr, decoded)
		Store(decoded, ctx.Field("decoded"))

		pbr := Dereference(ctx.Field("pbr"))
		br.store(pbr)
	}

	RET()

	Comment("Report error")
	Label("error_max_decoded_size_exceeded")
	{
		ctx := Dereference(Param("ctx"))
		tmp := GP64()
		MOVQ(I64(-1), tmp)
		Store(tmp, ctx.Field("decoded"))
	}

	RET()
}

func (d decompress1x) decompress(id int, br *bitReader, peekBits, dt reg.GPVirtual, out reg.Register) {
	// v := dt[br.peekBitsFast(d.actualTableLog)&tlMask]
	k := br.peekTopBits(peekBits)
	v := reg.RCX // Fixed, as we need 8H part
	MOVW(Mem{Base: dt, Index: k, Scale: 2}, v.As16())

	// buf[id] = uint8(v.entry >> 8)
	MOVB(v.As8H(), out)

	// br.advance(uint8(v.entry))
	MOVBQZX(v.As8L(), v)
	br.advance(v)
}
