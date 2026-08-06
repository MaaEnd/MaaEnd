package ims

import (
	"image"
	"image/color"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseAddItemDataParam(t *testing.T) {
	params, err := parseAddItemDataParam("")
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Items) != 0 {
		t.Fatalf("empty param should yield empty items, got %v", params.Items)
	}
	params, err = parseAddItemDataParam(`{
		"items": {
			"PROTODISK": "PROTODISK",
			"CAST_DIE": "CAST_DIE"
		}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if params.Items["PROTODISK"] != "PROTODISK" || params.Items["CAST_DIE"] != "CAST_DIE" {
		t.Fatalf("items=%v", params.Items)
	}
	if !params.maskHitRegionEnabled() {
		t.Fatal("mask_hit_region should default to true when omitted")
	}
}

func TestParseAddItemDataParamMaskHitRegion(t *testing.T) {
	params, err := parseAddItemDataParam(`{
		"items": {"PROTODISK": "PROTODISK"},
		"mask_hit_region": false
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if params.maskHitRegionEnabled() {
		t.Fatal("mask_hit_region=false should disable masking")
	}

	params, err = parseAddItemDataParam(`{
		"items": {"PROTODISK": "PROTODISK"},
		"mask_hit_region": true
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if !params.maskHitRegionEnabled() {
		t.Fatal("mask_hit_region=true should enable masking")
	}
}

func TestAddItemDataNeedsContextWhenNotInitialized(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	a := &AddItemData{}
	arg := &maa.CustomActionArg{
		CustomActionParam: `{"items":{"PROTODISK":"PROTODISK"}}`,
	}
	// Uninitialized cache still runs recognition; nil context cannot capture image.
	if a.Run(nil, arg) {
		t.Fatal("expected failure without context when recognition is required")
	}
	if got := globalCache.quantity("PROTODISK"); got != 0 {
		t.Fatalf("quantity=%d, want 0", got)
	}
}

func TestUnionRects(t *testing.T) {
	got := unionRects(maa.Rect{10, 20, 30, 40}, maa.Rect{25, 10, 20, 15})
	want := maa.Rect{10, 10, 35, 50}
	if got != want {
		t.Fatalf("union=%v, want %v", got, want)
	}
	if unionRects(maa.Rect{0, 0, 0, 0}, maa.Rect{1, 1, 0, 5}) != (maa.Rect{}) {
		t.Fatal("expected empty union for invalid rects")
	}
}

func TestItemHitMaskBox(t *testing.T) {
	detail := &maa.RecognitionDetail{
		Box: maa.Rect{0, 0, 1, 1},
		CombinedResult: []*maa.RecognitionDetail{
			{Box: maa.Rect{100, 200, 50, 10}},
			{Box: maa.Rect{100, 100, 50, 80}},
		},
	}
	got := itemHitMaskBox(detail)
	want := maa.Rect{100, 100, 50, 110}
	if got != want {
		t.Fatalf("mask box=%v, want %v", got, want)
	}

	fallback := &maa.RecognitionDetail{Box: maa.Rect{7, 8, 9, 10}}
	if itemHitMaskBox(fallback) != fallback.Box {
		t.Fatal("expected detail.Box fallback when CombinedResult empty")
	}
}

func TestPaintItemHitRegion(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	detail := &maa.RecognitionDetail{
		CombinedResult: []*maa.RecognitionDetail{
			{Box: maa.Rect{10, 20, 30, 10}},
			{Box: maa.Rect{10, 5, 30, 20}},
		},
	}
	if !paintItemHitRegion(img, detail) {
		t.Fatal("expected paint success")
	}
	c := img.RGBAAt(15, 10)
	if c != (color.RGBA{R: 0, G: 255, B: 0, A: 255}) {
		t.Fatalf("pixel=%v, want green", c)
	}
}
