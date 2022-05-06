// Code generated by command: go run gen.go -out ../decompress_amd64.s -pkg=huff0. DO NOT EDIT.

//go:build amd64 && !appengine && !noasm && gc

// func decompress4x_main_loop_amd64(ctx *decompress4xContext)
TEXT ·decompress4x_main_loop_amd64(SB), $8-8
	XORQ DX, DX

	// Preload values
	MOVQ    ctx+0(FP), AX
	MOVBQZX 32(AX), SI
	MOVQ    40(AX), DI
	MOVQ    DI, BX
	MOVQ    72(AX), CX
	MOVQ    CX, (SP)
	MOVQ    48(AX), R8
	MOVQ    56(AX), R9
	MOVQ    (AX), R10
	MOVQ    8(AX), R11
	MOVQ    16(AX), R12
	MOVQ    24(AX), R13

	// Main loop
main_loop:
	MOVQ  BX, DI
	CMPQ  DI, (SP)
	SETGE DL

	// br0.fillFast32()
	MOVQ    32(R10), R14
	MOVBQZX 40(R10), R15
	CMPQ    R15, $0x20
	JBE     skip_fill0
	MOVQ    24(R10), AX
	SUBQ    $0x20, R15
	SUBQ    $0x04, AX
	MOVQ    (R10), BP

	// b.value |= uint64(low) << (b.bitsRead & 63)
	MOVL (AX)(BP*1), BP
	MOVQ R15, CX
	SHLQ CL, BP
	MOVQ AX, 24(R10)
	ORQ  BP, R14

	// exhausted = exhausted || (br0.off < 4)
	CMPQ  AX, $0x04
	SETLT AL
	ORB   AL, DL

skip_fill0:
	// val0 := br0.peekTopBits(peekBits)
	MOVQ R14, BP
	MOVQ SI, CX
	SHRQ CL, BP

	// v0 := table[val0&mask]
	MOVW (R9)(BP*2), CX

	// br0.advance(uint8(v0.entry)
	MOVB CH, AL
	SHLQ CL, R14
	ADDB CL, R15

	// val1 := br0.peekTopBits(peekBits)
	MOVQ SI, CX
	MOVQ R14, BP
	SHRQ CL, BP

	// v1 := table[val1&mask]
	MOVW (R9)(BP*2), CX

	// br0.advance(uint8(v1.entry))
	MOVB CH, AH
	SHLQ CL, R14
	ADDB CL, R15

	// these two writes get coalesced
	// out[stream][off] = uint8(v0.entry >> 8)
	// out[stream][off+1] = uint8(v1.entry >> 8)
	MOVW AX, (DI)

	// update the bitrader reader structure
	MOVQ R14, 32(R10)
	MOVB R15, 40(R10)
	ADDQ R8, DI

	// br1.fillFast32()
	MOVQ    32(R11), R14
	MOVBQZX 40(R11), R15
	CMPQ    R15, $0x20
	JBE     skip_fill1
	MOVQ    24(R11), AX
	SUBQ    $0x20, R15
	SUBQ    $0x04, AX
	MOVQ    (R11), BP

	// b.value |= uint64(low) << (b.bitsRead & 63)
	MOVL (AX)(BP*1), BP
	MOVQ R15, CX
	SHLQ CL, BP
	MOVQ AX, 24(R11)
	ORQ  BP, R14

	// exhausted = exhausted || (br1.off < 4)
	CMPQ  AX, $0x04
	SETLT AL
	ORB   AL, DL

skip_fill1:
	// val0 := br1.peekTopBits(peekBits)
	MOVQ R14, BP
	MOVQ SI, CX
	SHRQ CL, BP

	// v0 := table[val0&mask]
	MOVW (R9)(BP*2), CX

	// br1.advance(uint8(v0.entry)
	MOVB CH, AL
	SHLQ CL, R14
	ADDB CL, R15

	// val1 := br1.peekTopBits(peekBits)
	MOVQ SI, CX
	MOVQ R14, BP
	SHRQ CL, BP

	// v1 := table[val1&mask]
	MOVW (R9)(BP*2), CX

	// br1.advance(uint8(v1.entry))
	MOVB CH, AH
	SHLQ CL, R14
	ADDB CL, R15

	// these two writes get coalesced
	// out[stream][off] = uint8(v0.entry >> 8)
	// out[stream][off+1] = uint8(v1.entry >> 8)
	MOVW AX, (DI)

	// update the bitrader reader structure
	MOVQ R14, 32(R11)
	MOVB R15, 40(R11)
	ADDQ R8, DI

	// br2.fillFast32()
	MOVQ    32(R12), R14
	MOVBQZX 40(R12), R15
	CMPQ    R15, $0x20
	JBE     skip_fill2
	MOVQ    24(R12), AX
	SUBQ    $0x20, R15
	SUBQ    $0x04, AX
	MOVQ    (R12), BP

	// b.value |= uint64(low) << (b.bitsRead & 63)
	MOVL (AX)(BP*1), BP
	MOVQ R15, CX
	SHLQ CL, BP
	MOVQ AX, 24(R12)
	ORQ  BP, R14

	// exhausted = exhausted || (br2.off < 4)
	CMPQ  AX, $0x04
	SETLT AL
	ORB   AL, DL

skip_fill2:
	// val0 := br2.peekTopBits(peekBits)
	MOVQ R14, BP
	MOVQ SI, CX
	SHRQ CL, BP

	// v0 := table[val0&mask]
	MOVW (R9)(BP*2), CX

	// br2.advance(uint8(v0.entry)
	MOVB CH, AL
	SHLQ CL, R14
	ADDB CL, R15

	// val1 := br2.peekTopBits(peekBits)
	MOVQ SI, CX
	MOVQ R14, BP
	SHRQ CL, BP

	// v1 := table[val1&mask]
	MOVW (R9)(BP*2), CX

	// br2.advance(uint8(v1.entry))
	MOVB CH, AH
	SHLQ CL, R14
	ADDB CL, R15

	// these two writes get coalesced
	// out[stream][off] = uint8(v0.entry >> 8)
	// out[stream][off+1] = uint8(v1.entry >> 8)
	MOVW AX, (DI)

	// update the bitrader reader structure
	MOVQ R14, 32(R12)
	MOVB R15, 40(R12)
	ADDQ R8, DI

	// br3.fillFast32()
	MOVQ    32(R13), R14
	MOVBQZX 40(R13), R15
	CMPQ    R15, $0x20
	JBE     skip_fill3
	MOVQ    24(R13), AX
	SUBQ    $0x20, R15
	SUBQ    $0x04, AX
	MOVQ    (R13), BP

	// b.value |= uint64(low) << (b.bitsRead & 63)
	MOVL (AX)(BP*1), BP
	MOVQ R15, CX
	SHLQ CL, BP
	MOVQ AX, 24(R13)
	ORQ  BP, R14

	// exhausted = exhausted || (br3.off < 4)
	CMPQ  AX, $0x04
	SETLT AL
	ORB   AL, DL

skip_fill3:
	// val0 := br3.peekTopBits(peekBits)
	MOVQ R14, BP
	MOVQ SI, CX
	SHRQ CL, BP

	// v0 := table[val0&mask]
	MOVW (R9)(BP*2), CX

	// br3.advance(uint8(v0.entry)
	MOVB CH, AL
	SHLQ CL, R14
	ADDB CL, R15

	// val1 := br3.peekTopBits(peekBits)
	MOVQ SI, CX
	MOVQ R14, BP
	SHRQ CL, BP

	// v1 := table[val1&mask]
	MOVW (R9)(BP*2), CX

	// br3.advance(uint8(v1.entry))
	MOVB CH, AH
	SHLQ CL, R14
	ADDB CL, R15

	// these two writes get coalesced
	// out[stream][off] = uint8(v0.entry >> 8)
	// out[stream][off+1] = uint8(v1.entry >> 8)
	MOVW AX, (DI)

	// update the bitrader reader structure
	MOVQ  R14, 32(R13)
	MOVB  R15, 40(R13)
	ADDQ  $0x02, BX
	TESTB DL, DL
	JZ    main_loop
	MOVQ  ctx+0(FP), AX
	MOVQ  40(AX), CX
	MOVQ  BX, DX
	SUBQ  CX, DX
	SHLQ  $0x02, DX
	MOVQ  DX, 64(AX)
	RET

// func decompress4x_8b_main_loop_amd64(ctx *decompress4xContext)
TEXT ·decompress4x_8b_main_loop_amd64(SB), $16-8
	XORQ DX, DX

	// Preload values
	MOVQ    ctx+0(FP), CX
	MOVBQZX 32(CX), BX
	MOVQ    40(CX), SI
	MOVQ    SI, (SP)
	MOVQ    72(CX), DX
	MOVQ    DX, 8(SP)
	MOVQ    48(CX), DI
	MOVQ    56(CX), R8
	MOVQ    (CX), R9
	MOVQ    8(CX), R10
	MOVQ    16(CX), R11
	MOVQ    24(CX), R12

	// Main loop
main_loop:
	MOVQ  (SP), SI
	CMPQ  SI, 8(SP)
	SETGE DL

	// br1000.fillFast32()
	MOVQ    32(R9), R13
	MOVBQZX 40(R9), R14
	CMPQ    R14, $0x20
	JBE     skip_fill1000
	MOVQ    24(R9), R15
	SUBQ    $0x20, R14
	SUBQ    $0x04, R15
	MOVQ    (R9), BP

	// b.value |= uint64(low) << (b.bitsRead & 63)
	MOVL (R15)(BP*1), BP
	MOVQ R14, CX
	SHLQ CL, BP
	MOVQ R15, 24(R9)
	ORQ  BP, R13

	// exhausted = exhausted || (br1000.off < 4)
	CMPQ  R15, $0x04
	SETLT AL
	ORB   AL, DL

skip_fill1000:
	// val0 := br0.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v0 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br0.advance(uint8(v0.entry)
	MOVB CH, AL
	SHLQ CL, R13
	ADDB CL, R14

	// val1 := br0.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v1 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br0.advance(uint8(v1.entry)
	MOVB   CH, AH
	SHLQ   CL, R13
	ADDB   CL, R14
	BSWAPL AX

	// val2 := br0.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v2 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br0.advance(uint8(v2.entry)
	MOVB CH, AH
	SHLQ CL, R13
	ADDB CL, R14

	// val3 := br0.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v3 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br0.advance(uint8(v3.entry)
	MOVB   CH, AL
	SHLQ   CL, R13
	ADDB   CL, R14
	BSWAPL AX

	// these four writes get coalesced
	// buf[stream][off] = uint8(v0.entry >> 8)
	// buf[stream][off+1] = uint8(v1.entry >> 8)
	// buf[stream][off+2] = uint8(v2.entry >> 8)
	// buf[stream][off+3] = uint8(v3.entry >> 8)
	MOVL AX, (SI)

	// update the bitreader reader structure
	MOVQ R13, 32(R9)
	MOVB R14, 40(R9)
	ADDQ DI, SI

	// br1001.fillFast32()
	MOVQ    32(R10), R13
	MOVBQZX 40(R10), R14
	CMPQ    R14, $0x20
	JBE     skip_fill1001
	MOVQ    24(R10), R15
	SUBQ    $0x20, R14
	SUBQ    $0x04, R15
	MOVQ    (R10), BP

	// b.value |= uint64(low) << (b.bitsRead & 63)
	MOVL (R15)(BP*1), BP
	MOVQ R14, CX
	SHLQ CL, BP
	MOVQ R15, 24(R10)
	ORQ  BP, R13

	// exhausted = exhausted || (br1001.off < 4)
	CMPQ  R15, $0x04
	SETLT AL
	ORB   AL, DL

skip_fill1001:
	// val0 := br1.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v0 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br1.advance(uint8(v0.entry)
	MOVB CH, AL
	SHLQ CL, R13
	ADDB CL, R14

	// val1 := br1.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v1 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br1.advance(uint8(v1.entry)
	MOVB   CH, AH
	SHLQ   CL, R13
	ADDB   CL, R14
	BSWAPL AX

	// val2 := br1.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v2 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br1.advance(uint8(v2.entry)
	MOVB CH, AH
	SHLQ CL, R13
	ADDB CL, R14

	// val3 := br1.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v3 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br1.advance(uint8(v3.entry)
	MOVB   CH, AL
	SHLQ   CL, R13
	ADDB   CL, R14
	BSWAPL AX

	// these four writes get coalesced
	// buf[stream][off] = uint8(v0.entry >> 8)
	// buf[stream][off+1] = uint8(v1.entry >> 8)
	// buf[stream][off+2] = uint8(v2.entry >> 8)
	// buf[stream][off+3] = uint8(v3.entry >> 8)
	MOVL AX, (SI)

	// update the bitreader reader structure
	MOVQ R13, 32(R10)
	MOVB R14, 40(R10)
	ADDQ DI, SI

	// br1002.fillFast32()
	MOVQ    32(R11), R13
	MOVBQZX 40(R11), R14
	CMPQ    R14, $0x20
	JBE     skip_fill1002
	MOVQ    24(R11), R15
	SUBQ    $0x20, R14
	SUBQ    $0x04, R15
	MOVQ    (R11), BP

	// b.value |= uint64(low) << (b.bitsRead & 63)
	MOVL (R15)(BP*1), BP
	MOVQ R14, CX
	SHLQ CL, BP
	MOVQ R15, 24(R11)
	ORQ  BP, R13

	// exhausted = exhausted || (br1002.off < 4)
	CMPQ  R15, $0x04
	SETLT AL
	ORB   AL, DL

skip_fill1002:
	// val0 := br2.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v0 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br2.advance(uint8(v0.entry)
	MOVB CH, AL
	SHLQ CL, R13
	ADDB CL, R14

	// val1 := br2.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v1 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br2.advance(uint8(v1.entry)
	MOVB   CH, AH
	SHLQ   CL, R13
	ADDB   CL, R14
	BSWAPL AX

	// val2 := br2.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v2 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br2.advance(uint8(v2.entry)
	MOVB CH, AH
	SHLQ CL, R13
	ADDB CL, R14

	// val3 := br2.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v3 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br2.advance(uint8(v3.entry)
	MOVB   CH, AL
	SHLQ   CL, R13
	ADDB   CL, R14
	BSWAPL AX

	// these four writes get coalesced
	// buf[stream][off] = uint8(v0.entry >> 8)
	// buf[stream][off+1] = uint8(v1.entry >> 8)
	// buf[stream][off+2] = uint8(v2.entry >> 8)
	// buf[stream][off+3] = uint8(v3.entry >> 8)
	MOVL AX, (SI)

	// update the bitreader reader structure
	MOVQ R13, 32(R11)
	MOVB R14, 40(R11)
	ADDQ DI, SI

	// br1003.fillFast32()
	MOVQ    32(R12), R13
	MOVBQZX 40(R12), R14
	CMPQ    R14, $0x20
	JBE     skip_fill1003
	MOVQ    24(R12), R15
	SUBQ    $0x20, R14
	SUBQ    $0x04, R15
	MOVQ    (R12), BP

	// b.value |= uint64(low) << (b.bitsRead & 63)
	MOVL (R15)(BP*1), BP
	MOVQ R14, CX
	SHLQ CL, BP
	MOVQ R15, 24(R12)
	ORQ  BP, R13

	// exhausted = exhausted || (br1003.off < 4)
	CMPQ  R15, $0x04
	SETLT AL
	ORB   AL, DL

skip_fill1003:
	// val0 := br3.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v0 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br3.advance(uint8(v0.entry)
	MOVB CH, AL
	SHLQ CL, R13
	ADDB CL, R14

	// val1 := br3.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v1 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br3.advance(uint8(v1.entry)
	MOVB   CH, AH
	SHLQ   CL, R13
	ADDB   CL, R14
	BSWAPL AX

	// val2 := br3.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v2 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br3.advance(uint8(v2.entry)
	MOVB CH, AH
	SHLQ CL, R13
	ADDB CL, R14

	// val3 := br3.peekTopBits(peekBits)
	MOVQ R13, R15
	MOVQ BX, CX
	SHRQ CL, R15

	// v3 := table[val0&mask]
	MOVW (R8)(R15*2), CX

	// br3.advance(uint8(v3.entry)
	MOVB   CH, AL
	SHLQ   CL, R13
	ADDB   CL, R14
	BSWAPL AX

	// these four writes get coalesced
	// buf[stream][off] = uint8(v0.entry >> 8)
	// buf[stream][off+1] = uint8(v1.entry >> 8)
	// buf[stream][off+2] = uint8(v2.entry >> 8)
	// buf[stream][off+3] = uint8(v3.entry >> 8)
	MOVL AX, (SI)

	// update the bitreader reader structure
	MOVQ  R13, 32(R12)
	MOVB  R14, 40(R12)
	ADDQ  $0x04, (SP)
	TESTB DL, DL
	JZ    main_loop
	MOVQ  ctx+0(FP), AX
	MOVQ  40(AX), CX
	MOVQ  (SP), DX
	SUBQ  CX, DX
	SHLQ  $0x02, DX
	MOVQ  DX, 64(AX)
	RET
