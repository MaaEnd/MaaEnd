package listcomplete

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseParams(t *testing.T) {
	t.Parallel()

	p, err := parseParams(`{"node":"FooOCR"}`)
	if err != nil {
		t.Fatalf("parseParams returned error: %v", err)
	}
	if p.Node != "FooOCR" {
		t.Fatalf("node = %q, want FooOCR", p.Node)
	}

	if _, err := parseParams(`{}`); err == nil {
		t.Fatal("expected error for empty node")
	}
	if _, err := parseParams(""); err == nil {
		t.Fatal("expected error for empty param")
	}
}

func TestBuildFingerprintSortsByVerticalThenHorizontal(t *testing.T) {
	t.Parallel()

	hit := buildFingerprint([]ocrHit{
		{Text: "B", Box: maa.Rect{10, 200, 20, 10}},
		{Text: "A", Box: maa.Rect{10, 100, 20, 10}},
		{Text: "C-left", Box: maa.Rect{5, 300, 20, 10}},
		{Text: "C-right", Box: maa.Rect{50, 300, 20, 10}},
	})

	wantText := "A" + fingerprintSep + "B" + fingerprintSep + "C-left" + fingerprintSep + "C-right"
	if hit.Text != wantText {
		t.Fatalf("fingerprint = %q, want %q", hit.Text, wantText)
	}
	if hit.Box != (maa.Rect{10, 100, 20, 10}) {
		t.Fatalf("box = %v, want topmost A box", hit.Box)
	}
}

func TestBuildFingerprintDetectsPartialScroll(t *testing.T) {
	t.Parallel()

	// 顶部名字不变、底部换人：旧 Best-only 会误判到底，整屏指纹应不同。
	before := buildFingerprint([]ocrHit{
		{Text: "苏墨#0514", Box: maa.Rect{135, 221, 147, 26}},
		{Text: "daddy#8190", Box: maa.Rect{137, 328, 157, 22}},
		{Text: "Astel#1915", Box: maa.Rect{137, 434, 84, 19}},
		{Text: "心宿二#4702", Box: maa.Rect{136, 534, 161, 26}},
	})
	after := buildFingerprint([]ocrHit{
		{Text: "苏墨#0514", Box: maa.Rect{135, 221, 147, 26}},
		{Text: "daddy#8190", Box: maa.Rect{137, 328, 157, 22}},
		{Text: "Astel#1915", Box: maa.Rect{137, 434, 84, 19}},
		{Text: "乘风#9587", Box: maa.Rect{136, 534, 120, 26}},
	})
	if before.Text == after.Text {
		t.Fatalf("partial scroll fingerprints collided: %q", before.Text)
	}
	if before.Box != after.Box {
		t.Fatalf("top box should stay same when only bottom changes: before=%v after=%v", before.Box, after.Box)
	}
}
