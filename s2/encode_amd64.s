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
//  - RDX   temp
//
// The unusual register allocation of local variables, such as R11 for the
// offset, matches the allocation used at the call site in encodeBlock, which
// makes it easier to manually inline this function.
TEXT ·emitRepeat(SB), NOSPLIT, $0-48
	MOVQ dst_base+0(FP), SI
	MOVQ offset+24(FP), R11
	MOVQ length+32(FP), AX

	// bytes written
	XORQ DI, DI

	// Next
emit_repeat_again:
	MOVQ AX, DX     // Copy length
	LEAQ -4(AX), AX // length -= 4

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
	JLT  repeat_three  // if length < (1<<8)+4
	CMPL AX, $65792
	JLT  repeat_four   // if length < (1 << 16) + (1 << 8)
	CMPL AX, $16842751 // 16777215+65536
	JLE  repeat_five

	// We have have more than 24 bits
	// Emit so we have at least 4 bytes left.
	LEAQ -16842747(AX), AX // length -= (maxRepeat - 4) + 65536
	MOVW $29, 0(SI)        // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	MOVW $65531, 2(SI)     // 0xfffb
	MOVB $255, 4(SI)
	ADDQ $5, SI
	ADDQ $5, DI
	JMP  emit_repeat_again

// Must be able to be within 5 bytes.
repeat_five:
	LEAQ -65536(AX), AX // length -= 65536
	MOVQ AX, R11
	MOVW $29, 0(SI)     // dst[0] = 7<<2 | tagCopy1, dst[1] = 0
	MOVW AX, 2(SI)      // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	SARQ $16, R11       // R11 = length >> 16
	MOVB R11, 4(SI)     // dst[4] = length >> 16
	ADDQ $5, DI         // i += 5
	JMP  repeat_final

repeat_four:
	LEAQ -256(AX), AX // length -= 256
	MOVW $25, 0(SI)   // dst[0] = 6<<2 | tagCopy1, dst[1] = 0
	MOVW AX, 2(SI)    // dst[2] = uint8(length), dst[3] = uint8(length >> 8)
	ADDQ $4, DI       // i += 4
	JMP  repeat_final

repeat_three:
	LEAQ -4(AX), AX   // length -= 4
	MOVW $21, 0(SI)   // dst[0] = 5<<2 | tagCopy1, dst[1] = 0
	MOVB AX, 2(SI)    // dst[2] = uint8(length)
	ADDQ $3, DI       // i += 3
	JMP  repeat_final

repeat_two:
	// dst[0] = uint8(length)<<2 | tagCopy1, dst[1] = 0
	SHLL $2, AX
	ORL  $1, AX
	MOVW AX, 0(SI)
	ADDQ $2, DI       // i += 2
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
	ADDQ $2, DI       // i += 2
	JMP  repeat_final

repeat_final:
	// Return the number of bytes written.
	MOVQ DI, ret+40(FP)
	RET

// returns the multiplier for 6 bytes hashes in tmp.
#define hash6mul_tmp(tmp) MOVQ	$227718039650203, tmp

// Create hash of 6 lowest bytes.
// Multiplier from hash6mul_tmp must be in mul_tmp
#define hash6(val, mul_tmp) SHLQ $16, val \
	IMULQ mul_tmp, val \
	SHRQ  $50, val

// func encodeBlockAsmBlah(dst, src []byte) (d int)
//
// "var table [maxTableSize]uint32" takes up 65536 bytes of stack space. An
// extra 56 bytes, to call other functions, and an extra 64 bytes, to spill
// local variables (registers) during calls gives 65536 + 56 + 64 = 65656.
TEXT ·encodeBlockAsmBlah(SB), 0, $65656-56
#define DST_PTR  DI
#define SRC_BASE SI
#define SRC_LEN  R14
	MOVQ dst_base+0(FP), DST_PTR
	MOVQ src_base+24(FP), SRC_BASE
	MOVQ src_len+32(FP), SRC_LEN

	// Clear 64KB
#define COUNT CX
#define TABLE BX
	PXOR X0, X0
	LEAQ table-65536(SP), TABLE
	MOVQ $512, COUNT            // 65536/128 = 512

memclr:
	MOVOU X0, 0(TABLE)
	MOVOU X0, 16(TABLE)
	MOVOU X0, 32(TABLE)
	MOVOU X0, 48(TABLE)
	MOVOU X0, 64(TABLE)
	MOVOU X0, 80(TABLE)
	MOVOU X0, 96(TABLE)
	MOVOU X0, 112(TABLE)
	ADDQ  $128, TABLE
	SUBQ  $1, COUNT
	JNZ   memclr

#undef COUNT
#undef TABLE

#define S AX
#define CV CX
#define REPEAT DX
	MOVQ 0(SRC_BASE), CV
	MOVQ $1, REPEAT

encode_loop:
#define CANDIDATE R8
#define TABLE BX
	LEAQ table-65536(SP), TABLE

search_loop:
#define HASH_MUL R9
#define CANDIDATE2 R10
#define CVONEDOWN R13
	MOVQ CV, CANDIDATE
	MOVQ CV, CANDIDATE2
	SHRQ $8, CANDIDATE2
	hash6mul_tmp(HASH_MUL)
	hash6(CANDIDATE, HASH_MUL)
	MOVQ CANDIDATE2, CVONEDOWN
	hash6(CANDIDATE2, HASH_MUL)

#undef HASH_MUL
#define C1_VAL R11
#define C2_VAL R12
#define SPLUSONE R15
	LEAQ 1(S), SPLUSONE
	MOVD 0(TABLE)(CANDIDATE*4), C1_VAL
	MOVD 0(TABLE)(CANDIDATE2*4), C2_VAL
	MOVD S, 0(TABLE)(CANDIDATE*4)
	MOVD SPLUSONE, 0(TABLE)(CANDIDATE2*4)

#undef TABLE
#undef SPLUSONE
#define REPEATVAL R15
#define REPEATOFF R14
// LEAQ (SRC_BASE)

// Check repeat
// MOVQ (), REPEATVAL

#undef REPEATVAL
#undef REPEATOFF
#undef CVONEDOWN
#undef C1_VAL
#undef C2_VAL

#undef CV

encodeBlockEnd:
#define DST_BASE AX
	MOVQ dst_base+0(FP), DST_BASE
	SUBQ DST_BASE, DST_PTR

#undef DST_BASE
	MOVQ DST_PTR, d+48(FP)
	RET

