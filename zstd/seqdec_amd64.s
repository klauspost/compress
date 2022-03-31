// Code generated by command: go run gen.go -out seqdec_amd64.s -stubs delme.go -pkg=zstd. DO NOT EDIT.

//go:build !appengine && !noasm && gc && !noasm
// +build !appengine,!noasm,gc,!noasm

// func sequenceDecs_decode_amd64(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int
// Requires: CMOV, SSE
TEXT ·sequenceDecs_decode_amd64(SB), $8-32
	MOVQ    br+8(FP), AX
	MOVQ    32(AX), DX
	MOVBQZX 40(AX), BX
	MOVQ    24(AX), SI
	MOVQ    (AX), AX
	ADDQ    SI, AX
	MOVQ    AX, (SP)
	MOVQ    ctx+16(FP), AX
	MOVQ    72(AX), DI
	MOVQ    80(AX), R8
	MOVQ    88(AX), R9
	MOVQ    104(AX), R10

sequenceDecs_decode_amd64_main_loop:
	MOVQ (SP), R11

	// Fill bitreader to have enough for the offset.
	CMPQ    BX, $0x20
	JL      sequenceDecs_decode_amd64_fill_end
	CMPQ    SI, $0x04
	JL      sequenceDecs_decode_amd64_fill_byte_by_byte
	SHLQ    $0x20, DX
	SUBQ    $0x04, R11
	SUBQ    $0x04, SI
	SUBQ    $0x20, BX
	MOVLQZX (R11), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decode_amd64_fill_end

sequenceDecs_decode_amd64_fill_byte_by_byte:
	CMPQ    SI, $0x00
	JLE     sequenceDecs_decode_amd64_fill_end
	SHLQ    $0x08, DX
	SUBQ    $0x01, R11
	SUBQ    $0x01, SI
	SUBQ    $0x08, BX
	MOVBQZX (R11), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decode_amd64_fill_byte_by_byte

sequenceDecs_decode_amd64_fill_end:
	// Update offset
	MOVQ    R9, AX
	MOVQ    BX, CX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVB    AH, CL
	ADDQ    CX, BX
	NEGL    CX
	SHRQ    CL, R12
	SHRQ    $0x20, AX
	TESTQ   CX, CX
	CMOVQEQ CX, R12
	ADDQ    R12, AX
	MOVQ    AX, 16(R10)

	// Fill bitreader for match and literal
	CMPQ    BX, $0x20
	JL      sequenceDecs_decode_amd64_fill_2_end
	CMPQ    SI, $0x04
	JL      sequenceDecs_decode_amd64_fill_2_byte_by_byte
	SHLQ    $0x20, DX
	SUBQ    $0x04, R11
	SUBQ    $0x04, SI
	SUBQ    $0x20, BX
	MOVLQZX (R11), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decode_amd64_fill_2_end

sequenceDecs_decode_amd64_fill_2_byte_by_byte:
	CMPQ    SI, $0x00
	JLE     sequenceDecs_decode_amd64_fill_2_end
	SHLQ    $0x08, DX
	SUBQ    $0x01, R11
	SUBQ    $0x01, SI
	SUBQ    $0x08, BX
	MOVBQZX (R11), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decode_amd64_fill_2_byte_by_byte

sequenceDecs_decode_amd64_fill_2_end:
	// Update match length
	MOVQ    R8, AX
	MOVQ    BX, CX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVB    AH, CL
	ADDQ    CX, BX
	NEGL    CX
	SHRQ    CL, R12
	SHRQ    $0x20, AX
	TESTQ   CX, CX
	CMOVQEQ CX, R12
	ADDQ    R12, AX
	MOVQ    AX, 8(R10)

	// Update literal length
	MOVQ    DI, AX
	MOVQ    BX, CX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVB    AH, CL
	ADDQ    CX, BX
	NEGL    CX
	SHRQ    CL, R12
	SHRQ    $0x20, AX
	TESTQ   CX, CX
	CMOVQEQ CX, R12
	ADDQ    R12, AX
	MOVQ    AX, (R10)

	// Fill bitreader for state updates
	CMPQ    BX, $0x20
	JL      sequenceDecs_decode_amd64_fill_3_end
	CMPQ    SI, $0x04
	JL      sequenceDecs_decode_amd64_fill_3_byte_by_byte
	SHLQ    $0x20, DX
	SUBQ    $0x04, R11
	SUBQ    $0x04, SI
	SUBQ    $0x20, BX
	MOVLQZX (R11), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decode_amd64_fill_3_end

sequenceDecs_decode_amd64_fill_3_byte_by_byte:
	CMPQ    SI, $0x00
	JLE     sequenceDecs_decode_amd64_fill_3_end
	SHLQ    $0x08, DX
	SUBQ    $0x01, R11
	SUBQ    $0x01, SI
	SUBQ    $0x08, BX
	MOVBQZX (R11), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decode_amd64_fill_3_byte_by_byte

sequenceDecs_decode_amd64_fill_3_end:
	MOVQ    R11, (SP)
	MOVQ    R9, AX
	SHRQ    $0x08, AX
	MOVBQZX AL, AX
	MOVQ    ctx+16(FP), CX
	CMPQ    96(CX), $0x00
	JZ      sequenceDecs_decode_amd64_skip_update

	// Update Literal Length State
	MOVBQZX DI, R11
	SHRQ    $0x10, DI
	MOVWQZX DI, DI
	CMPQ    R11, $0x00
	JZ      sequenceDecs_decode_amd64_llState_updateState_skip_zero
	MOVQ    BX, CX
	ADDQ    R11, BX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVQ    R11, CX
	NEGQ    CX
	SHRQ    CL, R12
	ADDQ    R12, DI

sequenceDecs_decode_amd64_llState_updateState_skip_zero:
	// Load ctx.llTable
	MOVQ ctx+16(FP), CX
	MOVQ (CX), CX
	MOVQ (CX)(DI*8), DI

	// Update Match Length State
	MOVBQZX R8, R11
	SHRQ    $0x10, R8
	MOVWQZX R8, R8
	CMPQ    R11, $0x00
	JZ      sequenceDecs_decode_amd64_mlState_updateState_skip_zero
	MOVQ    BX, CX
	ADDQ    R11, BX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVQ    R11, CX
	NEGQ    CX
	SHRQ    CL, R12
	ADDQ    R12, R8

sequenceDecs_decode_amd64_mlState_updateState_skip_zero:
	// Load ctx.mlTable
	MOVQ ctx+16(FP), CX
	MOVQ 24(CX), CX
	MOVQ (CX)(R8*8), R8

	// Update Offset State
	MOVBQZX R9, R11
	SHRQ    $0x10, R9
	MOVWQZX R9, R9
	CMPQ    R11, $0x00
	JZ      sequenceDecs_decode_amd64_ofState_updateState_skip_zero
	MOVQ    BX, CX
	ADDQ    R11, BX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVQ    R11, CX
	NEGQ    CX
	SHRQ    CL, R12
	ADDQ    R12, R9

sequenceDecs_decode_amd64_ofState_updateState_skip_zero:
	// Load ctx.ofTable
	MOVQ ctx+16(FP), CX
	MOVQ 48(CX), CX
	MOVQ (CX)(R9*8), R9

sequenceDecs_decode_amd64_skip_update:
	// Adjust offset
	MOVQ   s+0(FP), CX
	MOVQ   16(R10), R11
	CMPQ   AX, $0x01
	JBE    sequenceDecs_decode_amd64_adjust_offsetB_1_or_0
	MOVUPS 144(CX), X0
	MOVQ   R11, 144(CX)
	MOVUPS X0, 152(CX)
	JMP    sequenceDecs_decode_amd64_adjust_end

sequenceDecs_decode_amd64_adjust_offsetB_1_or_0:
	CMPQ (R10), $0x00000000
	JNE  sequenceDecs_decode_amd64_adjust_offset_maybezero
	INCQ R11
	JMP  sequenceDecs_decode_amd64_adjust_offset_nonzero

sequenceDecs_decode_amd64_adjust_offset_maybezero:
	TESTQ R11, R11
	JNZ   sequenceDecs_decode_amd64_adjust_offset_nonzero
	MOVQ  144(CX), R11
	JMP   sequenceDecs_decode_amd64_adjust_end

sequenceDecs_decode_amd64_adjust_offset_nonzero:
	MOVQ    R11, AX
	XORQ    R12, R12
	MOVQ    $-1, R13
	CMPQ    R11, $0x03
	CMOVQEQ R12, AX
	CMOVQEQ R13, R12
	LEAQ    144(CX), R13
	ADDQ    (R13)(AX*8), R12
	JNZ     sequenceDecs_decode_amd64_adjust_temp_valid
	MOVQ    $0x00000001, R12

sequenceDecs_decode_amd64_adjust_temp_valid:
	CMPQ R11, $0x01
	JZ   sequenceDecs_decode_amd64_adjust_skip
	MOVQ 152(CX), AX
	MOVQ AX, 160(CX)

sequenceDecs_decode_amd64_adjust_skip:
	MOVQ 144(CX), AX
	MOVQ AX, 152(CX)
	MOVQ R12, 144(CX)
	MOVQ R12, R11

sequenceDecs_decode_amd64_adjust_end:
	MOVQ R11, 16(R10)

	// Check values
	MOVQ  8(R10), AX
	MOVQ  (R10), CX
	LEAQ  (AX)(CX*1), R12
	MOVQ  s+0(FP), R13
	ADDQ  R12, 256(R13)
	MOVQ  ctx+16(FP), R12
	SUBQ  CX, 128(R12)
	CMPQ  AX, $0x00020002
	JA    sequenceDecs_decode_amd64_error_match_len_too_big
	TESTQ R11, R11
	JNZ   sequenceDecs_decode_amd64_match_len_ofs_ok
	TESTQ AX, AX
	JNZ   sequenceDecs_decode_amd64_error_match_len_ofs_mismatch

sequenceDecs_decode_amd64_match_len_ofs_ok:
	ADDQ $0x18, R10
	MOVQ ctx+16(FP), AX
	DECQ 96(AX)
	JNS  sequenceDecs_decode_amd64_main_loop
	MOVQ br+8(FP), AX
	MOVQ DX, 32(AX)
	MOVB BL, 40(AX)
	MOVQ SI, 24(AX)

	// Return success
	MOVQ $0x00000000, ret+24(FP)
	RET

	// Return with match length error
sequenceDecs_decode_amd64_error_match_len_ofs_mismatch:
	MOVQ $0x00000001, ret+24(FP)
	RET

	// Return with match length too long error
sequenceDecs_decode_amd64_error_match_len_too_big:
	MOVQ $0x00000002, ret+24(FP)
	RET

	// Return with match offset too long error
	MOVQ $0x00000003, ret+24(FP)
	RET

// func sequenceDecs_decode_bmi2(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int
// Requires: BMI, BMI2, CMOV, SSE
TEXT ·sequenceDecs_decode_bmi2(SB), $8-32
	MOVQ    br+8(FP), CX
	MOVQ    32(CX), AX
	MOVBQZX 40(CX), DX
	MOVQ    24(CX), BX
	MOVQ    (CX), CX
	ADDQ    BX, CX
	MOVQ    CX, (SP)
	MOVQ    ctx+16(FP), CX
	MOVQ    72(CX), SI
	MOVQ    80(CX), DI
	MOVQ    88(CX), R8
	MOVQ    104(CX), R9

sequenceDecs_decode_bmi2_main_loop:
	MOVQ (SP), R10

	// Fill bitreader to have enough for the offset.
	CMPQ    DX, $0x20
	JL      sequenceDecs_decode_bmi2_fill_end
	CMPQ    BX, $0x04
	JL      sequenceDecs_decode_bmi2_fill_byte_by_byte
	SHLQ    $0x20, AX
	SUBQ    $0x04, R10
	SUBQ    $0x04, BX
	SUBQ    $0x20, DX
	MOVLQZX (R10), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decode_bmi2_fill_end

sequenceDecs_decode_bmi2_fill_byte_by_byte:
	CMPQ    BX, $0x00
	JLE     sequenceDecs_decode_bmi2_fill_end
	SHLQ    $0x08, AX
	SUBQ    $0x01, R10
	SUBQ    $0x01, BX
	SUBQ    $0x08, DX
	MOVBQZX (R10), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decode_bmi2_fill_byte_by_byte

sequenceDecs_decode_bmi2_fill_end:
	// Update offset
	MOVQ   $0x00000808, CX
	BEXTRQ CX, R8, R11
	MOVQ   AX, R12
	LEAQ   (DX)(R11*1), CX
	ROLQ   CL, R12
	BZHIQ  R11, R12, R12
	MOVQ   CX, DX
	MOVQ   R8, CX
	SHRQ   $0x20, CX
	ADDQ   R12, CX
	MOVQ   CX, 16(R9)

	// Fill bitreader for match and literal
	CMPQ    DX, $0x20
	JL      sequenceDecs_decode_bmi2_fill_2_end
	CMPQ    BX, $0x04
	JL      sequenceDecs_decode_bmi2_fill_2_byte_by_byte
	SHLQ    $0x20, AX
	SUBQ    $0x04, R10
	SUBQ    $0x04, BX
	SUBQ    $0x20, DX
	MOVLQZX (R10), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decode_bmi2_fill_2_end

sequenceDecs_decode_bmi2_fill_2_byte_by_byte:
	CMPQ    BX, $0x00
	JLE     sequenceDecs_decode_bmi2_fill_2_end
	SHLQ    $0x08, AX
	SUBQ    $0x01, R10
	SUBQ    $0x01, BX
	SUBQ    $0x08, DX
	MOVBQZX (R10), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decode_bmi2_fill_2_byte_by_byte

sequenceDecs_decode_bmi2_fill_2_end:
	// Update match length
	MOVQ   $0x00000808, CX
	BEXTRQ CX, DI, R11
	MOVQ   AX, R12
	LEAQ   (DX)(R11*1), CX
	ROLQ   CL, R12
	BZHIQ  R11, R12, R12
	MOVQ   CX, DX
	MOVQ   DI, CX
	SHRQ   $0x20, CX
	ADDQ   R12, CX
	MOVQ   CX, 8(R9)

	// Update literal length
	MOVQ   $0x00000808, CX
	BEXTRQ CX, SI, R11
	MOVQ   AX, R12
	LEAQ   (DX)(R11*1), CX
	ROLQ   CL, R12
	BZHIQ  R11, R12, R12
	MOVQ   CX, DX
	MOVQ   SI, CX
	SHRQ   $0x20, CX
	ADDQ   R12, CX
	MOVQ   CX, (R9)

	// Fill bitreader for state updates
	CMPQ    DX, $0x20
	JL      sequenceDecs_decode_bmi2_fill_3_end
	CMPQ    BX, $0x04
	JL      sequenceDecs_decode_bmi2_fill_3_byte_by_byte
	SHLQ    $0x20, AX
	SUBQ    $0x04, R10
	SUBQ    $0x04, BX
	SUBQ    $0x20, DX
	MOVLQZX (R10), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decode_bmi2_fill_3_end

sequenceDecs_decode_bmi2_fill_3_byte_by_byte:
	CMPQ    BX, $0x00
	JLE     sequenceDecs_decode_bmi2_fill_3_end
	SHLQ    $0x08, AX
	SUBQ    $0x01, R10
	SUBQ    $0x01, BX
	SUBQ    $0x08, DX
	MOVBQZX (R10), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decode_bmi2_fill_3_byte_by_byte

sequenceDecs_decode_bmi2_fill_3_end:
	MOVQ   R10, (SP)
	MOVQ   $0x00000808, CX
	BEXTRQ CX, R8, R10
	MOVQ   ctx+16(FP), CX
	CMPQ   96(CX), $0x00
	JZ     sequenceDecs_decode_bmi2_skip_update

	// Update Literal Length State
	MOVBQZX SI, R11
	MOVQ    $0x00001010, CX
	BEXTRQ  CX, SI, SI
	LEAQ    (DX)(R11*1), CX
	MOVQ    AX, R12
	MOVQ    CX, DX
	ROLQ    CL, R12
	BZHIQ   R11, R12, R12
	ADDQ    R12, SI

	// Load ctx.llTable
	MOVQ ctx+16(FP), CX
	MOVQ (CX), CX
	MOVQ (CX)(SI*8), SI

	// Update Match Length State
	MOVBQZX DI, R11
	MOVQ    $0x00001010, CX
	BEXTRQ  CX, DI, DI
	LEAQ    (DX)(R11*1), CX
	MOVQ    AX, R12
	MOVQ    CX, DX
	ROLQ    CL, R12
	BZHIQ   R11, R12, R12
	ADDQ    R12, DI

	// Load ctx.mlTable
	MOVQ ctx+16(FP), CX
	MOVQ 24(CX), CX
	MOVQ (CX)(DI*8), DI

	// Update Offset State
	MOVBQZX R8, R11
	MOVQ    $0x00001010, CX
	BEXTRQ  CX, R8, R8
	LEAQ    (DX)(R11*1), CX
	MOVQ    AX, R12
	MOVQ    CX, DX
	ROLQ    CL, R12
	BZHIQ   R11, R12, R12
	ADDQ    R12, R8

	// Load ctx.ofTable
	MOVQ ctx+16(FP), CX
	MOVQ 48(CX), CX
	MOVQ (CX)(R8*8), R8

sequenceDecs_decode_bmi2_skip_update:
	// Adjust offset
	MOVQ   s+0(FP), CX
	MOVQ   16(R9), R11
	CMPQ   R10, $0x01
	JBE    sequenceDecs_decode_bmi2_adjust_offsetB_1_or_0
	MOVUPS 144(CX), X0
	MOVQ   R11, 144(CX)
	MOVUPS X0, 152(CX)
	JMP    sequenceDecs_decode_bmi2_adjust_end

sequenceDecs_decode_bmi2_adjust_offsetB_1_or_0:
	CMPQ (R9), $0x00000000
	JNE  sequenceDecs_decode_bmi2_adjust_offset_maybezero
	INCQ R11
	JMP  sequenceDecs_decode_bmi2_adjust_offset_nonzero

sequenceDecs_decode_bmi2_adjust_offset_maybezero:
	TESTQ R11, R11
	JNZ   sequenceDecs_decode_bmi2_adjust_offset_nonzero
	MOVQ  144(CX), R11
	JMP   sequenceDecs_decode_bmi2_adjust_end

sequenceDecs_decode_bmi2_adjust_offset_nonzero:
	MOVQ    R11, R10
	XORQ    R12, R12
	MOVQ    $-1, R13
	CMPQ    R11, $0x03
	CMOVQEQ R12, R10
	CMOVQEQ R13, R12
	LEAQ    144(CX), R13
	ADDQ    (R13)(R10*8), R12
	JNZ     sequenceDecs_decode_bmi2_adjust_temp_valid
	MOVQ    $0x00000001, R12

sequenceDecs_decode_bmi2_adjust_temp_valid:
	CMPQ R11, $0x01
	JZ   sequenceDecs_decode_bmi2_adjust_skip
	MOVQ 152(CX), R10
	MOVQ R10, 160(CX)

sequenceDecs_decode_bmi2_adjust_skip:
	MOVQ 144(CX), R10
	MOVQ R10, 152(CX)
	MOVQ R12, 144(CX)
	MOVQ R12, R11

sequenceDecs_decode_bmi2_adjust_end:
	MOVQ R11, 16(R9)

	// Check values
	MOVQ  8(R9), CX
	MOVQ  (R9), R10
	LEAQ  (CX)(R10*1), R12
	MOVQ  s+0(FP), R13
	ADDQ  R12, 256(R13)
	MOVQ  ctx+16(FP), R12
	SUBQ  R10, 128(R12)
	CMPQ  CX, $0x00020002
	JA    sequenceDecs_decode_bmi2_error_match_len_too_big
	TESTQ R11, R11
	JNZ   sequenceDecs_decode_bmi2_match_len_ofs_ok
	TESTQ CX, CX
	JNZ   sequenceDecs_decode_bmi2_error_match_len_ofs_mismatch

sequenceDecs_decode_bmi2_match_len_ofs_ok:
	ADDQ $0x18, R9
	MOVQ ctx+16(FP), CX
	DECQ 96(CX)
	JNS  sequenceDecs_decode_bmi2_main_loop
	MOVQ br+8(FP), CX
	MOVQ AX, 32(CX)
	MOVB DL, 40(CX)
	MOVQ BX, 24(CX)

	// Return success
	MOVQ $0x00000000, ret+24(FP)
	RET

	// Return with match length error
sequenceDecs_decode_bmi2_error_match_len_ofs_mismatch:
	MOVQ $0x00000001, ret+24(FP)
	RET

	// Return with match length too long error
sequenceDecs_decode_bmi2_error_match_len_too_big:
	MOVQ $0x00000002, ret+24(FP)
	RET

	// Return with match offset too long error
	MOVQ $0x00000003, ret+24(FP)
	RET

// func sequenceDecs_executeSimple_amd64(ctx *executeAsmContext) bool
// Requires: SSE
TEXT ·sequenceDecs_executeSimple_amd64(SB), $0-9
	MOVQ  ctx+0(FP), DI
	MOVQ  8(DI), CX
	TESTQ CX, CX
	JZ    empty_seqs
	MOVQ  (DI), AX
	MOVQ  24(DI), DX
	MOVQ  32(DI), BX
	MOVQ  40(DI), SI
	MOVQ  56(DI), SI
	MOVQ  80(DI), R8
	MOVQ  96(DI), DI

	// seqsBase += 24 * seqIndex
	LEAQ (DX)(DX*2), DI
	SHLQ $0x03, DI
	ADDQ DI, AX

	// outBase += outPosition
	ADDQ R8, BX

main_loop:
	// Copy literals
	MOVQ  (AX), DI
	TESTQ DI, DI
	JZ    copy_match
	XORQ  R9, R9

copy_1:
	MOVUPS (SI)(R9*1), X0
	MOVUPS X0, (BX)(R9*1)
	ADDQ   $0x10, R9
	CMPQ   R9, DI
	JB     copy_1
	ADDQ   DI, SI
	ADDQ   DI, BX
	ADDQ   DI, R8

	// Copy match
copy_match:
	MOVQ  8(AX), DI
	TESTQ DI, DI
	JZ    handle_loop
	MOVQ  16(AX), R9

	// Malformed input if seq.mo > t || seq.mo > s.windowSize
	CMPQ R9, R8
	JG   error_match_off_too_big
	MOVQ BX, R10
	SUBQ R9, R10

	// ml <= mo
	CMPQ DI, R9
	JA   copy_overlapping_match

	// Copy non-overlapping match
	XORQ R9, R9

copy_2:
	MOVUPS (R10)(R9*1), X0
	MOVUPS X0, (BX)(R9*1)
	ADDQ   $0x10, R9
	CMPQ   R9, DI
	JB     copy_2
	ADDQ   DI, BX
	ADDQ   DI, R8
	JMP    handle_loop

	// Copy overlapping match
copy_overlapping_match:
	XORQ R9, R9

copy_slow_3:
	MOVB (R10)(R9*1), R11
	MOVB R11, (BX)(R9*1)
	INCQ R9
	CMPQ R9, DI
	JB   copy_slow_3
	ADDQ DI, BX
	ADDQ DI, R8

handle_loop:
	ADDQ $0x18, AX
	INCQ DX
	CMPQ DX, CX
	JB   main_loop

	// Return value
	MOVB $0x01, ret+8(FP)

	// Update the context
	MOVQ ctx+0(FP), AX
	MOVQ DX, 24(AX)
	MOVQ R8, 80(AX)
	MOVQ 56(AX), CX
	SUBQ CX, SI
	MOVQ SI, 88(AX)
	RET

error_match_off_too_big:
	// Return value
	MOVB $0x00, ret+8(FP)

	// Update the context
	MOVQ ctx+0(FP), AX
	MOVQ DX, 24(AX)
	MOVQ R8, 80(AX)
	MOVQ 56(AX), CX
	SUBQ CX, SI
	MOVQ SI, 88(AX)
	RET

empty_seqs:
	// Return value
	MOVB $0x01, ret+8(FP)
	RET

// func sequenceDecs_decodeSync_amd64(s *sequenceDecs, br *bitReader, ctx *decodeSyncAsmContext) int
// Requires: CMOV, SSE
TEXT ·sequenceDecs_decodeSync_amd64(SB), $32-32
	MOVQ    br+8(FP), AX
	MOVQ    32(AX), DX
	MOVBQZX 40(AX), BX
	MOVQ    24(AX), SI
	MOVQ    (AX), AX
	ADDQ    SI, AX
	MOVQ    AX, (SP)
	MOVQ    ctx+16(FP), AX
	MOVQ    72(AX), DI
	MOVQ    80(AX), R8
	MOVQ    88(AX), R9
	MOVQ    112(AX), R11
	MOVQ    120(AX), CX
	MOVQ    136(AX), R12
	MOVQ    160(AX), R13
	MOVQ    168(AX), AX

	// outBase += outPosition
	ADDQ R13, R11

sequenceDecs_decodeSync_amd64_main_loop:
	MOVQ (SP), R14

	// Fill bitreader to have enough for the offset.
	CMPQ    BX, $0x20
	JL      sequenceDecs_decodeSync_amd64_fill_end
	CMPQ    SI, $0x04
	JL      sequenceDecs_decodeSync_amd64_fill_byte_by_byte
	SHLQ    $0x20, DX
	SUBQ    $0x04, R14
	SUBQ    $0x04, SI
	SUBQ    $0x20, BX
	MOVLQZX (R14), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decodeSync_amd64_fill_end

sequenceDecs_decodeSync_amd64_fill_byte_by_byte:
	CMPQ    SI, $0x00
	JLE     sequenceDecs_decodeSync_amd64_fill_end
	SHLQ    $0x08, DX
	SUBQ    $0x01, R14
	SUBQ    $0x01, SI
	SUBQ    $0x08, BX
	MOVBQZX (R14), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decodeSync_amd64_fill_byte_by_byte

sequenceDecs_decodeSync_amd64_fill_end:
	// Update offset
	MOVQ    R9, AX
	MOVQ    BX, CX
	MOVQ    DX, R15
	SHLQ    CL, R15
	MOVB    AH, CL
	ADDQ    CX, BX
	NEGL    CX
	SHRQ    CL, R15
	SHRQ    $0x20, AX
	TESTQ   CX, CX
	CMOVQEQ CX, R15
	ADDQ    R15, AX
	MOVQ    AX, 8(SP)

	// Fill bitreader for match and literal
	CMPQ    BX, $0x20
	JL      sequenceDecs_decodeSync_amd64_fill_2_end
	CMPQ    SI, $0x04
	JL      sequenceDecs_decodeSync_amd64_fill_2_byte_by_byte
	SHLQ    $0x20, DX
	SUBQ    $0x04, R14
	SUBQ    $0x04, SI
	SUBQ    $0x20, BX
	MOVLQZX (R14), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decodeSync_amd64_fill_2_end

sequenceDecs_decodeSync_amd64_fill_2_byte_by_byte:
	CMPQ    SI, $0x00
	JLE     sequenceDecs_decodeSync_amd64_fill_2_end
	SHLQ    $0x08, DX
	SUBQ    $0x01, R14
	SUBQ    $0x01, SI
	SUBQ    $0x08, BX
	MOVBQZX (R14), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decodeSync_amd64_fill_2_byte_by_byte

sequenceDecs_decodeSync_amd64_fill_2_end:
	// Update match length
	MOVQ    R8, AX
	MOVQ    BX, CX
	MOVQ    DX, R15
	SHLQ    CL, R15
	MOVB    AH, CL
	ADDQ    CX, BX
	NEGL    CX
	SHRQ    CL, R15
	SHRQ    $0x20, AX
	TESTQ   CX, CX
	CMOVQEQ CX, R15
	ADDQ    R15, AX
	MOVQ    AX, 16(SP)

	// Update literal length
	MOVQ    DI, AX
	MOVQ    BX, CX
	MOVQ    DX, R15
	SHLQ    CL, R15
	MOVB    AH, CL
	ADDQ    CX, BX
	NEGL    CX
	SHRQ    CL, R15
	SHRQ    $0x20, AX
	TESTQ   CX, CX
	CMOVQEQ CX, R15
	ADDQ    R15, AX
	MOVQ    AX, 24(SP)

	// Fill bitreader for state updates
	CMPQ    BX, $0x20
	JL      sequenceDecs_decodeSync_amd64_fill_3_end
	CMPQ    SI, $0x04
	JL      sequenceDecs_decodeSync_amd64_fill_3_byte_by_byte
	SHLQ    $0x20, DX
	SUBQ    $0x04, R14
	SUBQ    $0x04, SI
	SUBQ    $0x20, BX
	MOVLQZX (R14), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decodeSync_amd64_fill_3_end

sequenceDecs_decodeSync_amd64_fill_3_byte_by_byte:
	CMPQ    SI, $0x00
	JLE     sequenceDecs_decodeSync_amd64_fill_3_end
	SHLQ    $0x08, DX
	SUBQ    $0x01, R14
	SUBQ    $0x01, SI
	SUBQ    $0x08, BX
	MOVBQZX (R14), AX
	ORQ     AX, DX
	JMP     sequenceDecs_decodeSync_amd64_fill_3_byte_by_byte

sequenceDecs_decodeSync_amd64_fill_3_end:
	MOVQ    R14, (SP)
	MOVQ    R9, AX
	SHRQ    $0x08, AX
	MOVBQZX AL, AX
	MOVQ    ctx+16(FP), CX
	CMPQ    96(CX), $0x00
	JZ      sequenceDecs_decodeSync_amd64_skip_update

	// Update Literal Length State
	MOVBQZX DI, R14
	SHRQ    $0x10, DI
	MOVWQZX DI, DI
	CMPQ    R14, $0x00
	JZ      sequenceDecs_decodeSync_amd64_llState_updateState_skip_zero
	MOVQ    BX, CX
	ADDQ    R14, BX
	MOVQ    DX, R15
	SHLQ    CL, R15
	MOVQ    R14, CX
	NEGQ    CX
	SHRQ    CL, R15
	ADDQ    R15, DI

sequenceDecs_decodeSync_amd64_llState_updateState_skip_zero:
	// Load ctx.llTable
	MOVQ ctx+16(FP), CX
	MOVQ (CX), CX
	MOVQ (CX)(DI*8), DI

	// Update Match Length State
	MOVBQZX R8, R14
	SHRQ    $0x10, R8
	MOVWQZX R8, R8
	CMPQ    R14, $0x00
	JZ      sequenceDecs_decodeSync_amd64_mlState_updateState_skip_zero
	MOVQ    BX, CX
	ADDQ    R14, BX
	MOVQ    DX, R15
	SHLQ    CL, R15
	MOVQ    R14, CX
	NEGQ    CX
	SHRQ    CL, R15
	ADDQ    R15, R8

sequenceDecs_decodeSync_amd64_mlState_updateState_skip_zero:
	// Load ctx.mlTable
	MOVQ ctx+16(FP), CX
	MOVQ 24(CX), CX
	MOVQ (CX)(R8*8), R8

	// Update Offset State
	MOVBQZX R9, R14
	SHRQ    $0x10, R9
	MOVWQZX R9, R9
	CMPQ    R14, $0x00
	JZ      sequenceDecs_decodeSync_amd64_ofState_updateState_skip_zero
	MOVQ    BX, CX
	ADDQ    R14, BX
	MOVQ    DX, R15
	SHLQ    CL, R15
	MOVQ    R14, CX
	NEGQ    CX
	SHRQ    CL, R15
	ADDQ    R15, R9

sequenceDecs_decodeSync_amd64_ofState_updateState_skip_zero:
	// Load ctx.ofTable
	MOVQ ctx+16(FP), CX
	MOVQ 48(CX), CX
	MOVQ (CX)(R9*8), R9

sequenceDecs_decodeSync_amd64_skip_update:
	// Adjust offset
	MOVQ   s+0(FP), CX
	MOVQ   8(SP), R14
	CMPQ   AX, $0x01
	JBE    sequenceDecs_decodeSync_amd64_adjust_offsetB_1_or_0
	MOVUPS 144(CX), X0
	MOVQ   R14, 144(CX)
	MOVUPS X0, 152(CX)
	JMP    sequenceDecs_decodeSync_amd64_adjust_end

sequenceDecs_decodeSync_amd64_adjust_offsetB_1_or_0:
	CMPQ 24(SP), $0x00000000
	JNE  sequenceDecs_decodeSync_amd64_adjust_offset_maybezero
	INCQ R14
	JMP  sequenceDecs_decodeSync_amd64_adjust_offset_nonzero

sequenceDecs_decodeSync_amd64_adjust_offset_maybezero:
	TESTQ R14, R14
	JNZ   sequenceDecs_decodeSync_amd64_adjust_offset_nonzero
	MOVQ  144(CX), R14
	JMP   sequenceDecs_decodeSync_amd64_adjust_end

sequenceDecs_decodeSync_amd64_adjust_offset_nonzero:
	MOVQ    R14, AX
	XORQ    R15, R15
	MOVQ    $-1, BP
	CMPQ    R14, $0x03
	CMOVQEQ R15, AX
	CMOVQEQ BP, R15
	LEAQ    144(CX), BP
	ADDQ    (BP)(AX*8), R15
	JNZ     sequenceDecs_decodeSync_amd64_adjust_temp_valid
	MOVQ    $0x00000001, R15

sequenceDecs_decodeSync_amd64_adjust_temp_valid:
	CMPQ R14, $0x01
	JZ   sequenceDecs_decodeSync_amd64_adjust_skip
	MOVQ 152(CX), AX
	MOVQ AX, 160(CX)

sequenceDecs_decodeSync_amd64_adjust_skip:
	MOVQ 144(CX), AX
	MOVQ AX, 152(CX)
	MOVQ R15, 144(CX)
	MOVQ R15, R14

sequenceDecs_decodeSync_amd64_adjust_end:
	MOVQ R14, 8(SP)

	// Check values
	MOVQ  16(SP), AX
	MOVQ  24(SP), CX
	LEAQ  (AX)(CX*1), R15
	MOVQ  s+0(FP), BP
	ADDQ  R15, 256(BP)
	MOVQ  ctx+16(FP), R15
	SUBQ  CX, 104(R15)
	CMPQ  AX, $0x00020002
	JA    sequenceDecs_decodeSync_amd64_error_match_len_too_big
	TESTQ R14, R14
	JNZ   sequenceDecs_decodeSync_amd64_match_len_ofs_ok
	TESTQ AX, AX
	JNZ   sequenceDecs_decodeSync_amd64_error_match_len_ofs_mismatch

sequenceDecs_decodeSync_amd64_match_len_ofs_ok:
	// Copy literals
	MOVQ  24(SP), AX
	TESTQ AX, AX
	JZ    copy_match
	XORQ  CX, CX

copy_1:
	MOVUPS (R12)(CX*1), X0
	MOVUPS X0, (R11)(CX*1)
	ADDQ   $0x10, CX
	CMPQ   CX, AX
	JB     copy_1
	ADDQ   AX, R12
	ADDQ   AX, R11
	ADDQ   AX, R13

	// Copy match
copy_match:
	MOVQ  16(SP), AX
	TESTQ AX, AX
	JZ    handle_loop
	MOVQ  8(SP), CX

	// Malformed input if seq.mo > t || seq.mo > s.windowSize
	CMPQ CX, R13
	JG   error_match_off_too_big
	MOVQ R11, R14
	SUBQ CX, R14

	// ml <= mo
	CMPQ AX, CX
	JA   copy_overlapping_match

	// Copy non-overlapping match
	XORQ CX, CX

copy_2:
	MOVUPS (R14)(CX*1), X0
	MOVUPS X0, (R11)(CX*1)
	ADDQ   $0x10, CX
	CMPQ   CX, AX
	JB     copy_2
	ADDQ   AX, R11
	ADDQ   AX, R13
	JMP    handle_loop

	// Copy overlapping match
copy_overlapping_match:
	XORQ CX, CX

copy_slow_3:
	MOVB (R14)(CX*1), R15
	MOVB R15, (R11)(CX*1)
	INCQ CX
	CMPQ CX, AX
	JB   copy_slow_3
	ADDQ AX, R11
	ADDQ AX, R13

handle_loop:
	ADDQ $0x18, R10
	MOVQ ctx+16(FP), AX
	DECQ 96(AX)
	JNS  sequenceDecs_decodeSync_amd64_main_loop
	MOVQ br+8(FP), AX
	MOVQ DX, 32(AX)
	MOVB BL, 40(AX)
	MOVQ SI, 24(AX)

	// Return success
	MOVQ $0x00000000, ret+24(FP)
	RET

	// Return with match length error
sequenceDecs_decodeSync_amd64_error_match_len_ofs_mismatch:
	MOVQ $0x00000001, ret+24(FP)
	RET

	// Return with match length too long error
sequenceDecs_decodeSync_amd64_error_match_len_too_big:
	MOVQ $0x00000002, ret+24(FP)
	RET

	// Return with match offset too long error
error_match_off_too_big:
	MOVQ $0x00000003, ret+24(FP)
	RET
	RET

// func sequenceDecs_decodeSync_bmi2(s *sequenceDecs, br *bitReader, ctx *decodeSyncAsmContext) int
// Requires: BMI, BMI2, CMOV, SSE
TEXT ·sequenceDecs_decodeSync_bmi2(SB), $32-32
	MOVQ    br+8(FP), CX
	MOVQ    32(CX), AX
	MOVBQZX 40(CX), DX
	MOVQ    24(CX), BX
	MOVQ    (CX), CX
	ADDQ    BX, CX
	MOVQ    CX, (SP)
	MOVQ    ctx+16(FP), CX
	MOVQ    72(CX), SI
	MOVQ    80(CX), DI
	MOVQ    88(CX), R8
	MOVQ    112(CX), R10
	MOVQ    120(CX), R11
	MOVQ    136(CX), R11
	MOVQ    160(CX), R12
	MOVQ    168(CX), CX

	// outBase += outPosition
	ADDQ R12, R10

sequenceDecs_decodeSync_bmi2_main_loop:
	MOVQ (SP), R13

	// Fill bitreader to have enough for the offset.
	CMPQ    DX, $0x20
	JL      sequenceDecs_decodeSync_bmi2_fill_end
	CMPQ    BX, $0x04
	JL      sequenceDecs_decodeSync_bmi2_fill_byte_by_byte
	SHLQ    $0x20, AX
	SUBQ    $0x04, R13
	SUBQ    $0x04, BX
	SUBQ    $0x20, DX
	MOVLQZX (R13), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decodeSync_bmi2_fill_end

sequenceDecs_decodeSync_bmi2_fill_byte_by_byte:
	CMPQ    BX, $0x00
	JLE     sequenceDecs_decodeSync_bmi2_fill_end
	SHLQ    $0x08, AX
	SUBQ    $0x01, R13
	SUBQ    $0x01, BX
	SUBQ    $0x08, DX
	MOVBQZX (R13), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decodeSync_bmi2_fill_byte_by_byte

sequenceDecs_decodeSync_bmi2_fill_end:
	// Update offset
	MOVQ   $0x00000808, CX
	BEXTRQ CX, R8, R14
	MOVQ   AX, R15
	LEAQ   (DX)(R14*1), CX
	ROLQ   CL, R15
	BZHIQ  R14, R15, R15
	MOVQ   CX, DX
	MOVQ   R8, CX
	SHRQ   $0x20, CX
	ADDQ   R15, CX
	MOVQ   CX, 8(SP)

	// Fill bitreader for match and literal
	CMPQ    DX, $0x20
	JL      sequenceDecs_decodeSync_bmi2_fill_2_end
	CMPQ    BX, $0x04
	JL      sequenceDecs_decodeSync_bmi2_fill_2_byte_by_byte
	SHLQ    $0x20, AX
	SUBQ    $0x04, R13
	SUBQ    $0x04, BX
	SUBQ    $0x20, DX
	MOVLQZX (R13), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decodeSync_bmi2_fill_2_end

sequenceDecs_decodeSync_bmi2_fill_2_byte_by_byte:
	CMPQ    BX, $0x00
	JLE     sequenceDecs_decodeSync_bmi2_fill_2_end
	SHLQ    $0x08, AX
	SUBQ    $0x01, R13
	SUBQ    $0x01, BX
	SUBQ    $0x08, DX
	MOVBQZX (R13), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decodeSync_bmi2_fill_2_byte_by_byte

sequenceDecs_decodeSync_bmi2_fill_2_end:
	// Update match length
	MOVQ   $0x00000808, CX
	BEXTRQ CX, DI, R14
	MOVQ   AX, R15
	LEAQ   (DX)(R14*1), CX
	ROLQ   CL, R15
	BZHIQ  R14, R15, R15
	MOVQ   CX, DX
	MOVQ   DI, CX
	SHRQ   $0x20, CX
	ADDQ   R15, CX
	MOVQ   CX, 16(SP)

	// Update literal length
	MOVQ   $0x00000808, CX
	BEXTRQ CX, SI, R14
	MOVQ   AX, R15
	LEAQ   (DX)(R14*1), CX
	ROLQ   CL, R15
	BZHIQ  R14, R15, R15
	MOVQ   CX, DX
	MOVQ   SI, CX
	SHRQ   $0x20, CX
	ADDQ   R15, CX
	MOVQ   CX, 24(SP)

	// Fill bitreader for state updates
	CMPQ    DX, $0x20
	JL      sequenceDecs_decodeSync_bmi2_fill_3_end
	CMPQ    BX, $0x04
	JL      sequenceDecs_decodeSync_bmi2_fill_3_byte_by_byte
	SHLQ    $0x20, AX
	SUBQ    $0x04, R13
	SUBQ    $0x04, BX
	SUBQ    $0x20, DX
	MOVLQZX (R13), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decodeSync_bmi2_fill_3_end

sequenceDecs_decodeSync_bmi2_fill_3_byte_by_byte:
	CMPQ    BX, $0x00
	JLE     sequenceDecs_decodeSync_bmi2_fill_3_end
	SHLQ    $0x08, AX
	SUBQ    $0x01, R13
	SUBQ    $0x01, BX
	SUBQ    $0x08, DX
	MOVBQZX (R13), CX
	ORQ     CX, AX
	JMP     sequenceDecs_decodeSync_bmi2_fill_3_byte_by_byte

sequenceDecs_decodeSync_bmi2_fill_3_end:
	MOVQ   R13, (SP)
	MOVQ   $0x00000808, CX
	BEXTRQ CX, R8, R13
	MOVQ   ctx+16(FP), CX
	CMPQ   96(CX), $0x00
	JZ     sequenceDecs_decodeSync_bmi2_skip_update

	// Update Literal Length State
	MOVBQZX SI, R14
	MOVQ    $0x00001010, CX
	BEXTRQ  CX, SI, SI
	LEAQ    (DX)(R14*1), CX
	MOVQ    AX, R15
	MOVQ    CX, DX
	ROLQ    CL, R15
	BZHIQ   R14, R15, R15
	ADDQ    R15, SI

	// Load ctx.llTable
	MOVQ ctx+16(FP), CX
	MOVQ (CX), CX
	MOVQ (CX)(SI*8), SI

	// Update Match Length State
	MOVBQZX DI, R14
	MOVQ    $0x00001010, CX
	BEXTRQ  CX, DI, DI
	LEAQ    (DX)(R14*1), CX
	MOVQ    AX, R15
	MOVQ    CX, DX
	ROLQ    CL, R15
	BZHIQ   R14, R15, R15
	ADDQ    R15, DI

	// Load ctx.mlTable
	MOVQ ctx+16(FP), CX
	MOVQ 24(CX), CX
	MOVQ (CX)(DI*8), DI

	// Update Offset State
	MOVBQZX R8, R14
	MOVQ    $0x00001010, CX
	BEXTRQ  CX, R8, R8
	LEAQ    (DX)(R14*1), CX
	MOVQ    AX, R15
	MOVQ    CX, DX
	ROLQ    CL, R15
	BZHIQ   R14, R15, R15
	ADDQ    R15, R8

	// Load ctx.ofTable
	MOVQ ctx+16(FP), CX
	MOVQ 48(CX), CX
	MOVQ (CX)(R8*8), R8

sequenceDecs_decodeSync_bmi2_skip_update:
	// Adjust offset
	MOVQ   s+0(FP), CX
	MOVQ   8(SP), R14
	CMPQ   R13, $0x01
	JBE    sequenceDecs_decodeSync_bmi2_adjust_offsetB_1_or_0
	MOVUPS 144(CX), X0
	MOVQ   R14, 144(CX)
	MOVUPS X0, 152(CX)
	JMP    sequenceDecs_decodeSync_bmi2_adjust_end

sequenceDecs_decodeSync_bmi2_adjust_offsetB_1_or_0:
	CMPQ 24(SP), $0x00000000
	JNE  sequenceDecs_decodeSync_bmi2_adjust_offset_maybezero
	INCQ R14
	JMP  sequenceDecs_decodeSync_bmi2_adjust_offset_nonzero

sequenceDecs_decodeSync_bmi2_adjust_offset_maybezero:
	TESTQ R14, R14
	JNZ   sequenceDecs_decodeSync_bmi2_adjust_offset_nonzero
	MOVQ  144(CX), R14
	JMP   sequenceDecs_decodeSync_bmi2_adjust_end

sequenceDecs_decodeSync_bmi2_adjust_offset_nonzero:
	MOVQ    R14, R13
	XORQ    R15, R15
	MOVQ    $-1, BP
	CMPQ    R14, $0x03
	CMOVQEQ R15, R13
	CMOVQEQ BP, R15
	LEAQ    144(CX), BP
	ADDQ    (BP)(R13*8), R15
	JNZ     sequenceDecs_decodeSync_bmi2_adjust_temp_valid
	MOVQ    $0x00000001, R15

sequenceDecs_decodeSync_bmi2_adjust_temp_valid:
	CMPQ R14, $0x01
	JZ   sequenceDecs_decodeSync_bmi2_adjust_skip
	MOVQ 152(CX), R13
	MOVQ R13, 160(CX)

sequenceDecs_decodeSync_bmi2_adjust_skip:
	MOVQ 144(CX), R13
	MOVQ R13, 152(CX)
	MOVQ R15, 144(CX)
	MOVQ R15, R14

sequenceDecs_decodeSync_bmi2_adjust_end:
	MOVQ R14, 8(SP)

	// Check values
	MOVQ  16(SP), CX
	MOVQ  24(SP), R13
	LEAQ  (CX)(R13*1), R15
	MOVQ  s+0(FP), BP
	ADDQ  R15, 256(BP)
	MOVQ  ctx+16(FP), R15
	SUBQ  R13, 104(R15)
	CMPQ  CX, $0x00020002
	JA    sequenceDecs_decodeSync_bmi2_error_match_len_too_big
	TESTQ R14, R14
	JNZ   sequenceDecs_decodeSync_bmi2_match_len_ofs_ok
	TESTQ CX, CX
	JNZ   sequenceDecs_decodeSync_bmi2_error_match_len_ofs_mismatch

sequenceDecs_decodeSync_bmi2_match_len_ofs_ok:
	// Copy literals
	MOVQ  24(SP), CX
	TESTQ CX, CX
	JZ    copy_match
	XORQ  R13, R13

copy_1:
	MOVUPS (R11)(R13*1), X0
	MOVUPS X0, (R10)(R13*1)
	ADDQ   $0x10, R13
	CMPQ   R13, CX
	JB     copy_1
	ADDQ   CX, R11
	ADDQ   CX, R10
	ADDQ   CX, R12

	// Copy match
copy_match:
	MOVQ  16(SP), CX
	TESTQ CX, CX
	JZ    handle_loop
	MOVQ  8(SP), R13

	// Malformed input if seq.mo > t || seq.mo > s.windowSize
	CMPQ R13, R12
	JG   error_match_off_too_big
	MOVQ R10, R14
	SUBQ R13, R14

	// ml <= mo
	CMPQ CX, R13
	JA   copy_overlapping_match

	// Copy non-overlapping match
	XORQ R13, R13

copy_2:
	MOVUPS (R14)(R13*1), X0
	MOVUPS X0, (R10)(R13*1)
	ADDQ   $0x10, R13
	CMPQ   R13, CX
	JB     copy_2
	ADDQ   CX, R10
	ADDQ   CX, R12
	JMP    handle_loop

	// Copy overlapping match
copy_overlapping_match:
	XORQ R13, R13

copy_slow_3:
	MOVB (R14)(R13*1), R15
	MOVB R15, (R10)(R13*1)
	INCQ R13
	CMPQ R13, CX
	JB   copy_slow_3
	ADDQ CX, R10
	ADDQ CX, R12

handle_loop:
	ADDQ $0x18, R9
	MOVQ ctx+16(FP), CX
	DECQ 96(CX)
	JNS  sequenceDecs_decodeSync_bmi2_main_loop
	MOVQ br+8(FP), CX
	MOVQ AX, 32(CX)
	MOVB DL, 40(CX)
	MOVQ BX, 24(CX)

	// Return success
	MOVQ $0x00000000, ret+24(FP)
	RET

	// Return with match length error
sequenceDecs_decodeSync_bmi2_error_match_len_ofs_mismatch:
	MOVQ $0x00000001, ret+24(FP)
	RET

	// Return with match length too long error
sequenceDecs_decodeSync_bmi2_error_match_len_too_big:
	MOVQ $0x00000002, ret+24(FP)
	RET

	// Return with match offset too long error
error_match_off_too_big:
	MOVQ $0x00000003, ret+24(FP)
	RET
	RET
