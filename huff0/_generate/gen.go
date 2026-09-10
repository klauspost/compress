package main

//go:generate go run gen.go -out ../decompress.s -arch amd64,arm64 -pkg=huff0
//go:generate gofmt -w ../decompress_amd64.go

import (
	"flag"
	"fmt"
	mbits "math/bits"
	"strconv"

	_ "github.com/klauspost/compress"

	. "github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/gotypes"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

func main() {
	flag.Parse()

	ConstraintExpr("!appengine,!noasm,gc")

	{
		decompress := decompress4x{}
		decompress.generateProcedure("decompress4x_main_loop_amd64", fast4XSymbols)
		decompress.generateProcedure("decompress4x_8b_main_loop_amd64", fast4X8bSymbols)
		decompress.generateProcedure("decompress4x_4b_main_loop_amd64", fast4X4bSymbols)

		decompress.bmi2 = true
		decompress.generateProcedure("decompress4x_main_loop_bmi2", fast4XSymbols)
		decompress.generateProcedure("decompress4x_8b_main_loop_bmi2", fast4X8bSymbols)
		decompress.generateProcedure("decompress4x_4b_main_loop_bmi2", fast4X4bSymbols)
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
	bmi2 bool
}

// TODO [wmu]: I believe it's doable in avo, but can't figure out how to deal
// with arbitrary pointers to a given type
const bitReader_in = 0
const bitReader_off = bitReader_in + 3*8 // {ptr, len, cap}
const bitReader_value = bitReader_off + 8
const bitReader_bitsRead = bitReader_value + 8
const bitReader__size = bitReader_bitsRead + 8

// Symbols decoded from each stream between reloads. The bit container holds
// a sentinel bit just below the unread bits, so the consumed count is its
// trailing zero count and no separate bitsRead register is needed. A reload
// leaves at most 7 consumed bits, so n symbols of at most b bits each need
// 7+n*b <= 63 for the sentinel to survive and 64-(7+(n-1)*b) >= b valid bits
// ahead of the last symbol: n*b <= 56.
const (
	fast4XSymbols   = 5  // tablelog 9..11: 5*11 = 55
	fast4X8bSymbols = 7  // tablelog 5..8: 7*8 = 56
	fast4X4bSymbols = 14 // tablelog <= 4: 14*4 = 56
)

// generateProcedure emits the Decompress4X main loop for one symbols-per-
// reload count. It follows zstd's HUF_decompress4X1 fast loop.
//
// Bit container layout. Each stream owns a 64-bit register holding the next
// bits to decode, most significant first, with a single "sentinel" 1 bit
// directly below them and zeros below that:
//
//	bit 63 ............................ bit 0
//	[ unread bits (top) ][1][ 0 0 0 ... 0 ]
//	                      ^ position = bits consumed since the last reload
//
// Decoding shifts the register left, so the sentinel's position (its
// trailing zero count) is the consumed count and no bitsRead register is
// needed. A reload backs the input pointer up by whole consumed bytes, loads
// a fresh 8-byte window, ORs a new sentinel into bit 0 and shifts by the 0-7
// leftover bits. Bit 0 of a window is a real data bit, but it is never
// consumed before the next reload re-reads memory, so the OR is harmless
// (bitReaderShifted.restoreFromAsm re-reads memory for the same reason).
//
// Register budget: the four containers, the four output pointers, the table,
// peekBits and the context pointer are live throughout (11 of 15), so every
// step below uses at most two temporaries.
func (d decompress4x) generateProcedure(name string, nSyms int) {
	Package("github.com/klauspost/compress/huff0")
	TEXT(name, 0, "func(ctx* decompress4xContext)")
	doc := []string{fmt.Sprintf("%s decodes %d symbols per stream per reload from four interleaved huff0 streams.", name, nSyms)}
	if !d.bmi2 {
		doc = append(doc, "avo stamps it as requiring BMI because of TZCNT, but it runs as BSF on CPUs without BMI1 (see reload).")
	}
	Doc(doc...)
	Pragma("noescape")

	// ctx stays in one register for the whole function; every memory
	// operand below is a field of it or of the bit readers it points to.
	ctx := Dereference(Param("ctx"))
	peekBits := GP64()
	table := GP64()
	var op, bits [4]reg.GPVirtual
	for i := range op {
		op[i] = GP64()
		bits[i] = GP64()
	}
	ip := func(i int) Mem {
		m, err := ctx.Field("ip").Index(i).Resolve()
		if err != nil {
			panic(err)
		}
		return m.Addr
	}

	Comment("Preload values")
	Load(ctx.Field("peekBits"), peekBits)
	Load(ctx.Field("tbl"), table)
	Load(ctx.Field("out"), op[0])
	{
		dstEvery := GP64()
		Load(ctx.Field("dstEvery"), dstEvery)
		LEAQ(Mem{Base: op[0], Index: dstEvery, Scale: 1}, op[1])
		LEAQ(Mem{Base: op[0], Index: dstEvery, Scale: 2}, op[2])
		LEAQ(Mem{Base: dstEvery, Index: dstEvery, Scale: 2}, op[3])
		ADDQ(op[0], op[3])
	}

	Comment("Convert each bit reader to sentinel form: bits = value | 1<<bitsRead")
	{
		br := GP64()
		Load(ctx.Field("pbr"), br)
		for i := range 4 {
			off := i * bitReader__size
			tmp := GP64()
			MOVQ(Mem{Base: br, Disp: off + bitReader_value}, bits[i])
			MOVBQZX(Mem{Base: br, Disp: off + bitReader_bitsRead}, tmp)
			BTSQ(tmp, bits[i])
		}
	}

	Label("outer_loop")
	{
		// The outer loop only re-checks bounds after a whole batch of
		// iterations, and each iteration writes nSyms bytes per stream, so
		// the batch must satisfy iters*nSyms <= limit-op0. Dividing the byte
		// budget by the next power of two at or above nSyms is a cheap
		// conservative bound (5 and 7 -> 8, 14 -> 16).
		outShift := mbits.Len(uint(nSyms - 1))
		if nSyms > 1<<outShift {
			panic("output bound does not cover nSyms")
		}
		Commentf("Iterations allowed by the output: (limit - op0) / %d", 1<<outShift)
		iters := GP64()
		Load(ctx.Field("limit"), iters)
		SUBQ(op[0], iters)
		JLE(LabelRef("done"))
		SHRQ(U8(outShift), iters)

		Comment("Iterations allowed by the input: a reload backs a pointer up by at most 7 bytes")
		Comment("(whatever nSyms is), so every read stays inside the block while the lowest")
		Comment("pointer stays above ilowest.")
		ilowest, _ := ctx.Field("ilowest").Resolve()
		for i := range 4 {
			t := GP64()
			MOVQ(ip(i), t)
			SUBQ(ilowest.Addr, t)
			SHRQ(U8(3), t)
			CMPQ(t, iters)
			CMOVQCS(t, iters)
		}
		TESTQ(iters, iters)
		JZ(LabelRef("done"))
		IMUL3Q(Imm(uint64(nSyms)), iters, iters)
		ADDQ(op[0], iters)
		Store(iters, ctx.Field("inner"))
	}

	Label("inner_loop")
	for k := range nSyms {
		for i := range 4 {
			Commentf("stream %d, symbol %d", i, k)
			d.decodeSymbol(k, bits[i], op[i], peekBits, table)
		}
	}
	Comment("Reload the four bit containers")
	for i := range 4 {
		d.reload(bits[i], ip(i))
	}
	for i := range 4 {
		ADDQ(U8(nSyms), op[i])
	}
	{
		inner, _ := ctx.Field("inner").Resolve()
		CMPQ(op[0], inner.Addr)
		JB(LabelRef("inner_loop"))
	}
	JMP(LabelRef("outer_loop"))

	Label("done")
	Comment("Hand the state back: off = ip - in (negative when the window reached below the stream start),")
	Comment("value = the sentinel-form container. bitReaderShifted.restoreFromAsm normalizes both.")
	{
		br := GP64()
		Load(ctx.Field("pbr"), br)
		for i := range 4 {
			off := i * bitReader__size
			tmp := GP64()
			MOVQ(ip(i), tmp)
			SUBQ(Mem{Base: br, Disp: off + bitReader_in}, tmp)
			MOVQ(tmp, Mem{Base: br, Disp: off + bitReader_off})
			MOVQ(bits[i], Mem{Base: br, Disp: off + bitReader_value})
		}
	}
	{
		ctxout, _ := ctx.Field("out").Resolve()
		decoded := op[0]
		SUBQ(ctxout.Addr, decoded)
		SHLQ(U8(2), decoded) // decoded *= 4
		Store(decoded, ctx.Field("decoded"))
	}
	RET()
}

// decodeSymbol decodes one symbol from bits into op[k]: peek the top
// peekBits bits, look the entry up, shift the container by the entry's low
// byte (its bit length) and store the entry's high byte (the symbol).
func (d decompress4x) decodeSymbol(k int, bits, op, peekBits, table reg.GPVirtual) {
	val := GP64()
	if d.bmi2 {
		SHRXQ(peekBits, bits, val)
		MOVWQZX(Mem{Base: table, Index: val, Scale: 2}, val)
		SHLXQ(val, bits, bits)
		SHRQ(U8(8), val)
		MOVB(val.As8(), Mem{Base: op, Disp: k})
		return
	}
	// x86 variable shifts take their count in CL, so the generic twin
	// routes both counts through RCX.
	MOVQ(peekBits, reg.RCX)
	MOVQ(bits, val)
	SHRQ(reg.CL, val)
	MOVWQZX(Mem{Base: table, Index: val, Scale: 2}, reg.RCX)
	SHLQ(reg.CL, bits)
	SHRQ(U8(8), reg.RCX)
	MOVB(reg.CL, Mem{Base: op, Disp: k})
}

// reload tops up one bit container from its input pointer ip (a memory
// operand): the trailing zero count is the number of consumed bits, whole
// bytes move the pointer back, the fresh 8-byte window gets a new sentinel
// in bit 0, and the 0-7 leftover bits are re-applied as a shift.
func (d decompress4x) reload(bits reg.GPVirtual, ip Mem) {
	consumed := GP64()
	// TZCNT rather than BSF in both twins: the sentinel guarantees a set
	// bit, for which pre-BMI1 CPUs execute TZCNT (REP BSF) as BSF with the
	// same result, and unlike BSF its destination is write-only, so it does
	// not become a loop-carried register.
	TZCNTQ(bits, consumed)
	var nb, nb8 reg.Register
	if d.bmi2 {
		nb = GP64()
	} else {
		nb, nb8 = reg.RCX, reg.CL
	}
	MOVQ(consumed, nb)
	ANDQ(U8(7), nb)
	SHRQ(U8(3), consumed)
	// The old container is dead once its zero count is taken, so it holds
	// the input pointer until the fresh load overwrites it.
	MOVQ(ip, bits)
	SUBQ(consumed, bits)
	MOVQ(bits, ip)
	MOVQ(Mem{Base: bits}, bits)
	ORQ(U8(1), bits)
	if d.bmi2 {
		SHLXQ(nb, bits, bits)
	} else {
		SHLQ(nb8, bits)
	}
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
	MOVWQZX(Mem{Base: dt, Index: k, Scale: 2}, v.As64())

	// buf[id] = uint8(v.entry >> 8)
	MOVB(v.As8H(), out)

	// br.advance(uint8(v.entry))
	MOVBQZX(v.As8L(), v)
	br.advance(v)
}
