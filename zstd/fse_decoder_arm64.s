// Hand-written arm64 port of the avo-generated buildDtable_asm.
// Mirrors fse_decoder_amd64.s / _generate/gen_fse.go and the pure-Go
// reference in fse_decoder_generic.go. Scalar only: this kernel is serial
// bitfield work, there is nothing here to vectorize with NEON.

//go:build arm64 && !appengine && !noasm && gc

#include "textflag.h"

// fseDecoder field offsets (see fse_decoder.go):
//   dt             [512]decSymbol -> offset 0    (each decSymbol is uint64)
//   symbolLen      uint16         -> offset 4096
//   actualTableLog uint8          -> offset 4098
//
// decSymbol uint64 layout (little-endian):
//   byte 0   nBits
//   byte 1   addBits
//   bytes2-3 newState (uint16)
//   bytes4-7 baseline (uint32)
//
// buildDtableAsmContext layout:
//   stateTable *uint16 -> 0   (also used as symbolNext[:256])
//   norm       *int16  -> 8
//   dt         *uint64 -> 16
//   errParam1  uint64  -> 24
//   errParam2  uint64  -> 32

// func buildDtable_asm(s *fseDecoder, ctx *buildDtableAsmContext) int
TEXT ·buildDtable_asm(SB), NOSPLIT, $0-24
	MOVD  s+0(FP), R0
	MOVD  ctx+8(FP), R1

	// Load values.
	MOVBU 4098(R0), R2     // R2 = actualTableLog
	MOVD  $1, R3
	LSL   R2, R3, R3       // R3 = tableSize = 1<<actualTableLog
	SUB   $1, R3, R8       // R8 = highThreshold = tableSize-1
	MOVD  (R1), R4         // R4 = stateTable (symbolNext)
	MOVD  8(R1), R5        // R5 = norm
	MOVD  16(R1), R6       // R6 = dt
	MOVHU 4096(R0), R7     // R7 = symbolLen

	// Init, lay down lowprob symbols.
	MOVD  $0, R9           // i = 0
	JMP   init_loop_cond

init_loop:
	ADD   R9<<1, R5, R10   // &norm[i]
	MOVH  (R10), R10       // R10 = norm[i] (sign-extended)
	CMN   $1, R10          // set Z if norm[i] == -1
	BNE   init_not_low
	ADD   R8<<3, R6, R11   // &dt[highThreshold]
	MOVB  R9, 1(R11)       // dt[highThreshold].setAddBits(uint8(i))
	SUB   $1, R8, R8       // highThreshold--
	MOVD  $1, R10          // v = 1

init_not_low:
	ADD   R9<<1, R4, R11   // &symbolNext[i]
	MOVH  R10, (R11)       // symbolNext[i] = uint16(v)
	ADD   $1, R9, R9       // i++

init_loop_cond:
	CMP   R7, R9
	BLT   init_loop

	// Spread symbols.
	// step = (tableSize>>1) + (tableSize>>3) + 3
	LSR   $1, R3, R9
	LSR   $3, R3, R10
	ADD   R10, R9, R9
	ADD   $3, R9, R9       // R9 = step
	SUB   $1, R3, R10      // R10 = tableMask = tableSize-1
	MOVD  $0, R11          // position = 0
	MOVD  $0, R12          // ss = 0
	JMP   spread_main_cond

spread_main:
	MOVD  $0, R13          // i = 0
	ADD   R12<<1, R5, R14  // &norm[ss]
	MOVH  (R14), R14       // R14 = norm[ss] (signed)
	JMP   spread_inner_cond

spread_inner:
	ADD   R11<<3, R6, R15  // &dt[position]
	MOVB  R12, 1(R15)      // dt[position].setAddBits(uint8(ss))

adjust_position:
	ADD   R9, R11, R11     // position += step
	AND   R10, R11, R11    // position &= tableMask
	CMP   R8, R11          // position vs highThreshold
	BGT   adjust_position  // while position > highThreshold
	ADD   $1, R13, R13     // i++

spread_inner_cond:
	CMP   R14, R13         // i vs v (signed: v == -1 -> no iterations)
	BLT   spread_inner
	ADD   $1, R12, R12     // ss++

spread_main_cond:
	CMP   R7, R12          // ss vs symbolLen
	BLT   spread_main
	CBZ   R11, build_table_start

	// error: position != 0 (corrupted normalized counter)
	MOVD  R11, 24(R1)      // errParam1 = position
	MOVD  $1, R20
	MOVD  R20, ret+16(FP)
	RET

build_table_start:
	MOVD  $0, R7           // u = 0 (symbolLen no longer needed)
	JMP   build_cond

build_loop:
	ADD   R7<<3, R6, R19   // R19 = &dt[u]
	MOVBU 1(R19), R10      // symbol = dt[u].addBits()
	ADD   R10<<1, R4, R11  // &symbolNext[symbol]
	MOVHU (R11), R12       // nextState = symbolNext[symbol]
	ADD   $1, R12, R13
	MOVH  R13, (R11)       // symbolNext[symbol] = nextState + 1

	// nBits = actualTableLog - highBits(nextState), highBits = 31 - clz32(nextState)
	CLZW  R12, R14         // R14 = clz32(nextState)
	MOVD  $31, R15
	SUB   R14, R15, R15    // R15 = highBits
	SUB   R15, R2, R16     // R16 = nBits = actualTableLog - highBits

	// newState = (nextState << nBits) - tableSize
	LSL   R16, R12, R17
	SUB   R3, R17, R17     // R17 = newState

	MOVB  R16, (R19)       // dt[u].setNBits(nBits)
	MOVH  R17, 2(R19)      // dt[u].setNewState(newState)

	// error: newState > tableSize
	CMP   R3, R17
	BLE   build_check1_ok
	MOVD  R17, 24(R1)      // errParam1 = newState
	MOVD  R3, 32(R1)       // errParam2 = tableSize
	MOVD  $2, R20
	MOVD  R20, ret+16(FP)
	RET

build_check1_ok:
	// error: newState == u && nBits == 0
	CBNZ  R16, build_check2_ok
	CMP   R7, R17
	BNE   build_check2_ok
	MOVD  R17, 24(R1)      // errParam1 = newState
	MOVD  R7, 32(R1)       // errParam2 = u
	MOVD  $3, R20
	MOVD  R20, ret+16(FP)
	RET

build_check2_ok:
	ADD   $1, R7, R7       // u++

build_cond:
	CMP   R3, R7           // u vs tableSize
	BLT   build_loop

	MOVD  $0, R20
	MOVD  R20, ret+16(FP)
	RET
