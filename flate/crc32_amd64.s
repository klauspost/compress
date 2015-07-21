//+build !noasm !appengine

// func crc32sse(a []byte) int
TEXT ·crc32sse(SB),7, $0
    MOVQ    a+0(FP), R10
    MOVQ    $0, BX
    MOVL    (R10), AX

    // CRC32   EAX, EBX
    BYTE $0xF2; BYTE $0x0f; 
    BYTE $0x38; BYTE $0xf1; BYTE $0xd8

    MOVQ    BX, ret+24(FP)
    RET

// func crc32sseAll(a []byte, dst []uint32)
TEXT ·crc32sseAll(SB), 7, $0
    MOVQ    a+0(FP), R8
    MOVQ    a_len+8(FP), R10
    MOVQ    dst+24(FP), R9
    MOVQ    $0, AX
    SUBQ    $3, R10
    JZ      end
    JS      end
    MOVQ    R10, R13
    SHRQ    $2, R10  // len/4
    ANDQ    $3, R13  // len&3
    TESTQ   R10,R10
    JZ      remain_crc

crc_loop:
    MOVQ    (R8), R11
    XORQ    BX,BX
    XORQ    DX,DX
    XORQ    DI,DI
    MOVQ    R11, R12
    SHRQ    $8, R12
    MOVQ    R11, AX
    MOVQ    R12, CX
    SHRQ    $16, R11
    SHRQ    $16, R12
    MOVQ    R11, SI

    // CRC32   EAX, EBX
    BYTE $0xF2; BYTE $0x0f; 
    BYTE $0x38; BYTE $0xf1; BYTE $0xd8
    // CRC32   ECX, EDX
    BYTE $0xF2; BYTE $0x0f; 
    BYTE $0x38; BYTE $0xf1; BYTE $0xd1
    // CRC32   ESI, EDI
    BYTE $0xF2; BYTE $0x0f; 
    BYTE $0x38; BYTE $0xf1; BYTE $0xfe
    MOVL    BX, (R9)
    MOVL    DX, 4(R9)
    MOVL    DI, 8(R9)

    XORQ    BX, BX
    MOVL    R12, AX

    // CRC32   EAX, EBX
    BYTE $0xF2; BYTE $0x0f; 
    BYTE $0x38; BYTE $0xf1; BYTE $0xd8
    MOVL    BX, 12(R9)

    ADDQ    $16, R9
    ADDQ    $4, R8
    SUBQ    $1, R10
    JNZ     crc_loop

remain_crc:
    XORQ    BX, BX
    TESTQ    R13, R13
    JZ      end
rem_loop:
    XORQ    AX, AX
    MOVL    (R8), AX
    XORQ    BX, BX

    // CRC32   EAX, EBX
    BYTE $0xF2; BYTE $0x0f; 
    BYTE $0x38; BYTE $0xf1; BYTE $0xd8

    MOVL    BX,(R9)
    ADDQ    $4, R9
    ADDQ    $1, R8
    SUBQ    $1, R13
    JNZ    rem_loop
end:
    RET
