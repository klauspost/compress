// Code generated by command: go run gen.go -out seqdec_amd64.s -stubs delme.go -pkg=zstd. DO NOT EDIT.

//go:build !appengine && !noasm && gc && !noasm
// +build !appengine,!noasm,gc,!noasm

// func sequenceDecs_decode_amd64(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int
// Requires: CMOV
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
	MOVQ R11, (SP)
	MOVQ R9, AX
	MOVQ ctx+16(FP), CX
	CMPQ 96(CX), $0x00
	JZ   sequenceDecs_decode_amd64_skip_update

	// Update Literal Length State
	MOVBQZX DI, R11
	SHRQ    $0x10, DI
	MOVWQZX DI, DI
	CMPQ    R11, $0x00
	JZ      sequenceDecs_decode_amd64_llState_updateState_skip
	MOVQ    BX, CX
	ADDQ    R11, BX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVQ    R11, CX
	NEGQ    CX
	SHRQ    CL, R12
	TESTQ   R11, R11
	CMOVQEQ R11, R12
	ADDQ    R12, DI

sequenceDecs_decode_amd64_llState_updateState_skip:
	// Load ctx.llTable
	MOVQ ctx+16(FP), CX
	MOVQ (CX), CX
	MOVQ (CX)(DI*8), DI

	// Update Match Length State
	MOVBQZX R8, R11
	SHRQ    $0x10, R8
	MOVWQZX R8, R8
	CMPQ    R11, $0x00
	JZ      sequenceDecs_decode_amd64_mlState_updateState_skip
	MOVQ    BX, CX
	ADDQ    R11, BX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVQ    R11, CX
	NEGQ    CX
	SHRQ    CL, R12
	TESTQ   R11, R11
	CMOVQEQ R11, R12
	ADDQ    R12, R8

sequenceDecs_decode_amd64_mlState_updateState_skip:
	// Load ctx.mlTable
	MOVQ ctx+16(FP), CX
	MOVQ 24(CX), CX
	MOVQ (CX)(R8*8), R8

	// Update Offset State
	MOVBQZX R9, R11
	SHRQ    $0x10, R9
	MOVWQZX R9, R9
	CMPQ    R11, $0x00
	JZ      sequenceDecs_decode_amd64_ofState_updateState_skip
	MOVQ    BX, CX
	ADDQ    R11, BX
	MOVQ    DX, R12
	SHLQ    CL, R12
	MOVQ    R11, CX
	NEGQ    CX
	SHRQ    CL, R12
	TESTQ   R11, R11
	CMOVQEQ R11, R12
	ADDQ    R12, R9

sequenceDecs_decode_amd64_ofState_updateState_skip:
	// Load ctx.ofTable
	MOVQ ctx+16(FP), CX
	MOVQ 48(CX), CX
	MOVQ (CX)(R9*8), R9

sequenceDecs_decode_amd64_skip_update:
	SHRQ    $0x08, AX
	MOVBQZX AL, AX

	// Adjust offset
	MOVQ s+0(FP), CX
	MOVQ 16(R10), R11
	CMPQ AX, $0x01
	JBE  sequenceDecs_decode_amd64_adjust_offsetB_1_or_0
	MOVQ 144(CX), AX
	MOVQ 152(CX), R12
	MOVQ R11, 144(CX)
	MOVQ AX, 152(CX)
	MOVQ R12, 160(CX)
	JMP  sequenceDecs_decode_amd64_adjust_end

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

	// Return with match too long error
sequenceDecs_decode_amd64_error_match_len_too_big:
	MOVQ $0x00000002, ret+24(FP)
	RET

// func sequenceDecs_decode_bmi2(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int
// Requires: BMI, BMI2, CMOV
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
	MOVQ R10, (SP)
	MOVQ R8, R10
	MOVQ ctx+16(FP), CX
	CMPQ 96(CX), $0x00
	JZ   sequenceDecs_decode_bmi2_skip_update

	// Update Literal Length State
	MOVBQZX SI, R11
	SHRQ    $0x10, SI
	MOVWQZX SI, SI
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
	SHRQ    $0x10, DI
	MOVWQZX DI, DI
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
	SHRQ    $0x10, R8
	MOVWQZX R8, R8
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
	SHRQ    $0x08, R10
	MOVBQZX R10, R10

	// Adjust offset
	MOVQ s+0(FP), CX
	MOVQ 16(R9), R11
	CMPQ R10, $0x01
	JBE  sequenceDecs_decode_bmi2_adjust_offsetB_1_or_0
	MOVQ 144(CX), R10
	MOVQ 152(CX), R12
	MOVQ R11, 144(CX)
	MOVQ R10, 152(CX)
	MOVQ R12, 160(CX)
	JMP  sequenceDecs_decode_bmi2_adjust_end

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

	// Return with match too long error
sequenceDecs_decode_bmi2_error_match_len_too_big:
	MOVQ $0x00000002, ret+24(FP)
	RET

// func sequenceDecs_executeSimple_amd64(ctx *executeAsmContext) bool
// Requires: SSE
TEXT ·sequenceDecs_executeSimple_amd64(SB), $0-9
	MOVQ ctx+0(FP), R9
	MOVQ (R9), AX
	MOVQ 8(R9), CX
	MOVQ 24(R9), DX
	MOVQ 32(R9), BX
	MOVQ 40(R9), SI
	MOVQ 56(R9), DI
	MOVQ 80(R9), R8
	MOVQ 88(R9), R9

	// seqsBase += 24 * seqIndex
	LEAQ (DX)(DX*2), R10
	SHLQ $0x03, R10
	ADDQ R10, AX

	// outBase += outPosition
	ADDQ R8, BX

main_loop:
	MOVQ 8(AX), R10
	MOVQ (AX), R11

	// Check if we won't overflow ctx.out while fast copying
	LEAQ (R10)(R11*1), R12
	LEAQ 16(R8)(R12*1), R13
	CMPQ R13, SI
	JA   slow_path

	// Update the counters upfront
	ADDQ R12, R8
	ADDQ R11, R9

	// Copy literals
	TESTQ R11, R11
	JZ    copy_match
	XORQ  R12, R12

copy_1:
	MOVUPS (DI)(R12*1), X0
	MOVUPS X0, (BX)(R12*1)
	ADDQ   $0x10, R12
	CMPQ   R12, R11
	JB     copy_1
	ADDQ   R11, DI
	ADDQ   R11, BX

	// Copy match
copy_match:
	TESTQ R10, R10
	JZ    handle_loop
	MOVQ  16(AX), R11
	MOVQ  BX, R12
	SUBQ  R11, R12

	// ml <= mo
	CMPQ R10, R11
	JA   copy_overalapping_match

	// Copy non-overlapping match
	XORQ R11, R11

copy_2:
	MOVUPS (R12)(R11*1), X0
	MOVUPS X0, (BX)(R11*1)
	ADDQ   $0x10, R11
	CMPQ   R11, R10
	JB     copy_2
	ADDQ   R10, BX
	JMP    handle_loop

	// Copy overlapping match
copy_overalapping_match:
	XORQ R11, R11

copy_slow_3:
	MOVB (R12)(R11*1), R13
	MOVB R13, (BX)(R11*1)
	INCQ R11
	CMPQ R11, R10
	JB   copy_slow_3
	ADDQ R10, BX

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
	MOVQ R9, 88(AX)
	RET

slow_path:
	// Return value
	MOVB $0x00, ret+8(FP)

	// Update the context
	MOVQ ctx+0(FP), AX
	MOVQ DX, 24(AX)
	MOVQ R8, 80(AX)
	MOVQ R9, 88(AX)
	RET
