package minicv

import "testing"

func dotRGBA3Reference(imgPix, tplPix []byte, pixels int) uint64 {
	var dot uint64
	off := 0
	for range pixels {
		dot += uint64(imgPix[off]) * uint64(tplPix[off])
		dot += uint64(imgPix[off+1]) * uint64(tplPix[off+1])
		dot += uint64(imgPix[off+2]) * uint64(tplPix[off+2])
		off += 4
	}
	return dot
}

func TestDotRGBA3WideRow(t *testing.T) {
	const pixels = 50000

	imgPix := make([]byte, pixels*4)
	tplPix := make([]byte, pixels*4)
	for i := range pixels {
		off := i * 4
		imgPix[off] = byte((i*17 + 11) & 0xFF)
		imgPix[off+1] = byte((i*31 + 7) & 0xFF)
		imgPix[off+2] = byte((i*47 + 3) & 0xFF)
		imgPix[off+3] = 255

		tplPix[off] = byte((i*19 + 5) & 0xFF)
		tplPix[off+1] = byte((i*23 + 13) & 0xFF)
		tplPix[off+2] = byte((i*29 + 17) & 0xFF)
		tplPix[off+3] = 255
	}

	got := dotRGBA3(&imgPix[0], &tplPix[0], pixels)
	want := dotRGBA3Reference(imgPix, tplPix, pixels)
	if got != want {
		t.Fatalf("unexpected dot product: got=%d want=%d", got, want)
	}
}
