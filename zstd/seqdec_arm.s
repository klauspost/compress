#include "textflag.h"

// func sequenceDecs_decodeSync_arm(ctx *armSeqCtx) int32
//
// 32-bit ARM implementation of the zstd sequence decoder + executor. It is a
// transliteration of the generic decodeSync loop in seqdec.go, using a bit
// reader split into two 32-bit words (hi:lo) instead of a uint64, and inline
// 8/4/2/1-byte copies instead of runtime.memmove calls.
//
// Register assignment in the main loop:
//   R0  bitsRead          R1  value hi          R2  value lo
//   R3  cursor            R7  out write ptr     R8  literals read ptr
//   R4 R5 R6 R9 R11 R12   scratch
//
// R10 (g) and R11 (the assembler's REGTMP) must not hold long-lived values:
// the assembler itself clobbers R11 when it needs a temporary for a large
// immediate, and the runtime requires R10 to remain valid for signal handling.

#define CTXP      ctxp-4(SP)
#define OUTBASE   outbase-8(SP)
#define LITBASE   litbase-12(SP)
#define LITEND    litend-16(SP)
#define HISTBASE  histbase-20(SP)
#define LLTAB     lltab-24(SP)
#define MLTAB     mltab-28(SP)
#define OFTAB     oftab-32(SP)
#define PREV0     prev0-36(SP)
#define PREV1     prev1-40(SP)
#define PREV2     prev2-44(SP)
#define HISTLEN   histlen-48(SP)
#define WINDOW    window-52(SP)
#define MAXBLOCK  maxblock-56(SP)
#define STARTSZ   startsz-60(SP)
#define MAXBITS   maxbits-64(SP)
#define NSEQ      nseq-68(SP)
#define LLV       llv-72(SP)
#define MLV       mlv-76(SP)
#define MOV       mov-80(SP)
#define LLHDR     llhdr-84(SP)
#define LLBASE    llbase-88(SP)
#define MLHDR     mlhdr-92(SP)
#define MLBASE    mlbase-96(SP)
#define OFHDR     ofhdr-100(SP)
#define OFBASE    ofbase-104(SP)
#define TMP1      tmp1-112(SP)
#define INBASE    inbase-116(SP)

TEXT ·sequenceDecs_decodeSync_arm(SB), NOSPLIT, $128-8
	MOVW ctx+0(FP), R9
	MOVW R9, CTXP
	MOVW 0(R9), R11
	MOVW R11, INBASE
	MOVW 4(R9), R3        // cursor
	MOVW 8(R9), R0        // bitsRead
	MOVW 12(R9), R1       // hi
	MOVW 16(R9), R2       // lo
	MOVW 20(R9), R11
	MOVW R11, LLTAB
	MOVW 24(R9), R11
	MOVW R11, MLTAB
	MOVW 28(R9), R11
	MOVW R11, OFTAB
	MOVW 32(R9), R11
	MOVW R11, LLHDR
	MOVW 36(R9), R11
	MOVW R11, LLBASE
	MOVW 40(R9), R11
	MOVW R11, MLHDR
	MOVW 44(R9), R11
	MOVW R11, MLBASE
	MOVW 48(R9), R11
	MOVW R11, OFHDR
	MOVW 52(R9), R11
	MOVW R11, OFBASE
	MOVW 56(R9), R11
	MOVW R11, PREV0
	MOVW 60(R9), R11
	MOVW R11, PREV1
	MOVW 64(R9), R11
	MOVW R11, PREV2
	MOVW 68(R9), R11
	MOVW R11, NSEQ
	MOVW 72(R9), R11
	MOVW R11, OUTBASE
	MOVW 76(R9), R7       // outLen
	ADD R11, R7, R7       // outPtr
	MOVW 84(R9), R11
	MOVW R11, LITBASE
	MOVW 88(R9), R4       // litLen
	MOVW 92(R9), R8       // litPos
	ADD R11, R8, R8       // litPtr
	ADD R4, R11, R4       // litEnd
	MOVW R4, LITEND
	MOVW 96(R9), R11
	MOVW R11, HISTBASE
	MOVW 100(R9), R11
	MOVW R11, HISTLEN
	MOVW 104(R9), R11
	MOVW R11, WINDOW
	MOVW 108(R9), R11
	MOVW R11, MAXBLOCK
	MOVW 112(R9), R11
	MOVW R11, STARTSZ
	MOVW 116(R9), R11
	MOVW R11, MAXBITS

mainloop:
	CMP $64, R0
	BHI err_overread

	// ---- offset ----
	MOVW OFHDR, R4
	MOVW OFBASE, R6
	MOVW R6, MOV
	CMP $32, R0
	BLO of_fill_done
	CMP $4, R3
	BLO of_fill_done_byte
	SUB $4, R3
	MOVW INBASE, R9
	ADD R3, R9, R9
	MOVW R2, R12
	MOVW (R9), R2
	MOVW R12, R1
	SUB $32, R0
	B of_fill_done
of_fill_done_byte:
	SLL $3, R3, R9
	SUB R9, R0
of_fill_done_bloop:
	CMP $0, R3
	BEQ of_fill_done
	SUB $1, R3
	MOVW INBASE, R9
	ADD R3, R9, R9
	MOVBU (R9), R12
	MOVW R1, R9
	SLL $8, R9
	MOVW R2, R11
	SRL $24, R11
	ORR R11, R9
	MOVW R9, R1
	SLL $8, R2
	ORR R12, R2
	B of_fill_done_bloop
of_fill_done:
	SRL $8, R4, R5
	AND $0xff, R5
	// getBits(R5) -> R4
	MOVW R0, R9
	CMP $32, R9
	BHS of_gb_hi
	MOVW R1, R4
	SLL R9, R4
	MOVW $32, R12
	SUB R9, R12, R12
	MOVW R2, R9
	SRL R12, R9
	ORR R9, R4
	B of_gb_shift
of_gb_hi:
	SUB $32, R9, R9
	MOVW R2, R4
	SLL R9, R4
of_gb_shift:
	ADD R5, R0, R0
	MOVW $32, R12
	SUB R5, R12, R12
	SRL R12, R4
	MOVW MOV, R6
	ADD R4, R6, R6
	MOVW R6, MOV

	// ---- match length ----
	MOVW MAXBITS, R9
	CMP $32, R9
	BLE ml_nofill
	CMP $32, R0
	BLO ml_nofill
	CMP $4, R3
	BLO ml2_byte
	SUB $4, R3
	MOVW INBASE, R9
	ADD R3, R9, R9
	MOVW R2, R12
	MOVW (R9), R2
	MOVW R12, R1
	SUB $32, R0
	B ml_nofill
ml2_byte:
	SLL $3, R3, R9
	SUB R9, R0
ml2_bloop:
	CMP $0, R3
	BEQ ml_nofill
	SUB $1, R3
	MOVW INBASE, R9
	ADD R3, R9, R9
	MOVBU (R9), R12
	MOVW R1, R9
	SLL $8, R9
	MOVW R2, R11
	SRL $24, R11
	ORR R11, R9
	MOVW R9, R1
	SLL $8, R2
	ORR R12, R2
	B ml2_bloop
ml_nofill:
	MOVW MLHDR, R4
	MOVW MLBASE, R6
	SRL $8, R4, R5
	AND $0xff, R5
	MOVW R0, R9
	CMP $32, R9
	BHS ml_gb_hi
	MOVW R1, R4
	SLL R9, R4
	MOVW $32, R12
	SUB R9, R12, R12
	MOVW R2, R9
	SRL R12, R9
	ORR R9, R4
	B ml_gb_shift
ml_gb_hi:
	SUB $32, R9, R9
	MOVW R2, R4
	SLL R9, R4
ml_gb_shift:
	ADD R5, R0, R0
	MOVW $32, R12
	SUB R5, R12, R12
	SRL R12, R4
	ADD R4, R6, R6
	MOVW R6, MLV

	// ---- literal length ----
	MOVW LLHDR, R4
	MOVW LLBASE, R6
	SRL $8, R4, R5
	AND $0xff, R5
	MOVW R0, R9
	CMP $32, R9
	BHS ll_gb_hi
	MOVW R1, R4
	SLL R9, R4
	MOVW $32, R12
	SUB R9, R12, R12
	MOVW R2, R9
	SRL R12, R9
	ORR R9, R4
	B ll_gb_shift
ll_gb_hi:
	SUB $32, R9, R9
	MOVW R2, R4
	SLL R9, R4
ll_gb_shift:
	ADD R5, R0, R0
	MOVW $32, R12
	SUB R5, R12, R12
	SRL R12, R4
	ADD R4, R6, R6
	MOVW R6, LLV

	// ---- adjust offset ----
	MOVW OFHDR, R9
	SRL $8, R9
	AND $0xff, R9
	MOVW MOV, R6
	CMP $1, R9
	BLS adj_repeat
	MOVW PREV1, R11
	MOVW R11, PREV2
	MOVW PREV0, R11
	MOVW R11, PREV1
	MOVW R6, PREV0
	B adj_done
adj_repeat:
	MOVW LLV, R4
	CMP $0, R4
	BNE adj_noinc
	ADD $1, R6
adj_noinc:
	CMP $0, R6
	BNE adj_nonzero
	MOVW PREV0, R6
	B adj_done
adj_nonzero:
	CMP $3, R6
	BNE adj_idx
	MOVW PREV0, R11
	SUB $1, R11
	B adj_temp
adj_idx:
	CMP $1, R6
	BNE adj_idx2
	MOVW PREV1, R11
	B adj_temp
adj_idx2:
	MOVW PREV2, R11
adj_temp:
	CMP $0, R11
	BNE adj_tok
	MOVW $1, R11
adj_tok:
	CMP $1, R6
	BEQ adj_noshift
	MOVW PREV1, R9
	MOVW R9, PREV2
adj_noshift:
	MOVW PREV0, R9
	MOVW R9, PREV1
	MOVW R11, PREV0
	MOVW R11, R6
adj_done:
	MOVW R6, MOV

	// Fill for the state updates.
	CMP $32, R0
	BLO st_fill_done
	CMP $4, R3
	BLO st_fill_done_byte
	SUB $4, R3
	MOVW INBASE, R9
	ADD R3, R9, R9
	MOVW R2, R12
	MOVW (R9), R2
	MOVW R12, R1
	SUB $32, R0
	B st_fill_done
st_fill_done_byte:
	SLL $3, R3, R9
	SUB R9, R0
st_fill_done_bloop:
	CMP $0, R3
	BEQ st_fill_done
	SUB $1, R3
	MOVW INBASE, R9
	ADD R3, R9, R9
	MOVBU (R9), R12
	MOVW R1, R9
	SLL $8, R9
	MOVW R2, R11
	SRL $24, R11
	ORR R11, R9
	MOVW R9, R1
	SLL $8, R2
	ORR R12, R2
	B st_fill_done_bloop
st_fill_done:

	// ---- bounds ----
	MOVW LLV, R9          // ll
	MOVW LITEND, R5
	SUB R8, R5, R5        // literal bytes left
	CMP R5, R9
	BHI err_lit
	MOVW MLV, R6
	ADD R9, R6, R5        // ll + ml
	MOVW OUTBASE, R11
	SUB R11, R7, R4       // outPos
	ADD R4, R5, R5
	MOVW STARTSZ, R4
	SUB R4, R5, R5
	MOVW MAXBLOCK, R4
	CMP R4, R5
	BHI err_space
	MOVW MLV, R6
	CMP $131074, R6
	BHI err_mllen

	// ---- copy literals ----
	CMP $8, R9
	BLO lit_tail
lit_l8:
	MOVW (R8), R4
	MOVW 4(R8), R5
	MOVW R4, (R7)
	MOVW R5, 4(R7)
	ADD $8, R7
	ADD $8, R8
	SUB $8, R9
	CMP $8, R9
	BHS lit_l8
lit_tail:
	CMP $4, R9
	BLO lit_t2
	MOVW (R8), R4
	MOVW R4, (R7)
	ADD $4, R7
	ADD $4, R8
	SUB $4, R9
lit_t2:
	CMP $2, R9
	BLO lit_t1
	MOVHU (R8), R4
	MOVH R4, (R7)
	ADD $2, R7
	ADD $2, R8
	SUB $2, R9
lit_t1:
	CMP $0, R9
	BEQ lit_done
	MOVBU (R8), R4
	MOVB R4, (R7)
	ADD $1, R7
	ADD $1, R8
lit_done:

	// ---- match ----
	MOVW MOV, R6
	MOVW MLV, R9
	CMP $0, R9
	BEQ seq_done
	CMP $0, R6
	BEQ err_mismatch
	MOVW OUTBASE, R11
	SUB R11, R7, R5       // outPos
	MOVW HISTLEN, R4
	ADD R5, R4, R4        // outPos + histLen
	CMP R4, R6
	BHI err_moff
	MOVW WINDOW, R4
	CMP R4, R6
	BHI err_moff
	CMP R5, R6            // mo > outPos ?
	BLS match_out
	// v := mo - outPos, start := histLen - v
	MOVW R6, R4
	SUB R5, R4, R4
	MOVW R4, TMP1
	MOVW HISTLEN, R5
	SUB R4, R5, R5
	MOVW HISTBASE, R12
	ADD R5, R12, R12
	MOVW MLV, R9
	CMP R4, R9
	BLS hist_all
	// copy v bytes, then the rest from out
	MOVW R4, R5
	CMP $8, R5
	BLO hist_l_tail
hist_l8:
	MOVW (R12), R4
	MOVW 4(R12), R6
	MOVW R4, (R7)
	MOVW R6, 4(R7)
	ADD $8, R12
	ADD $8, R7
	SUB $8, R5
	CMP $8, R5
	BHS hist_l8
hist_l_tail:
	CMP $4, R5
	BLO hist_l_t2
	MOVW (R12), R4
	MOVW R4, (R7)
	ADD $4, R12
	ADD $4, R7
	SUB $4, R5
hist_l_t2:
	CMP $2, R5
	BLO hist_l_t1
	MOVHU (R12), R4
	MOVH R4, (R7)
	ADD $2, R12
	ADD $2, R7
	SUB $2, R5
hist_l_t1:
	CMP $0, R5
	BEQ hist_l_done
	MOVBU (R12), R4
	MOVB R4, (R7)
	ADD $1, R12
	ADD $1, R7
hist_l_done:
	MOVW MLV, R9
	MOVW TMP1, R4
	SUB R4, R9, R9
	MOVW MOV, R6
	B match_out
hist_all:
	MOVW R9, R5
	CMP $8, R5
	BLO hist_a_tail
hist_a8:
	MOVW (R12), R4
	MOVW 4(R12), R6
	MOVW R4, (R7)
	MOVW R6, 4(R7)
	ADD $8, R12
	ADD $8, R7
	SUB $8, R5
	CMP $8, R5
	BHS hist_a8
hist_a_tail:
	CMP $4, R5
	BLO hist_a_t2
	MOVW (R12), R4
	MOVW R4, (R7)
	ADD $4, R12
	ADD $4, R7
	SUB $4, R5
hist_a_t2:
	CMP $2, R5
	BLO hist_a_t1
	MOVHU (R12), R4
	MOVH R4, (R7)
	ADD $2, R12
	ADD $2, R7
	SUB $2, R5
hist_a_t1:
	CMP $0, R5
	BEQ seq_done
	MOVBU (R12), R4
	MOVB R4, (R7)
	ADD $1, R7
	B seq_done

match_out:
	// src = outPtr - mo
	MOVW R7, R12
	SUB R6, R12, R12
	CMP $8, R6
	BLO match_ovl
match_l8:
	CMP $8, R9
	BLO match_tail
	MOVW (R12), R4
	MOVW 4(R12), R5
	MOVW R4, (R7)
	MOVW R5, 4(R7)
	ADD $8, R12
	ADD $8, R7
	SUB $8, R9
	B match_l8
match_tail:
	CMP $4, R9
	BLO match_t2
	MOVW (R12), R4
	MOVW R4, (R7)
	ADD $4, R12
	ADD $4, R7
	SUB $4, R9
match_t2:
	CMP $2, R9
	BLO match_t1
	MOVHU (R12), R4
	MOVH R4, (R7)
	ADD $2, R12
	ADD $2, R7
	SUB $2, R9
match_t1:
	CMP $0, R9
	BEQ seq_done
	MOVBU (R12), R4
	MOVB R4, (R7)
	ADD $1, R7
	B seq_done
match_ovl:
	CMP $0, R9
	BEQ seq_done
match_ovl_loop:
	MOVBU (R12), R4
	MOVB R4, (R7)
	ADD $1, R12
	ADD $1, R7
	SUB $1, R9
	CMP $0, R9
	BNE match_ovl_loop

seq_done:
	MOVW NSEQ, R9
	SUB $1, R9
	MOVW R9, NSEQ
	CMP $0, R9
	BEQ finish

	// ---- update FSE states ----
	MOVW LLHDR, R4
	AND $0xff, R4, R9
	MOVW MLHDR, R5
	AND $0xff, R5, R11
	ADD R11, R9
	MOVW OFHDR, R6
	AND $0xff, R6, R12
	ADD R12, R9
	CMP $0, R9
	BEQ upd_zero
	MOVW R9, R5
	MOVW R0, R9
	CMP $32, R9
	BHS upd_gb_hi
	MOVW R1, R4
	SLL R9, R4
	MOVW $32, R12
	SUB R9, R12, R12
	MOVW R2, R9
	SRL R12, R9
	ORR R9, R4
	B upd_gb_shift
upd_gb_hi:
	SUB $32, R9, R9
	MOVW R2, R4
	SLL R9, R4
upd_gb_shift:
	ADD R5, R0, R0
	MOVW $32, R12
	SUB R5, R12, R12
	SRL R12, R4
	MOVW R4, TMP1
	B upd_ll

upd_zero:
	MOVW $0, R4
	MOVW R4, TMP1
	B upd_ll

upd_ll:
	// ll: low = bits >> ((ofNb+mlNb)&31) then & mask(llNb)
	MOVW OFHDR, R5
	AND $0xff, R5
	MOVW MLHDR, R6
	AND $0xff, R6
	ADD R6, R5
	AND $31, R5
	MOVW R4, R12
	SRL R5, R12
	MOVW LLHDR, R5
	AND $0xff, R5
	MOVW $1, R11
	SLL R5, R11
	SUB $1, R11
	AND R11, R12
	MOVW LLHDR, R6
	SRL $16, R6
	ADD R12, R6
	AND $511, R6
	MOVW LLTAB, R11
	SLL $3, R6, R9
	ADD R11, R9
	MOVW (R9), R11
	MOVW 4(R9), R12
	MOVW R11, LLHDR
	MOVW R12, LLBASE

	// ml: low = (bits >> (ofNb&31)) & mask(mlNb)
	MOVW OFHDR, R5
	AND $0xff, R5
	AND $31, R5
	MOVW R4, R12
	SRL R5, R12
	MOVW MLHDR, R5
	AND $0xff, R5
	MOVW $1, R11
	SLL R5, R11
	SUB $1, R11
	AND R11, R12
	MOVW MLHDR, R6
	SRL $16, R6
	ADD R12, R6
	AND $511, R6
	MOVW MLTAB, R11
	SLL $3, R6, R9
	ADD R11, R9
	MOVW (R9), R11
	MOVW 4(R9), R12
	MOVW R11, MLHDR
	MOVW R12, MLBASE

	// of: low = bits & mask(ofNb)
	MOVW OFHDR, R5
	AND $0xff, R5
	MOVW $1, R11
	SLL R5, R11
	SUB $1, R11
	AND R11, R4
	MOVW OFHDR, R6
	SRL $16, R6
	ADD R4, R6
	AND $511, R6
	MOVW OFTAB, R11
	SLL $3, R6, R9
	ADD R11, R9
	MOVW (R9), R11
	MOVW 4(R9), R12
	MOVW R11, OFHDR
	MOVW R12, OFBASE
	B mainloop

	// ---- errors ----
err_overread:
	MOVW $6, R5
	B publish
err_space:
	MOVW $5, R5
	B publish
err_mismatch:
	MOVW MLV, R6
	MOVW $1, R5
	B publish_ml
err_mllen:
	MOVW MLV, R6
	MOVW $2, R5
	B publish_ml
err_moff:
	MOVW MOV, R6
	MOVW $3, R5
	B publish_mo
err_lit:
	MOVW LLV, R6
	MOVW $4, R5
	B publish_ll

publish_ml:
	MOVW CTXP, R9
	MOVW R6, 128(R9)
	B publish
publish_mo:
	MOVW CTXP, R9
	MOVW R6, 132(R9)
	B publish
publish_ll:
	MOVW CTXP, R9
	MOVW R6, 124(R9)
	B publish

publish:
	MOVW CTXP, R9
	MOVW R3, 4(R9)
	MOVW R0, 8(R9)
	MOVW R1, 12(R9)
	MOVW R2, 16(R9)
	MOVW PREV0, R11
	MOVW R11, 56(R9)
	MOVW PREV1, R11
	MOVW R11, 60(R9)
	MOVW PREV2, R11
	MOVW R11, 64(R9)
	MOVW LITBASE, R11
	SUB R11, R8, R11
	MOVW R11, 92(R9)
	MOVW OUTBASE, R11
	SUB R11, R7, R11
	MOVW R11, 120(R9)
	MOVW R5, ret+4(FP)
	RET

finish:
	MOVW CTXP, R9
	MOVW R3, 4(R9)
	MOVW R0, 8(R9)
	MOVW R1, 12(R9)
	MOVW R2, 16(R9)
	MOVW PREV0, R11
	MOVW R11, 56(R9)
	MOVW PREV1, R11
	MOVW R11, 60(R9)
	MOVW PREV2, R11
	MOVW R11, 64(R9)
	MOVW LITBASE, R11
	SUB R11, R8, R11
	MOVW R11, 92(R9)
	MOVW OUTBASE, R11
	SUB R11, R7, R11
	MOVW R11, 120(R9)
	MOVW $0, R0
	MOVW R0, ret+4(FP)
	RET

