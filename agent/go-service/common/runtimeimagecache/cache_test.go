package runtimeimagecache

import (
	"image"
	"image/color"
	"testing"
)

func TestBuildImageName(t *testing.T) {
	got, err := BuildImageName("ItemTransfer", "Items/精选柑实罐头.png")
	if err != nil {
		t.Fatal(err)
	}
	const want = "__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/精选柑实罐头.png"
	if got != want {
		t.Fatalf("BuildImageName()=%q want=%q", got, want)
	}
}

func TestBuildImageNameRejectsUnsafeSegments(t *testing.T) {
	tests := []struct {
		module string
		key    string
	}{
		{module: "", key: "Items/A.png"},
		{module: "Item/Transfer", key: "Items/A.png"},
		{module: "ItemTransfer", key: ""},
		{module: "ItemTransfer", key: "../A.png"},
		{module: "ItemTransfer", key: "Items\\A.png"},
	}

	for _, tt := range tests {
		if _, err := BuildImageName(tt.module, tt.key); err == nil {
			t.Fatalf("BuildImageName(%q, %q) error=nil, want non-nil", tt.module, tt.key)
		}
	}
}

func TestEscapeKeyComponent(t *testing.T) {
	got := EscapeKeyComponent(`A/B\C%`)
	const want = `A%2FB%5CC%25`
	if got != want {
		t.Fatalf("EscapeKeyComponent()=%q want=%q", got, want)
	}
}

func TestApplyROIOffset(t *testing.T) {
	got, err := ApplyROIOffset(image.Rect(100, 200, 152, 250), [4]int{0, 0, 0, -10})
	if err != nil {
		t.Fatal(err)
	}
	want := image.Rect(100, 200, 152, 240)
	if got != want {
		t.Fatalf("ApplyROIOffset()=%v want=%v", got, want)
	}
}

func TestApplyROIOffsetRejectsEmptyRectangle(t *testing.T) {
	if _, err := ApplyROIOffset(image.Rect(0, 0, 10, 10), [4]int{0, 0, -10, 0}); err == nil {
		t.Fatal("ApplyROIOffset() error=nil, want non-nil")
	}
}

func TestCropCopiesPixelsWithoutAliasing(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 20, 20))
	src.SetRGBA(5, 6, color.RGBA{R: 10, G: 20, B: 30, A: 255})

	got, err := Crop(src, image.Rect(5, 6, 15, 16))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != image.Rect(0, 0, 10, 10) {
		t.Fatalf("Crop() bounds=%v want=%v", got.Bounds(), image.Rect(0, 0, 10, 10))
	}
	if pixel := got.RGBAAt(0, 0); pixel != (color.RGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatalf("Crop() first pixel=%v", pixel)
	}

	got.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	if pixel := src.RGBAAt(5, 6); pixel != (color.RGBA{R: 10, G: 20, B: 30, A: 255}) {
		t.Fatal("Crop() result aliases source pixels")
	}
}

func TestCropRejectsOutOfBoundsRectangle(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 20, 20))
	if _, err := Crop(src, image.Rect(15, 15, 25, 25)); err == nil {
		t.Fatal("Crop() error=nil, want non-nil")
	}
}
