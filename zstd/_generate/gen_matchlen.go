package main

//go:generate go run gen_matchlen.go -out ../matchlen_amd64.s -pkg=zstd

import (
	"flag"

	. "github.com/mmcloughlin/avo/build"
	"github.com/mmcloughlin/avo/buildtags"
	. "github.com/mmcloughlin/avo/operand"
)

func main() {
	flag.Parse()

	Constraint(buildtags.Not("appengine").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())
	Constraint(buildtags.Term("gc").ToConstraint())
	Constraint(buildtags.Not("noasm").ToConstraint())
	generateMatchLen()
	Generate()
}

func generateMatchLen() {
	Package("github.com/klauspost/compress/zstd")
	TEXT("matchLen", NOSPLIT, "func (a, b []byte) int")
	Pragma("noescape")
	Comment("load param")
	aptr := Load(Param("a").Base(), GP64())
	alen := Load(Param("a").Len(), GP64())
	bptr := Load(Param("b").Base(), GP64())
	equalMaskBits := GP64()
	ret := GP64()
	XORQ(ret, ret)

	Comment("find the minimum length slice")
	remainLen := alen

	Label("loop")
	{
		CMPQ(remainLen, U8(32))
		JB(LabelRef("last_loop"))
		Comment("load 32 bytes into YMM registers")
		adata := YMM()
		bdata := YMM()
		equalMaskBytes := YMM()
		VMOVDQU(Mem{Base: aptr}, adata)
		VMOVDQU(Mem{Base: bptr}, bdata)
		Comment("compare bytes in adata and bdata, like 'bytewise XNOR'",
			"if the byte is the same in adata and bdata, VPCMPEQB will store 0xFF in the same position in equalMaskBytes")
		VPCMPEQB(adata, bdata, equalMaskBytes)
		Comment("like convert byte to bit, store equalMaskBytes into general reg")
		VPMOVMSKB(equalMaskBytes, equalMaskBits.As32())
		CMPL(equalMaskBits.As32(), U32(0xffffffff))
		JNE(LabelRef("cal_prefix"))
		ADDQ(U8(32), aptr)
		ADDQ(U8(32), bptr)
		SUBQ(U8(32), remainLen)
		ADDQ(U8(32), ret)
		JMP(LabelRef("loop"))
	}

	Label("last_loop")
	{
		TESTQ(remainLen, remainLen)
		JZ(LabelRef("ret"))
		adata := YMM()
		bdata := YMM()
		equalMaskBytes := YMM()
		VMOVDQU(Mem{Base: aptr}, adata)
		VMOVDQU(Mem{Base: bptr}, bdata)
		VPCMPEQB(adata, bdata, equalMaskBytes)
		VPMOVMSKB(equalMaskBytes, equalMaskBits.As32())
		CMPL(equalMaskBits.As32(), U32(0xffffffff))
		JNE(LabelRef("cal_last_prefix"))
		Comment("if last bytes are all equal, just add remaining len on ret and return")
		ADDQ(remainLen, ret)
		JMP(LabelRef("ret"))
	}

	Label("cal_last_prefix")
	{
		matchedLen := GP64()
		NOTQ(equalMaskBits)
		Comment("store first not equal position into matchedLen")
		TZCNTQ(equalMaskBits, matchedLen)
		Comment("if matched len > remaining len, just add remaining on ret")
		CMPQ(remainLen, matchedLen)
		CMOVQLT(remainLen, matchedLen)
		ADDQ(matchedLen, ret)
		JMP(LabelRef("ret"))
	}

	Label("cal_prefix")
	{
		matchedLen := GP64()
		NOTQ(equalMaskBits)
		TZCNTQ(equalMaskBits, matchedLen)
		ADDQ(matchedLen, ret)
	}

	Label("ret")
	{
		Store(ret, ReturnIndex(0))
		RET()
	}
}
