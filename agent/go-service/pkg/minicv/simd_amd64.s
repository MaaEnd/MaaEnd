//go:build amd64 && !purego

#include "textflag.h"

DATA ·rgba3Mask<>(SB)/8, $0x00FFFFFF00FFFFFF
DATA ·rgba3Mask<>+8(SB)/8, $0x00FFFFFF00FFFFFF
GLOBL ·rgba3Mask<>(SB), RODATA, $16

TEXT ·dotRGBA3SIMD(SB), NOSPLIT, $16-32
	MOVQ imgPix+0(FP), SI
	MOVQ tplPix+8(FP), DI
	MOVQ pixels+16(FP), CX

	XORQ AX, AX
	PXOR X0, X0
	MOVOU ·rgba3Mask<>(SB), X7

chunkLoop:
	CMPQ CX, $0
	JEQ done

	// Keep each SIMD chunk within the range where the 32-bit PADDD accumulators
	// and the final 32-bit horizontal reduction cannot overflow.
	MOVQ CX, R8
	CMPQ R8, $16384
	JLE chunkReady
	MOVQ $16384, R8

chunkReady:
	MOVQ R8, R9
	PXOR X5, X5

	CMPQ R9, $4
	JLT reduceChunk

loop4:
	MOVOU (SI), X1
	MOVOU (DI), X2
	PAND X7, X1
	PAND X7, X2

	MOVOU X1, X3
	PUNPCKLBW X0, X3
	MOVOU X1, X4
	PUNPCKHBW X0, X4

	MOVOU X2, X8
	PUNPCKLBW X0, X8
	MOVOU X2, X9
	PUNPCKHBW X0, X9

	PMADDWL X8, X3
	PMADDWL X9, X4
	PADDD X3, X5
	PADDD X4, X5

	ADDQ $16, SI
	ADDQ $16, DI
	SUBQ $4, R9
	CMPQ R9, $4
	JGE loop4

reduceChunk:
	// Reduce the chunk-local SIMD accumulators to a scalar before moving on to
	// the next chunk so the running total stays in 64-bit AX.
	MOVOU X5, 0(SP)
	MOVL 0(SP), R10
	MOVL 4(SP), BX
	ADDL BX, R10
	MOVL 8(SP), BX
	ADDL BX, R10
	MOVL 12(SP), BX
	ADDL BX, R10
	MOVLQZX R10, R10
	ADDQ R10, AX

	CMPQ R9, $0
	JEQ nextChunk

tailLoop:
	MOVBLZX 0(SI), BX
	MOVBLZX 0(DI), DX
	IMULQ DX, BX
	ADDQ BX, AX

	MOVBLZX 1(SI), BX
	MOVBLZX 1(DI), DX
	IMULQ DX, BX
	ADDQ BX, AX

	MOVBLZX 2(SI), BX
	MOVBLZX 2(DI), DX
	IMULQ DX, BX
	ADDQ BX, AX

	ADDQ $4, SI
	ADDQ $4, DI
	DECQ R9
	JNZ tailLoop

nextChunk:
	SUBQ R8, CX
	JMP chunkLoop

done:
	MOVQ AX, ret+24(FP)
	RET
