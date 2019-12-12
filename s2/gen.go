//+build ignore

//go:generate go run gen.go -out encodeblock_amd64.s -stubs encodeblock_amd64.go

package main

import (
	. "github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/buildtags"
	. "github.com/mmcloughlin/avo/operand"
)

func main() {
	Constraint(buildtags.Not("appengine").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())
	Constraint(buildtags.Term("gc").ToConstraint())
	TEXT("encodeBlockAsm", NOSPLIT, "func(dst, src []byte) int")
	Doc("encodeBlock encodes a non-empty src to a guaranteed-large-enough dst.",
		"It assumes that the varint-encoded length of the decompressed bytes has already been written.", "")
	Pragma("noescape")

	// "var table [maxTableSize]uint32" takes up 65536 bytes of stack space. An
	// extra 56 bytes, to call other functions, and an extra 64 bytes, to spill
	// local variables (registers) during calls gives 65536 + 56 + 64 = 65656.
	const (
		tableSize  = 65536
		allocStack = 56 + 64 + tableSize
	)
	stack := AllocLocal(allocStack)
	table := stack.Offset(allocStack - tableSize)

	// Zero table
	iReg := GP64()
	MOVQ(U32(tableSize/8/16), iReg)
	tablePtr := GP64()
	LEAQ(table, tablePtr)
	XORQ(iReg, iReg)
	zeroXmm := XMM()
	PXOR(zeroXmm, zeroXmm)

	Label("zeroloop")
	for i := 0; i < 8; i++ {
		MOVUPS(zeroXmm, Mem{Base: tablePtr}.Offset(i*16))
	}
	ADDQ(Imm(16*8), tablePtr)
	DECQ(iReg)
	JNZ(LabelRef("zeroloop"))

	//src := Load(Param("src"), GP64())
	//dst := Load(Param("dst"), GP64())

	RET()
	Generate()
}
