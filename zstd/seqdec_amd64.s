// +build !appengine
// +build gc
// +build !noasm

#include "textflag.h"
#include "funcdata.h"
#include "go_asm.h"

#ifdef GOAMD64_v4
#ifndef GOAMD64_v3
#define GOAMD64_v3
#endif
#endif

#define maxMatchLen 131074

// Please keep in sync with consts from `seqdec_amd64.go`
#define errorMatchLenOfsMismatch 1
#define errorMatchLenTooBig 2

// func sequenceDecs_decode_amd64(s *sequenceDecs, br *bitReader, ctx *decodeAsmContext) int
TEXT ·sequenceDecs_decode_amd64(SB), NOSPLIT, $8
/*
    This procedure implements the following sequence:

    for ctx.iteration >= 0 {
        // s.next()
        br.fill()
        mo, moB := ofState.final()
        mo += br.getBits(moB)

        br.fill()
        ml, mlB := mlState.final()
        ml += br.getBits(mlB)

        ll, llB := llState.final()
        ll += br.getBits(llB)

        br.fill()
        if ctx.iteration != 0 {
            nBits := ctx.llState.nbBits() + ctx.mlState.nbBits() + ctx.ofState.nbBits()
            bits := br.get32BitsFast(nBits)
            lowBits := uint16(bits >> ((ofState.nbBits() + mlState.nbBits()) & 31))
            llState = llTable[(llState.newState()+lowBits)&maxTableMask]

            lowBits = uint16(bits >> (ofState.nbBits() & 31))
            lowBits &= bitMask[mlState.nbBits()&15]
            mlState = mlTable[(mlState.newState()+lowBits)&maxTableMask]

            lowBits = uint16(bits) & bitMask[ofState.nbBits()&15]
            ofState = ofTable[(ofState.newState()+lowBits)&maxTableMask]
        }

        mo = s.adjustOffset(mo, ll, moB)

        if ml > maxMatchLen {
            return errorMatchLenTooBig
        }
        if mo == 0 && ml > 0 {
            return errorMatchLenOfsMismatch
        }

        ctx.iteration -= 1
    }

    return 0

*/
#define br_value        R8 // br.value
#define br_bits_read    R9 // br.bitsRead
#define br_offset       R10 // br.offset
#define br_pointer      BP // &br.out[0]

#define llState         R11 // ctx.llState
#define mlState         R12 // ctx.mlState
#define ofState         R13 // ctx.ofState

#define seqs            SI // ctx.seqs

// AX, BX, CX, DX, DI, R14 and R15 are clobberred in the loop
// The registers R8-R13 and BP are preserved.

	MOVQ BP, 0(SP)

	// 1. load bitReader (done once)
	MOVQ    br+8(FP), DI
	MOVQ    bitReader_value(DI), br_value
	MOVBQZX bitReader_bitsRead(DI), br_bits_read
	MOVQ    bitReader_off(DI), br_offset

	MOVQ bitReader_in(DI), br_pointer // br.in data pointer
	ADDQ br_offset, br_pointer

	// 2. preload states (done once)
	MOVQ ctx+16(FP), DI
	MOVQ decodeAsmContext_llState(DI), llState
	MOVQ decodeAsmContext_mlState(DI), mlState
	MOVQ decodeAsmContext_ofState(DI), ofState

	// 3. load seqs address
	MOVQ decodeAsmContext_seqs(DI), seqs

main_loop:
	// the main procedure
	// br.fill()

	// bitreader_fill begin
	CMPQ br_bits_read, $32      // b.bitsRead < 32
	JL   br_fill_end_1
	CMPQ br_offset, $4          // b.off >= 4
	JL   br_fill_byte_by_byte_1

br_fill_fast_1:
	SUBQ $4, br_pointer
	SUBQ $4, br_offset
	SUBQ $32, br_bits_read

	SHLQ    $32, br_value    // b.value << 32 | uint32(mem)
	MOVLQZX (br_pointer), AX
	ORQ     AX, br_value
	JMP     br_fill_end_1

br_fill_byte_by_byte_1:
	CMPQ br_offset, $0 // for b.off > 0
	JLE  br_fill_end_1

	SUBQ $1, br_pointer
	SUBQ $1, br_offset
	SUBQ $8, br_bits_read

	SHLQ    $8, br_value
	MOVBQZX (br_pointer), AX
	ORQ     AX, br_value
	JMP     br_fill_byte_by_byte_1

br_fill_end_1:
	// bitreader_fill end

	// mo, moB := ofState.final()
	// mo += br.getBits(moB)
#ifdef GOAMD64_v3
	MOVQ    ofState, AX
	MOVQ    br_value, BX
	MOVB    AH, DL
	MOVBQZX DL, DX                   // DX = n
	LEAQ    (br_bits_read)(DX*1), CX // CX = r.bitsRead + n
	ROLQ    CL, BX
	BZHIQ   DX, BX, BX
	MOVQ    CX, br_bits_read         // br.bitsRead += moB
	SHRQ    $32, AX                  // AX = mo (ofState.baselineInt(), that's the higer dword of moState)
	ADDQ    BX, AX                   // AX - mo + br.getBits(moB)
	MOVQ    AX, seqVals_mo(seqs)

#else
	MOVQ    ofState, AX
	MOVQ    br_bits_read, CX
	MOVQ    br_value, BX
	SHLQ    CL, BX               // BX = br.value << br.bitsRead (part of getBits)
	MOVB    AH, CL               // CX = moB  (ofState.addBits(), that is byte #1 of moState)
	ADDQ    CX, br_bits_read     // br.bitsRead += n (part of getBits)
	NEGL    CX                   // CX = 64 - n
	SHRQ    CL, BX               // BX = (br.value << br.bitsRead) >> (64 - n) -- getBits() result
	SHRQ    $32, AX              // AX = mo (ofState.baselineInt(), that's the higer dword of moState)
	TESTQ   CX, CX
	CMOVQEQ CX, BX               // BX is zero if n is zero
	ADDQ    BX, AX               // AX - mo + br.getBits(moB)
	MOVQ    AX, seqVals_mo(seqs)

#endif

// br.fill()

	// bitreader_fill begin
	CMPQ br_bits_read, $32      // b.bitsRead < 32
	JL   br_fill_end_2
	CMPQ br_offset, $4          // b.off >= 4
	JL   br_fill_byte_by_byte_2

br_fill_fast_2:
	SUBQ $4, br_pointer
	SUBQ $4, br_offset
	SUBQ $32, br_bits_read

	SHLQ    $32, br_value    // b.value << 32 | uint32(mem)
	MOVLQZX (br_pointer), AX
	ORQ     AX, br_value
	JMP     br_fill_end_2

br_fill_byte_by_byte_2:
	CMPQ br_offset, $0 // for b.off > 0
	JLE  br_fill_end_2

	SUBQ $1, br_pointer
	SUBQ $1, br_offset
	SUBQ $8, br_bits_read

	SHLQ    $8, br_value
	MOVBQZX (br_pointer), AX
	ORQ     AX, br_value
	JMP     br_fill_byte_by_byte_2

br_fill_end_2:
	// bitreader_fill end

	// ml, mlB := mlState.final()
	// ml += br.getBits(mlB)
#ifdef GOAMD64_v3
	MOVQ    mlState, AX
	MOVQ    br_value, BX
	MOVB    AH, DL
	MOVBQZX DL, DX                   // DX = n
	LEAQ    (br_bits_read)(DX*1), CX // CX = r.bitsRead + n
	ROLQ    CL, BX
	BZHIQ   DX, BX, BX
	MOVQ    CX, br_bits_read         // br.bitsRead += moB
	SHRQ    $32, AX                  // AX = mo (ofState.baselineInt(), that's the higer dword of moState)
	ADDQ    BX, AX                   // AX - mo + br.getBits(moB)
	MOVQ    AX, seqVals_ml(seqs)

#else
	MOVQ    mlState, AX
	MOVQ    br_bits_read, CX
	MOVQ    br_value, BX
	SHLQ    CL, BX               // BX = br.value << br.bitsRead (part of getBits)
	MOVB    AH, CL               // CX = moB  (ofState.addBits(), that is byte #1 of moState)
	ADDQ    CX, br_bits_read     // br.bitsRead += n (part of getBits)
	NEGL    CX                   // CX = 64 - n
	SHRQ    CL, BX               // BX = (br.value << br.bitsRead) >> (64 - n) -- getBits() result
	SHRQ    $32, AX              // AX = mo (ofState.baselineInt(), that's the higer dword of moState)
	TESTQ   CX, CX
	CMOVQEQ CX, BX               // BX is zero if n is zero
	ADDQ    BX, AX               // AX - mo + br.getBits(moB)
	MOVQ    AX, seqVals_ml(seqs)

#endif

// ll, llB := llState.final()
// ll += br.getBits(llB)
#ifdef GOAMD64_v3
	MOVQ    llState, AX
	MOVQ    br_value, BX
	MOVB    AH, DL
	MOVBQZX DL, DX                   // DX = n
	LEAQ    (br_bits_read)(DX*1), CX // CX = r.bitsRead + n
	ROLQ    CL, BX
	BZHIQ   DX, BX, BX
	MOVQ    CX, br_bits_read         // br.bitsRead += moB
	SHRQ    $32, AX                  // AX = mo (ofState.baselineInt(), that's the higer dword of moState)
	ADDQ    BX, AX                   // AX - mo + br.getBits(moB)
	MOVQ    AX, seqVals_ll(seqs)

#else
	MOVQ    llState, AX
	MOVQ    br_bits_read, CX
	MOVQ    br_value, BX
	SHLQ    CL, BX               // BX = br.value << br.bitsRead (part of getBits)
	MOVB    AH, CL               // CX = moB  (ofState.addBits(), that is byte #1 of moState)
	ADDQ    CX, br_bits_read     // br.bitsRead += n (part of getBits)
	NEGL    CX                   // CX = 64 - n
	SHRQ    CL, BX               // BX = (br.value << br.bitsRead) >> (64 - n) -- getBits() result
	SHRQ    $32, AX              // AX = mo (ofState.baselineInt(), that's the higer dword of moState)
	TESTQ   CX, CX
	CMOVQEQ CX, BX               // BX is zero if n is zero
	ADDQ    BX, AX               // AX - mo + br.getBits(moB)
	MOVQ    AX, seqVals_ll(seqs)

#endif

// br.fill()

	// bitreader_fill begin
	CMPQ br_bits_read, $32      // b.bitsRead < 32
	JL   br_fill_end_3
	CMPQ br_offset, $4          // b.off >= 4
	JL   br_fill_byte_by_byte_3

br_fill_fast_3:
	SUBQ $4, br_pointer
	SUBQ $4, br_offset
	SUBQ $32, br_bits_read

	SHLQ    $32, br_value    // b.value << 32 | uint32(mem)
	MOVLQZX (br_pointer), AX
	ORQ     AX, br_value
	JMP     br_fill_end_3

br_fill_byte_by_byte_3:
	CMPQ br_offset, $0 // for b.off > 0
	JLE  br_fill_end_3

	SUBQ $1, br_pointer
	SUBQ $1, br_offset
	SUBQ $8, br_bits_read

	SHLQ    $8, br_value
	MOVBQZX (br_pointer), AX
	ORQ     AX, br_value
	JMP     br_fill_byte_by_byte_3

br_fill_end_3:
	// bitreader_fill end

	// if ctx.iteration != 0 {
	//     nBits := ctx.llState.nbBits() + ctx.mlState.nbBits() + ctx.ofState.nbBits()
	//     bits := br.get32BitsFast(nBits)
	//     lowBits := uint16(bits >> ((ofState.nbBits() + mlState.nbBits()) & 31))
	//     llState = llTable[(llState.newState()+lowBits)&maxTableMask]
	//     lowBits = uint16(bits >> (ofState.nbBits() & 31))
	//     lowBits &= bitMask[mlState.nbBits()&15]
	//     mlState = mlTable[(mlState.newState()+lowBits)&maxTableMask]
	//     lowBits = uint16(bits) & bitMask[ofState.nbBits()&15]
	//     ofState = ofTable[(ofState.newState()+lowBits)&maxTableMask]
	// }
	CMPQ decodeAsmContext_iteration(DI), $0
	MOVQ ofState, R14                       // copy ofState, its current value is needed below
	JZ   skip_update

	MOVQ ctx+16(FP), R15

	// lowBits := getBits(llState.nbBits())
	MOVBQZX llState, AX // AX = nbBits() -- note: nbBits is the lowest byte of state

	// BX = lowBits

#ifdef GOAMD64_v3
	LEAQ  (br_bits_read)(AX*1), CX
	MOVQ  br_value, BX
	MOVQ  CX, br_bits_read
	ROLQ  CL, BX
	BZHIQ AX, BX, BX

#else
	MOVQ    br_bits_read, CX // BX = ((br_value << br_bits_read) >> (64 - AX))
	ADDQ    AX, br_bits_read
	MOVQ    br_value, BX
	SHLQ    CL, BX
	MOVQ    AX, CX
	NEGQ    CX
	SHRQ    CL, BX
	TESTQ   AX, AX
	CMOVQEQ AX, BX

#endif

	MOVQ    llState, DX // DX = newState()
	SHRQ    $16, DX
	MOVWQZX DX, DX

	ADDQ BX, DX // DX += lowBits()

	// llState = llTable[...]
	MOVQ decodeAsmContext_llTable(R15), AX
	MOVQ (AX)(DX*8), llState

	// lowBits := getBits(mlState.nbBits())
	MOVBQZX mlState, AX // AX = nbBits() -- note: nbBits is the lowest byte of state

	// BX = lowBits

#ifdef GOAMD64_v3
	LEAQ  (br_bits_read)(AX*1), CX
	MOVQ  br_value, BX
	MOVQ  CX, br_bits_read
	ROLQ  CL, BX
	BZHIQ AX, BX, BX

#else
	MOVQ    br_bits_read, CX // BX = ((br_value << br_bits_read) >> (64 - AX))
	ADDQ    AX, br_bits_read
	MOVQ    br_value, BX
	SHLQ    CL, BX
	MOVQ    AX, CX
	NEGQ    CX
	SHRQ    CL, BX
	TESTQ   AX, AX
	CMOVQEQ AX, BX

#endif

	MOVQ    mlState, DX // DX = newState()
	SHRQ    $16, DX
	MOVWQZX DX, DX

	ADDQ BX, DX // DX += lowBits()

	// mlState = llTable[...]
	MOVQ decodeAsmContext_mlTable(R15), AX
	MOVQ (AX)(DX*8), mlState

	// lowBits := getBits(mlState.nbBits())
	MOVBQZX ofState, AX // AX = nbBits() -- note: nbBits is the lowest byte of state

	// BX = lowBits

#ifdef GOAMD64_v3
	LEAQ  (br_bits_read)(AX*1), CX
	MOVQ  br_value, BX
	MOVQ  CX, br_bits_read
	ROLQ  CL, BX
	BZHIQ AX, BX, BX

#else
	MOVQ    br_bits_read, CX // BX = ((br_value << br_bits_read) >> (64 - AX))
	ADDQ    AX, br_bits_read
	MOVQ    br_value, BX
	SHLQ    CL, BX
	MOVQ    AX, CX
	NEGQ    CX
	SHRQ    CL, BX
	TESTQ   AX, AX
	CMOVQEQ AX, BX

#endif

	MOVQ    ofState, DX // DX = newState()
	SHRQ    $16, DX
	MOVWQZX DX, DX

	ADDQ BX, DX // DX += lowBits()

	// mlState = llTable[...]
	MOVQ decodeAsmContext_ofTable(R15), AX
	MOVQ (AX)(DX*8), ofState

skip_update:

	// mo = s.adjustOffset(mo, ll, moB)
	// prepare args for sequenceDecs_adjustOffsets
	MOVQ    s+0(FP), DI
	MOVQ    seqVals_mo(seqs), BX            // mo
	MOVQ    seqVals_ll(seqs), CX            // ll
	MOVQ    R14, DX                         // moB (from the ofState before its update)
	SHRQ    $8, DX
	MOVBQZX DL, DX
	LEAQ    sequenceDecs_prevOffset(DI), DI // DI = &s.prevOffset[0]

	// if offsetB > 1 {
	//     s.prevOffset[2] = s.prevOffset[1]
	//     s.prevOffset[1] = s.prevOffset[0]
	//     s.prevOffset[0] = offset
	//     return offset
	// }
	CMPQ DX, $1
	JBE  offsetB_1_or_0

	MOVQ 8(DI), AX
	MOVQ AX, 16(DI) // s.prevOffset[2] = s.prevOffset[1]
	MOVQ 0(DI), AX
	MOVQ AX, 8(DI)  // s.prevOffset[1] = s.prevOffset[0]
	MOVQ BX, 0(DI)  // s.prevOffset[0] = offset

	MOVQ BX, BX
	JMP  adjust_offsets_end

offsetB_1_or_0:
	// if litLen == 0 {
	//     offset++
	// }
	LEAQ    1(BX), AX // AX = offset + 1
	TESTQ   CX, CX
	CMOVQEQ AX, BX    // offset++ if litLen == 0

	// if offset == 0 {
	//     return s.prevOffset[0]
	// }
	TESTQ BX, BX
	JNZ   offset_nonzero
	MOVQ  0(DI), BX
	JMP   adjust_offsets_end

offset_nonzero:
	// Note: at this point CX (litLen) and DX (offsetB) are free

	// var temp int
	// if offset == 3 {
	//     temp = s.prevOffset[0] - 1
	// } else {
	//     temp = s.prevOffset[offset]
	// }
	//
	// this if got transformed into:
	//
	// ofs   := offset
	// shift := 0
	// if offset == 3 {
	//     ofs   = 0
	//     shift = -1
	// }
	// temp := s.prevOffset[ofs] + shift

	MOVQ    BX, CX   // ofs_other   = offset
	XORQ    DX, DX   // shift_other = 0
	XORQ    R14, R14 // ofs_3       = 0
	MOVQ    $-1, R15 // shift_3     = -1
	CMPQ    BX, $3
	CMOVQEQ R14, CX
	CMOVQEQ R15, DX

	ADDQ 0(DI)(CX*8), DX // DX is temp

	// if temp == 0 {
	//     temp = 1
	// }
	JNZ  temp_valid
	MOVQ $1, DX

temp_valid:

	// if offset != 1 {
	//     s.prevOffset[2] = s.prevOffset[1]
	// }
	CMPQ BX, $1
	JZ   skip
	MOVQ 8(DI), AX
	MOVQ AX, 16(DI) // s.prevOffset[2] = s.prevOffset[1]

skip:
	// s.prevOffset[1] = s.prevOffset[0]
	// s.prevOffset[0] = temp
	MOVQ 0(DI), AX
	MOVQ AX, 8(DI) // s.prevOffset[1] = s.prevOffset[0]
	MOVQ DX, 0(DI) // s.prevOffset[0] = temp

	// return temp
	MOVQ DX, BX

adjust_offsets_end:

check_triple:

	MOVQ BX, seqVals_mo(seqs) // BX - mo
	MOVQ seqVals_ml(seqs), CX // CX - ml
	MOVQ seqVals_ll(seqs), DX // DX - ll

	MOVQ s+0(FP), DI
	LEAQ 0(CX)(DX*1), AX
	ADDQ AX, sequenceDecs_seqSize(DI) // s.seqSize += ml + ll

	MOVQ ctx+16(FP), DI
	SUBQ DX, decodeAsmContext_litRemain(DI) // ctx.litRemain -= ll

/*
		if ml > maxMatchLen {
			return fmt.Errorf("match len (%d) bigger than max allowed length", ml)
		}
    */
	CMPQ CX, $maxMatchLen
	JA   error_match_len_too_big
/*
		if mo == 0 && ml > 0 {
			return fmt.Errorf("zero matchoff and matchlen (%d) > 0", ml)
		}
    */
	TESTQ BX, BX
	JNZ   match_len_ofs_ok             // mo != 0
	TESTQ CX, CX                       // mo == 0 && ml != 0
	JNZ   error_match_len_ofs_mismatch

match_len_ofs_ok:

	ADDQ $24, seqs // sizof(seqVals) == 3*8

	DECQ decodeAsmContext_iteration(DI)
	JNS  main_loop

	XORQ AX, AX

end:
	MOVQ 0(SP), BP
	MOVQ AX, ret+24(FP)

	// update bitreader state
	MOVQ br+8(FP), DI
	MOVQ br_value, bitReader_value(DI)
	MOVQ br_offset, bitReader_off(DI)
	MOVB br_bits_read, bitReader_bitsRead(DI)
	RET

error_match_len_too_big:
	MOVQ $errorMatchLenTooBig, AX
	JMP  end

error_match_len_ofs_mismatch:
	MOVQ $errorMatchLenOfsMismatch, AX
	JMP  end

#undef br_value
#undef br_bits_read
#undef br_offset
#undef br_pointer

#undef llState
#undef mlState
#undef ofState
