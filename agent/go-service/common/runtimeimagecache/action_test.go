package runtimeimagecache

import (
	"image"
	"image/color"
	"testing"
)

func TestParseStoreActionParamDefaultsToContextRecognitionBox(t *testing.T) {
	got, err := parseStoreActionParam("{\"module\":\"ItemTransfer\",\"key\":\"Items/A.png\",\"roi_offset\":[0,0,0,-10]}")
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != ScopeContext {
		t.Fatalf("scope=%q want=%q", got.Scope, ScopeContext)
	}
	if got.Source != SourceRecognitionBox {
		t.Fatalf("source=%q want=%q", got.Source, SourceRecognitionBox)
	}
	if got.ROIOffset != ([4]int{0, 0, 0, -10}) {
		t.Fatalf("roi_offset=%v", got.ROIOffset)
	}
}

func TestParseStoreActionParamAcceptsResourceAbsoluteROI(t *testing.T) {
	got, err := parseStoreActionParam("{\"scope\":\"resource\",\"module\":\"Example\",\"key\":\"Panel.png\",\"source\":\"roi\",\"roi\":[10,20,30,40]}")
	if err != nil {
		t.Fatal(err)
	}
	if got.Scope != ScopeResource {
		t.Fatalf("scope=%q want=%q", got.Scope, ScopeResource)
	}
	if got.ROI != ([4]int{10, 20, 30, 40}) {
		t.Fatalf("roi=%v", got.ROI)
	}
}

func TestParseStoreActionParamRejectsInvalidConfiguration(t *testing.T) {
	params := []string{
		"{\"scope\":\"disk\",\"module\":\"A\",\"key\":\"B.png\"}",
		"{\"module\":\"A\",\"key\":\"B.png\",\"source\":\"roi\"}",
		"{\"module\":\"A\",\"key\":\"B.png\",\"source\":\"unknown\"}",
		"{\"module\":\"\",\"key\":\"B.png\"}",
	}
	for _, param := range params {
		if _, err := parseStoreActionParam(param); err == nil {
			t.Fatalf("parseStoreActionParam(%s) error=nil, want non-nil", param)
		}
	}
}

func TestStoreCallsOverrideWithCroppedImage(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 100))
	src.SetRGBA(20, 30, color.RGBA{R: 11, G: 22, B: 33, A: 255})

	var gotName string
	var gotImage image.Image
	entry, err := Store(
		"ItemTransfer",
		"Items/A.png",
		src,
		image.Rect(20, 30, 72, 80),
		[4]int{0, 0, 0, -10},
		func(name string, img image.Image) error {
			gotName = name
			gotImage = img
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/A.png" {
		t.Fatalf("override name=%q", gotName)
	}
	if gotImage.Bounds() != image.Rect(0, 0, 52, 40) {
		t.Fatalf("override image bounds=%v", gotImage.Bounds())
	}
	if entry.ImageName != gotName || entry.Rect != image.Rect(20, 30, 72, 70) {
		t.Fatalf("entry=%+v", entry)
	}
	if pixel := gotImage.At(0, 0); pixel != (color.RGBA{R: 11, G: 22, B: 33, A: 255}) {
		t.Fatalf("override first pixel=%v", pixel)
	}
}
