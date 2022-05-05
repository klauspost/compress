package main

//go:generate go run gen.go -out ../decompress_amd64.s -pkg=huff0

import (
	"flag"
	"strconv"

	_ "github.com/klauspost/compress"

	. "github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/buildtags"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

func main() {
	flag.Parse()

	Constraint(buildtags.Not("appengine").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())
	Constraint(buildtags.Term("gc").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())

	decompress := decompress4x{}
	decompress.generateProcedure("decompress4x_main_loop_amd64")

	decompress8b := decompress4x8bit{}
	decompress8b.generateProcedure("decompress4x_8b_main_loop_amd64")

	Generate()
}

const buffoff = 256 // see decompress.go, we're using [4][256]byte table

type decompress4x struct {
	bmi2 bool
}

func (d decompress4x) generateProcedure(name string) {
	Package("github.com/klauspost/compress/huff0")
	TEXT(name, 0, "func(ctx* decompress4xContext) uint8")
	Doc(name+" is an x86 assembler implementation of Decompress4X when tablelog > 8.decodes a sequence", "")
	Pragma("noescape")

	off := GP64()
	XORQ(off, off)

	exhausted := reg.RBX                     // Fixed since we need 8H
	XORQ(exhausted.As64(), exhausted.As64()) // exhausted = false

	peekBits := GP64()
	buffer := GP64()
	table := GP64()

	br0 := GP64()
	br1 := GP64()
	br2 := GP64()
	br3 := GP64()

	Comment("Preload values")
	{
		ctx := Dereference(Param("ctx"))
		Load(ctx.Field("peekBits"), peekBits)
		Load(ctx.Field("buf"), buffer)
		Load(ctx.Field("tbl"), table)
		Load(ctx.Field("pbr0"), br0)
		Load(ctx.Field("pbr1"), br1)
		Load(ctx.Field("pbr2"), br2)
		Load(ctx.Field("pbr3"), br3)
	}

	Comment("Main loop")
	Label("main_loop")

	d.decodeTwoValues(0, br0, peekBits, table, buffer, off, exhausted)
	d.decodeTwoValues(1, br1, peekBits, table, buffer, off, exhausted)
	d.decodeTwoValues(2, br2, peekBits, table, buffer, off, exhausted)
	d.decodeTwoValues(3, br3, peekBits, table, buffer, off, exhausted)

	ADDB(U8(2), off.As8()) // off += 2

	TESTB(exhausted.As8H(), exhausted.As8H()) // any br[i].ofs < 4?
	JNZ(LabelRef("done"))

	CMPB(off.As8(), U8(0))
	JNZ(LabelRef("main_loop"))

	Label("done")
	offsetComp, err := ReturnIndex(0).Resolve()
	if err != nil {
		panic(err)
	}
	MOVB(off.As8(), offsetComp.Addr)
	RET()
}

// TODO [wmu]: I believe it's doable in avo, but can't figure out how to deal
//             with arbitrary pointers to a given type
const bitReader_in = 0
const bitReader_off = bitReader_in + 3*8 // {ptr, len, cap}
const bitReader_value = bitReader_off + 8
const bitReader_bitsRead = bitReader_value + 8

func (d decompress4x) decodeTwoValues(id int, br, peekBits, table, buffer, off reg.GPVirtual, exhausted reg.GPPhysical) {
	Commentf("br%d.fillFast()", id)
	brOffset := GP64()
	brBitsRead := GP64()
	brValue := GP64()

	MOVQ(Mem{Base: br, Disp: bitReader_off}, brOffset)
	MOVQ(Mem{Base: br, Disp: bitReader_value}, brValue)
	MOVBQZX(Mem{Base: br, Disp: bitReader_bitsRead}, brBitsRead)

	// We must have at least 2 * max tablelog left
	CMPQ(brBitsRead, U8(64-22))
	JBE(LabelRef("skip_fill" + strconv.Itoa(id)))

	SUBQ(U8(32), brBitsRead) // b.bitsRead -= 32
	SUBQ(U8(4), brOffset)    // b.off -= 4

	// v := b.in[b.off-4 : b.off]
	// v = v[:4]
	// low := (uint32(v[0])) | (uint32(v[1]) << 8) | (uint32(v[2]) << 16) | (uint32(v[3]) << 24)
	tmp := GP64()
	MOVQ(Mem{Base: br, Disp: bitReader_in}, tmp)

	Comment("b.value |= uint64(low) << (b.bitsRead & 63)")
	CX := reg.CL
	addr := Mem{Base: brOffset, Index: tmp.As64(), Scale: 1}
	if d.bmi2 {
		SHLXQ(brBitsRead, addr, tmp.As64()) // tmp = uint32(b.in[b.off:b.off+4]) << (b.bitsRead & 63)
	} else {
		MOVL(addr, tmp.As32()) // tmp = uint32(b.in[b.off:b.off+4])
		MOVQ(brBitsRead, CX.As64())
		SHLQ(CX, tmp.As64())
	}

	ORQ(tmp.As64(), brValue)

	Commentf("exhausted = exhausted || (br%d.off < 4)", id)
	CMPQ(brOffset, U8(4))
	SETLT(exhausted.As8L())
	ORB(exhausted.As8L(), exhausted.As8H())
	Label("skip_fill" + strconv.Itoa(id))

	val := GP64()
	Commentf("val0 := br%d.peekTopBits(peekBits)", id)
	if d.bmi2 {
		SHRXQ(peekBits, brValue, val.As64()) // val = (value >> peek_bits) & mask
	} else {
		MOVQ(brValue, val.As64())
		MOVQ(peekBits, CX.As64())
		SHRQ(CX, val.As64()) // val = (value >> peek_bits) & mask
	}

	Comment("v0 := table[val0&mask]")
	v := reg.RDX
	MOVW(Mem{Base: table, Index: val.As64(), Scale: 2}, v.As16())

	Commentf("br%d.advance(uint8(v0.entry)", id)
	out := reg.RAX // Fixed since we need 8H
	MOVB(v.As8H(), out.As8()) // BL = uint8(v0.entry >> 8)

	MOVBQZX(v.As8(), CX.As64())
	if d.bmi2 {
		SHLXQ(v.As64(), brValue, brValue) // value <<= n
	} else {
		SHLQ(CX, brValue) // value <<= n
	}

	ADDQ(CX.As64(), brBitsRead) // bits_read += n

	Commentf("val1 := br%d.peekTopBits(peekBits)", id)
	if d.bmi2 {
		SHRXQ(peekBits, brValue, val.As64()) // val = (value >> peek_bits) & mask
	} else {
		MOVQ(peekBits, CX.As64())
		MOVQ(brValue, val.As64())
		SHRQ(CX, val.As64()) // val = (value >> peek_bits) & mask
	}

	Comment("v1 := table[val1&mask]")
	MOVW(Mem{Base: table, Index: val.As64(), Scale: 2}, v.As16()) // tmp - v1

	Commentf("br%d.advance(uint8(v1.entry))", id)
	MOVB(v.As8H(), out.As8H()) // BH = uint8(v0.entry >> 8)

	MOVBQZX(v.As8(), CX.As64())
	if d.bmi2 {
		SHLXQ(v.As64(), brValue, brValue) // value <<= n
	} else {
		SHLQ(CX, brValue) // value <<= n
	}

	ADDQ(CX.As64(), brBitsRead) // bits_read += n

	Comment("these two writes get coalesced")
	Comment("buf[stream][off] = uint8(v0.entry >> 8)")
	Comment("buf[stream][off+1] = uint8(v1.entry >> 8)")
	MOVW(out.As16(), Mem{Base: buffer, Index: off, Scale: 1, Disp: id * buffoff})

	Comment("update the bitrader reader structure")
	MOVQ(brOffset, Mem{Base: br, Disp: bitReader_off})
	MOVQ(brValue, Mem{Base: br, Disp: bitReader_value})
	MOVB(brBitsRead.As8(), Mem{Base: br, Disp: bitReader_bitsRead})
}

type decompress4x8bit struct {
	bmi2 bool
}

func (d decompress4x8bit) generateProcedure(name string) {
	Package("github.com/klauspost/compress/huff0")
	TEXT(name, 0, "func(ctx* decompress4xContext) uint8")
	Doc(name+" is an x86 assembler implementation of Decompress4X when tablelog > 8.decodes a sequence", "")
	Pragma("noescape")

	off := GP64()
	XORQ(off, off)

	exhausted := reg.RBX                     // Fixed since we need 8H
	XORQ(exhausted.As64(), exhausted.As64()) // exhausted = false

	peekBits := GP64()
	buffer := GP64()
	table := GP64()

	br0 := GP64()
	br1 := GP64()
	br2 := GP64()
	br3 := GP64()

	Comment("Preload values")
	{
		ctx := Dereference(Param("ctx"))
		Load(ctx.Field("peekBits"), peekBits)
		Load(ctx.Field("buf"), buffer)
		Load(ctx.Field("tbl"), table)
		Load(ctx.Field("pbr0"), br0)
		Load(ctx.Field("pbr1"), br1)
		Load(ctx.Field("pbr2"), br2)
		Load(ctx.Field("pbr3"), br3)
	}

	Comment("Main loop")
	Label("main_loop")

	d.decodeFourValues(0, br0, peekBits, table, buffer, off, exhausted)
	d.decodeFourValues(1, br1, peekBits, table, buffer, off, exhausted)
	d.decodeFourValues(2, br2, peekBits, table, buffer, off, exhausted)
	d.decodeFourValues(3, br3, peekBits, table, buffer, off, exhausted)

	ADDB(U8(4), off.As8()) // off += 4

	TESTB(exhausted.As8H(), exhausted.As8H()) // any br[i].ofs < 4?
	JNZ(LabelRef("done"))

	CMPB(off.As8(), U8(0))
	JNZ(LabelRef("main_loop"))

	Label("done")
	offsetComp, err := ReturnIndex(0).Resolve()
	if err != nil {
		panic(err)
	}
	MOVB(off.As8(), offsetComp.Addr)
	RET()
}

func (d decompress4x8bit) decodeFourValues(id int, br, peekBits, table, buffer, off reg.GPVirtual, exhausted reg.GPPhysical) {
	Commentf("br%d.fillFast()", id)
	brOffset := GP64()
	brBitsRead := GP64()
	brValue := GP64()

	MOVQ(Mem{Base: br, Disp: bitReader_off}, brOffset)
	MOVQ(Mem{Base: br, Disp: bitReader_value}, brValue)
	MOVBQZX(Mem{Base: br, Disp: bitReader_bitsRead}, brBitsRead)

	// We must have at least 2 * max tablelog left
	CMPQ(brBitsRead, U8(32))
	JBE(LabelRef("skip_fill" + strconv.Itoa(id)))

	SUBQ(U8(32), brBitsRead) // b.bitsRead -= 32
	SUBQ(U8(4), brOffset)    // b.off -= 4

	// v := b.in[b.off-4 : b.off]
	// v = v[:4]
	// low := (uint32(v[0])) | (uint32(v[1]) << 8) | (uint32(v[2]) << 16) | (uint32(v[3]) << 24)
	tmp := GP64()
	MOVQ(Mem{Base: br, Disp: bitReader_in}, tmp)

	Comment("b.value |= uint64(low) << (b.bitsRead & 63)")
	CX := reg.CL
	addr := Mem{Base: brOffset, Index: tmp.As64(), Scale: 1}
	if d.bmi2 {
		SHLXQ(brBitsRead, addr, tmp.As64()) // tmp = uint32(b.in[b.off:b.off+4]) << (b.bitsRead & 63)
	} else {
		MOVL(addr, tmp.As32()) // tmp = uint32(b.in[b.off:b.off+4])
		MOVQ(brBitsRead, CX.As64())
		SHLQ(CX, tmp.As64())
	}

	ORQ(tmp.As64(), brValue)

	Commentf("exhausted = exhausted || (br%d.off < 4)", id)
	CMPQ(brOffset, U8(4))
	SETLT(exhausted.As8L())
	ORB(exhausted.As8L(), exhausted.As8H())
	Label("skip_fill" + strconv.Itoa(id))

	decompress := func(valID int, outByte reg.Register) {
		val := GP64()
		Commentf("val%d := br%d.peekTopBits(peekBits)", valID, id)
		if d.bmi2 {
			SHRXQ(peekBits, brValue, val.As64()) // val = (value >> peek_bits) & mask
		} else {
			MOVQ(brValue, val.As64())
			MOVQ(peekBits, CX.As64())
			SHRQ(CX, val.As64()) // val = (value >> peek_bits) & mask
		}

		Commentf("v%d := table[val0&mask]", valID)
		MOVW(Mem{Base: table, Index: val.As64(), Scale: 2}, CX.As16())

		Commentf("br%d.advance(uint8(v%d.entry)", id, valID)
		MOVB(CX.As8H(), outByte) // BL = uint8(v0.entry >> 8)

		MOVBQZX(CX.As8(), CX.As64())
		if d.bmi2 {
			SHLXQ(CX.As64(), brValue, brValue) // value <<= n
		} else {
			SHLQ(CX, brValue) // value <<= n
		}

		ADDQ(CX.As64(), brBitsRead) // bits_read += n
	}

	out := reg.RAX // Fixed since we need 8H
	decompress(0, out.As8L())
	decompress(1, out.As8H())
	BSWAPL(out.As32())
	decompress(2, out.As8H())
	decompress(3, out.As8L())
	BSWAPL(out.As32())

	Comment("these four writes get coalesced")
	Comment("buf[stream][off] = uint8(v0.entry >> 8)")
	Comment("buf[stream][off+1] = uint8(v1.entry >> 8)")
	Comment("buf[stream][off+2] = uint8(v2.entry >> 8)")
	Comment("buf[stream][off+3] = uint8(v3.entry >> 8)")
	MOVL(out.As32(), Mem{Base: buffer, Index: off, Scale: 1, Disp: id * buffoff})

	Comment("update the bitrader reader structure")
	MOVQ(brOffset, Mem{Base: br, Disp: bitReader_off})
	MOVQ(brValue, Mem{Base: br, Disp: bitReader_value})
	MOVB(brBitsRead.As8(), Mem{Base: br, Disp: bitReader_bitsRead})
}
