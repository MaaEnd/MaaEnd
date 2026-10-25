package core

import (
	"image"
	"image/color"
	"math/rand"
	"testing"
)

// texture draws a random block pattern whose content starts offset px higher.
func texture(offset int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	rng := rand.New(rand.NewSource(7))
	blocks := make([][]uint8, 120)
	for i := range blocks {
		blocks[i] = make([]uint8, 80)
		for j := range blocks[i] {
			blocks[i][j] = uint8(rng.Intn(256))
		}
	}
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			v := blocks[(y+offset)/12][x/16]
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	return img
}

func TestScrollShift(t *testing.T) {
	for _, want := range []int{0, 40, 190, 300} {
		got, err := ScrollShift(texture(0), texture(want), ScrollArea, 340)
		if err != nil || got != want {
			t.Fatalf("shift %d: got %d, %v", want, got, err)
		}
	}
	flat := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	if _, err := ScrollShift(flat, flat, ScrollArea, 340); err == nil {
		t.Fatal("a featureless list must not yield a shift")
	}
}

func TestScrollRejectsReverseAndUnrelatedFrames(t *testing.T) {
	if n, err := ScrollShift(texture(160), texture(0), ScrollArea, 340); err == nil {
		t.Fatalf("reverse scroll accepted as %d", n)
	}
	if _, err := ScrollShift(nil, texture(0), ScrollArea, 340); err == nil {
		t.Fatal("nil frame accepted")
	}
	if _, err := ScrollShift(texture(0), texture(0), ScrollArea, -1); err == nil {
		t.Fatal("negative search range accepted")
	}
	// The same inventory-like repeated row pattern cannot locate which row
	// moved into view; multiple equally good visual peaks must be rejected.
	periodic := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	for y := 0; y < 720; y++ {
		for x := 0; x < 1280; x++ {
			v := uint8(30)
			if y%80 < 12 {
				v = 220
			}
			periodic.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	if n, err := ScrollShift(periodic, periodic, ScrollArea, 340); err == nil {
		t.Fatalf("ambiguous repeated rows accepted as %d", n)
	}
}
