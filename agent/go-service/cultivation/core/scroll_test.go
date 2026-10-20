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
		got, err := ScrollShift(texture(0), texture(want), ListArea, 340)
		if err != nil || got != want {
			t.Fatalf("shift %d: got %d, %v", want, got, err)
		}
	}
	flat := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	if _, err := ScrollShift(flat, flat, ListArea, 340); err == nil {
		t.Fatal("a featureless list must not yield a shift")
	}
}

func TestPlacedRows(t *testing.T) {
	gold := Material{ID: "item_gold", Name: "折金票", Need: 537000, Owned: 2750000}
	exp := Material{ID: "item_expcard_stage1_low", Name: "初级作战记录", Need: 3736, Owned: 27300}
	box := Material{ID: "item_char_break_stage_1_2", Name: "协议圆盘", Need: 33, Owned: 251}
	var p PlacedRows
	// Page 0, then the list moved only 57 px: the same two rows plus nothing new.
	if err := p.Add([]Material{gold, exp}, []image.Point{{950, 470}, {865, 600}}, 0); err != nil {
		t.Fatal(err)
	}
	if err := p.Add([]Material{gold, exp}, []image.Point{{950, 413}, {865, 543}}, 57); err != nil {
		t.Fatal(err)
	}
	// Next page: one known row and one new row below it.
	if err := p.Add([]Material{exp, box}, []image.Point{{865, 410}, {865, 540}}, 190); err != nil {
		t.Fatal(err)
	}
	if len(p.Rows) != 3 || p.Rows[2] != box {
		t.Fatalf("rows: %+v", p.Rows)
	}
	// The same item twice at different places is kept twice (e.g. two skills).
	if err := p.Add([]Material{gold}, []image.Point{{950, 600}}, 400); err != nil || len(p.Rows) != 4 {
		t.Fatalf("repeated item at a new place: %v %d", err, len(p.Rows))
	}
	if err := p.Add([]Material{box}, []image.Point{{950, 413}}, 57); err == nil {
		t.Fatal("a different item at a seen place must be rejected")
	}
}
