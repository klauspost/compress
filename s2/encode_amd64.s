// +build !appengine
// +build gc
// +build !noasm

#include "textflag.h"

// func emitLiteral(dst, lit []byte) int
//
// All local variables fit into registers. The register allocation:
//	- AX	len(lit)
//	- BX	n
//  - CX    n >> 16
//	- DX	return value
//	- DI	&dst[i]
//	- R10	&lit[0]
//
// The 24 bytes of stack space is to call runtime·memmove.
//
TEXT ·emitLiteral(SB), NOSPLIT, $24-56
	MOVQ dst_base+0(FP), DI
	MOVQ lit_base+24(FP), R10
	MOVQ lit_len+32(FP), AX
	MOVQ AX, DX
	MOVL AX, BX

	SUBL $1, BX

	// Return if AX was 0
	JC zero_end

	MOVQ BX, CX
	CMPL BX, $60
	JLT  oneByte
	CMPL BX, $256
	JLT  twoBytes
	CMPL BX, $65536
	JLT  threeBytes
	CMPL BX, $16777216
	JLT  fourBytes

fiveBytes:
	MOVB $252, 0(DI)
	MOVL BX, 1(DI)
	ADDQ $5, DI
	ADDQ $5, DX
	JMP  memmove

fourBytes:
	SHRL $16, CX
	MOVB $248, 0(DI)
	MOVW BX, 1(DI)
	MOVB CX, 3(DI)
	ADDQ $4, DI
	ADDQ $4, DX
	JMP  memmove

threeBytes:
	MOVB $0xf4, 0(DI)
	MOVW BX, 1(DI)
	ADDQ $3, DI
	ADDQ $3, DX
	JMP  memmove

twoBytes:
	MOVB $0xf0, 0(DI)
	MOVB BX, 1(DI)
	ADDQ $2, DI
	ADDQ $2, DX
	JMP  memmove

oneByte:
	SHLB $2, BX
	MOVB BX, 0(DI)
	ADDQ $1, DI
	ADDQ $1, DX

memmove:
	// Store return value
	MOVQ DX, ret+48(FP)

	// copy(dst[i:], lit)
	//
	// This means calling runtime·memmove(&dst[i], &lit[0], len(lit)), so we push
	// DI, R10 and AX as arguments.
	MOVQ DI, 0(SP)
	MOVQ R10, 8(SP)
	MOVQ AX, 16(SP)
	CALL runtime·memmove(SB)
	RET

zero_end:
	MOVQ $0, ret+48(FP)
	RET
