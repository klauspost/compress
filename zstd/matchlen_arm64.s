// Copied from S2 implementation.

//go:build !appengine && !noasm && gc && !noasm

#include "textflag.h"

// func matchLen(a []byte, b []byte) int
// Requires: BMI
TEXT ·matchLen(SB), NOSPLIT, $0-56
	MOVD a_base+0(FP), R0
	MOVD b_base+24(FP), R1
	MOVD a_len+8(FP), R2

	// matchLen
	MOVD $0, R5

matchlen_loopback_16_standalone:
	CMPW  $0x10, R2
	BLO   matchlen_match8_standalone
	MOVD  (R0)(R5), R3
	ADD   R5, R0, R15
	MOVD  8(R15), R6
	MOVD  (R1)(R5), R16
	EOR   R16, R3, R3
	TST   R3, R3
	BNE   matchlen_bsf_8_standalone
	ADD   R5, R1, R15
	MOVD  8(R15), R16
	EOR   R16, R6, R6
	TST   R6, R6
	BNE   matchlen_bsf_16standalone
	SUB   $16, R2, R2
	MOVWU R2, R2
	ADD   $16, R5, R5
	MOVWU R5, R5
	JMP   matchlen_loopback_16_standalone

matchlen_bsf_16standalone:
#ifdef GOAMD64_v3
	RBIT R6, R6
	CLZ  R6, R6

#else
	RBIT R6, R6
	CLZ  R6, R6

#endif
	ASR   $0x03, R6, R6
	ADD   R6, R5, R5
	ADD   $8, R5, R5
	MOVWU R5, R5
	JMP   gen_match_len_end

matchlen_match8_standalone:
	CMPW  $0x08, R2
	BLO   matchlen_match4_standalone
	MOVD  (R0)(R5), R3
	MOVD  (R1)(R5), R16
	EOR   R16, R3, R3
	TST   R3, R3
	BNE   matchlen_bsf_8_standalone
	SUB   $8, R2, R2
	MOVWU R2, R2
	ADD   $8, R5, R5
	MOVWU R5, R5
	JMP   matchlen_match4_standalone

matchlen_bsf_8_standalone:
#ifdef GOAMD64_v3
	RBIT R3, R3
	CLZ  R3, R3

#else
	RBIT R3, R3
	CLZ  R3, R3

#endif
	ASR   $0x03, R3, R3
	ADD   R3, R5, R5
	MOVWU R5, R5
	JMP   gen_match_len_end

matchlen_match4_standalone:
	CMPW  $0x04, R2
	BLO   matchlen_match2_standalone
	MOVWU (R0)(R5), R3
	MOVD  (R1)(R5), R16
	CMPW  R3, R16
	BNE   matchlen_match2_standalone
	SUB   $4, R2, R2
	MOVWU R2, R2
	ADD   $4, R5, R5
	MOVWU R5, R5

matchlen_match2_standalone:
	CMPW  $0x01, R2
	BEQ   matchlen_match1_standalone
	BLO   gen_match_len_end
	MOVHU (R0)(R5), R3
	MOVHU (R1)(R5), R15
	AND   $0xffff, R3, R16
	CMP   R16, R15
	BNE   matchlen_match1_standalone
	ADD   $2, R5, R5
	MOVWU R5, R5
	SUBSW $0x02, R2, R2
	BEQ   gen_match_len_end

matchlen_match1_standalone:
	MOVBU (R0)(R5), R16
	BFI   $0, R16, $8, R3
	MOVBU (R1)(R5), R15
	AND   $0xff, R3, R16
	CMP   R16, R15
	BNE   gen_match_len_end
	ADD   $1, R5, R5
	MOVWU R5, R5

gen_match_len_end:
	MOVD R5, ret+48(FP)
	RET
