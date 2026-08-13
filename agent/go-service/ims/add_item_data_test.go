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

func TestFindRecognitionDetailByAlgorithmDoesNotDependOnChildOrder(t *testing.T) {
	templateDetail := &maa.RecognitionDetail{
		Algorithm: "TemplateMatch",
		Box:       maa.Rect{700, 280, 120, 100},
	}
	detail := &maa.RecognitionDetail{
		Algorithm: "And",
		CombinedResult: []*maa.RecognitionDetail{
			{Algorithm: "ColorMatch", Box: maa.Rect{400, 390, 100, 5}},
			{
				Algorithm: "And",
				CombinedResult: []*maa.RecognitionDetail{
					{Algorithm: "OCR", Box: maa.Rect{800, 370, 20, 15}},
					templateDetail,
				},
			},
		},
	}

	got := findRecognitionDetailByAlgorithm(detail, "TemplateMatch")
	if got != templateDetail {
		t.Fatalf("template detail=%p, want %p", got, templateDetail)
	}
}

func TestBestTemplateMatchBoxRequiresTypedBestResult(t *testing.T) {
	detail := &maa.RecognitionDetail{
		Algorithm: "And",
		Box:       maa.Rect{806, 377, 20, 15},
		CombinedResult: []*maa.RecognitionDetail{
			{Algorithm: "ColorMatch", Box: maa.Rect{416, 392, 96, 3}},
			{
				Algorithm: "TemplateMatch",
				Box:       maa.Rect{768, 299, 96, 79},
			},
			{Algorithm: "OCR", Box: maa.Rect{806, 377, 20, 15}},
		},
	}

	if box, ok := bestTemplateMatchBox(detail); ok || box != (maa.Rect{}) {
		t.Fatalf("mask box=%v, ok=%v; want no fallback to detail.Box", box, ok)
	}
}

func TestPaintItemHitRegionDoesNotUseRecognitionDetailBox(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1280, 720))
	detail := &maa.RecognitionDetail{
		Algorithm: "And",
		CombinedResult: []*maa.RecognitionDetail{
			{Algorithm: "ColorMatch", Box: maa.Rect{416, 392, 96, 3}},
			{Algorithm: "TemplateMatch", Box: maa.Rect{768, 299, 96, 79}},
			{Algorithm: "OCR", Box: maa.Rect{806, 377, 20, 15}},
		},
	}

	if paintItemHitRegion(img, detail) {
		t.Fatal("expected masking to fail without TemplateMatch Results.Best")
	}
	if got := img.RGBAAt(800, 320); got != (color.RGBA{}) {
		t.Fatalf("template detail.Box pixel=%v, want untouched", got)
	}
	if got := img.RGBAAt(450, 393); got != (color.RGBA{}) {
		t.Fatalf("quality anchor pixel=%v, want untouched", got)
	}
}
