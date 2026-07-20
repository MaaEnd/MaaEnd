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

func TestBuildFingerprintUsesFirstAndLastOnly(t *testing.T) {
	t.Parallel()

	hit := buildFingerprint([]ocrHit{
		{Text: "B", Box: maa.Rect{10, 200, 20, 10}},
		{Text: "A", Box: maa.Rect{10, 100, 20, 10}},
		{Text: "C-left", Box: maa.Rect{5, 300, 20, 10}},
		{Text: "C-right", Box: maa.Rect{50, 300, 20, 10}},
	})

	wantText := "A" + fingerprintSep + "C-right"
	if hit.Text != wantText {
		t.Fatalf("fingerprint = %q, want %q", hit.Text, wantText)
	}
	if hit.Box != (maa.Rect{10, 100, 20, 10}) {
		t.Fatalf("box = %v, want topmost A box", hit.Box)
	}
}

func TestBuildFingerprintSingleHit(t *testing.T) {
	t.Parallel()

	hit := buildFingerprint([]ocrHit{
		{Text: "only", Box: maa.Rect{10, 100, 20, 10}},
	})
	if hit.Text != "only" {
		t.Fatalf("fingerprint = %q, want only", hit.Text)
	}
}

func TestBuildFingerprintDetectsPartialScroll(t *testing.T) {
	t.Parallel()

	// 顶部名字不变、底部换人：旧 Best-only 会误判到底，首尾指纹应不同。
	before := buildFingerprint([]ocrHit{
		{Text: "Alpha#1001", Box: maa.Rect{135, 221, 147, 26}},
		{Text: "Bravo#2002", Box: maa.Rect{137, 328, 157, 22}},
		{Text: "Charlie#3003", Box: maa.Rect{137, 434, 84, 19}},
		{Text: "Delta#4004", Box: maa.Rect{136, 534, 161, 26}},
	})
	after := buildFingerprint([]ocrHit{
		{Text: "Alpha#1001", Box: maa.Rect{135, 221, 147, 26}},
		{Text: "Bravo#2002", Box: maa.Rect{137, 328, 157, 22}},
		{Text: "Charlie#3003", Box: maa.Rect{137, 434, 84, 19}},
		{Text: "Echo#5005", Box: maa.Rect{136, 534, 120, 26}},
	})
	if before.Text == after.Text {
		t.Fatalf("partial scroll fingerprints collided: %q", before.Text)
	}
	wantBefore := "Alpha#1001" + fingerprintSep + "Delta#4004"
	wantAfter := "Alpha#1001" + fingerprintSep + "Echo#5005"
	if before.Text != wantBefore {
		t.Fatalf("before = %q, want %q", before.Text, wantBefore)
	}
	if after.Text != wantAfter {
		t.Fatalf("after = %q, want %q", after.Text, wantAfter)
	}
}

func TestBuildFingerprintIgnoresMiddleOCRJitter(t *testing.T) {
	t.Parallel()

	stable := buildFingerprint([]ocrHit{
		{Text: "top", Box: maa.Rect{10, 100, 20, 10}},
		{Text: "mid-a", Box: maa.Rect{10, 200, 20, 10}},
		{Text: "bottom", Box: maa.Rect{10, 300, 20, 10}},
	})
	jitter := buildFingerprint([]ocrHit{
		{Text: "top", Box: maa.Rect{10, 100, 20, 10}},
		{Text: "bottom", Box: maa.Rect{10, 300, 20, 10}},
	})
	if stable.Text != jitter.Text {
		t.Fatalf("middle miss should not change first/last fingerprint: %q vs %q", stable.Text, jitter.Text)
	}
}
