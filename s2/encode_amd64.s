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

// func emitRepeat(dst []byte, offset, length int) int
//
// All local variables fit into registers. The register allocation:
//	- AX	length
//	- SI	&dst[0]
//	- DI	i
//	- R11	offset
//
// The unusual register allocation of local variables, such as R11 for the
// offset, matches the allocation used at the call site in encodeBlock, which
// makes it easier to manually inline this function.
TEXT ·emitRepeat(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), SI
	MOVQ offset+24(FP), R11
	MOVQ length+32(FP), AX

	MOVQ AX, DX

	// length -= 4
	LEAQ -4(AX), AX

	// if length <= 4 (use copied value)
	CMPL DX, $8
	JLE  repeat_two

	// length < 8 && offset < 2048
	CMPL DX, $12
	JGE  cant_repeat_two_offset
	CMPL R11, $2048
	JLT  repeat_two_offset

cant_repeat_two_offset:
	CMPL AX, $260
	JLT  repeat_three // if length < (1<<8)+4
	CMPL AX, $65792
	JLT  repeat_four  // if length < (1 << 16) + (1 << 8)

	// Must be able to be within 5 bytes.
repeat_five:
	LEAQ -65536(AX), AX // length -= 65536
	MOVQ AX, R11
	MOVW $29, 0(SI)     // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	MOVW AX, 2(SI)      // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	SARQ $16, R11       // R11 = length >> 16
	MOVB R11, 4(SI)     // dst[4] = length >> 16
	MOVQ $5, DI         // i = 5
	JMP  repeat_final

repeat_four:
	LEAQ -256(AX), AX // length -= 256
	MOVW $25, 0(SI)   // dst[0] = 6<<2 | tagCopy1, dst[1] = 0
	MOVW AX, 2(SI)    // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	MOVQ $4, DI       // i = 3
	JMP  repeat_final

repeat_three:
	LEAQ -4(AX), AX   // length -= 4
	MOVW $21, 0(SI)   // dst[0] = 5<<2 | tagCopy1, dst[1] = 0
	MOVB AX, 2(SI)    // dst[2] = uint8(length)
	MOVQ $3, DI       // i = 3
	JMP  repeat_final

repeat_two:
	// dst[0] = uint8(length)<<2 | tagCopy1, dst[1] = 0
	SHLL $2, AX
	ORL  $1, AX
	MOVW AX, 0(SI)
	MOVQ $2, DI       // i = 2
	JMP  repeat_final

repeat_two_offset:
	// dst[0] = uint8(offset>>8)<<5 | uint8(length)<<2 | tagCopy1
	// dst[1] = uint8(offset)

	MOVB R11B, 1(SI)  // Store offset lower byte
	SARQ $8, R11      // Remove lower
	SHLL $5, R11      // Shift back up
	SHLL $2, AX       // Place lenght
	ORL  $1, AX       // Add tagCopy1
	ORL  R11, AX      // OR result
	MOVB AX, 0(SI)
	MOVQ $2, DI       // i = 2
	JMP  repeat_final

repeat_final:
	// Return the number of bytes written.
	MOVQ DI, ret+40(FP)
	RET
