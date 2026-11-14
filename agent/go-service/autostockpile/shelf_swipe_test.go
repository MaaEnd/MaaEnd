package autostockpile

import (
	"image"
	"image/color"
	"testing"
)

func TestSamePageGoods(t *testing.T) {
	goods := func(ids ...string) []GoodsItem {
		items := make([]GoodsItem, 0, len(ids))
		for _, id := range ids {
			items = append(items, GoodsItem{ID: id, Name: id, Tier: "ValleyIV.Tier1", Price: 100})
		}
		return items
	}

	tests := []struct {
		name     string
		previous []GoodsItem
		current  []GoodsItem
		want     bool
	}{
		{
			name:     "same ids in different order",
			previous: goods("a", "b", "c"),
			current:  goods("c", "a", "b"),
			want:     true,
		},
		{
			name:     "both empty",
			previous: nil,
			current:  goods(),
			want:     true,
		},
		{
			name:     "new goods appeared after scrolling",
			previous: goods("a", "b", "c"),
			current:  goods("b", "c", "d"),
			want:     false,
		},
		{
			name:     "fewer goods recognized on current page",
			previous: goods("a", "b", "c"),
			current:  goods("a", "b"),
			want:     false,
		},
		{
			name:     "same size but different ids",
			previous: goods("a", "b", "c"),
			current:  goods("a", "b", "d"),
			want:     false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := samePageGoods(test.previous, test.current); got != test.want {
				t.Fatalf("samePageGoods() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestShelfFramesLookIdentical(t *testing.T) {
	newFrame := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
		for y := 0; y < 720; y++ {
			for x := 0; x < 1280; x++ {
				img.Set(x, y, color.RGBA{R: 120, G: 120, B: 120, A: 255})
			}
		}
		return img
	}

	if shelfFramesLookIdentical(nil, newFrame()) {
		t.Fatal("nil frame must not be treated as identical")
	}

	previous := newFrame()
	if !shelfFramesLookIdentical(previous, newFrame()) {
		t.Fatal("identical frames must be detected as identical")
	}

	// 只改商品区域内的一个采样点，模拟画面真的发生变化。
	moved := newFrame()
	moved.Set(63+shelfFrameCompareStep*5, 162+shelfFrameCompareStep*5, color.RGBA{R: 200, A: 255})
	if shelfFramesLookIdentical(previous, moved) {
		t.Fatal("different frames must not be detected as identical")
	}
}
