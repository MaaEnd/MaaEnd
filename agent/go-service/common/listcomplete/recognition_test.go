package listcomplete

import (
	"encoding/json"
	"fmt"
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
	if p.Retry != 0 {
		t.Fatalf("retry = %d, want 0 (default)", p.Retry)
	}

	p, err = parseParams(`{"node":"FooOCR","retry":2}`)
	if err != nil {
		t.Fatalf("parseParams with retry returned error: %v", err)
	}
	if p.Retry != 2 {
		t.Fatalf("retry = %d, want 2", p.Retry)
	}

	if _, err := parseParams(`{"node":"FooOCR","retry":-1}`); err == nil {
		t.Fatal("expected error for negative retry")
	}
	if _, err := parseParams(`{}`); err == nil {
		t.Fatal("expected error for empty node")
	}
	if _, err := parseParams(""); err == nil {
		t.Fatal("expected error for empty param")
	}
}

func TestApplyFingerprintRetryBoundary(t *testing.T) {
	t.Parallel()

	const (
		currentNode = "VisitFriendsMenuScanScrollList"
		targetNode  = "VisitFriendsRecognitionItemWithNameForScroll"
		fingerprint = "Alpha#1001" + fingerprintSep + "Bravo#2002"
		retry       = 1
	)

	store := newFakeNodeStore()

	// (1) 首次：attach 为空 → hit，first_run，unchanged_count=0
	first, err := applyFingerprint(store, currentNode, targetNode, fingerprint, retry)
	if err != nil {
		t.Fatalf("first applyFingerprint: %v", err)
	}
	if !first.Hit {
		t.Fatal("first call should hit")
	}
	if first.UnchangedRetry {
		t.Fatal("first call should not be unchanged retry")
	}
	if first.Next.UnchangedCount != 0 {
		t.Fatalf("first unchanged_count = %d, want 0", first.Next.UnchangedCount)
	}
	if first.Detail["first_run"] != true {
		t.Fatalf("first_run = %v, want true", first.Detail["first_run"])
	}
	if first.Detail["retry"] != retry {
		t.Fatalf("retry in detail = %v, want %d", first.Detail["retry"], retry)
	}
	if first.Detail["unchanged_count"] != 0 {
		t.Fatalf("detail unchanged_count = %v, want 0", first.Detail["unchanged_count"])
	}

	// (2) 相同指纹且 unchanged_count <= retry → 仍 hit，unchanged_count 递增
	second, err := applyFingerprint(store, currentNode, targetNode, fingerprint, retry)
	if err != nil {
		t.Fatalf("second applyFingerprint: %v", err)
	}
	if !second.Hit {
		t.Fatal("second call should still hit when unchanged_count <= retry")
	}
	if !second.UnchangedRetry {
		t.Fatal("second call should mark unchanged_retry")
	}
	if second.Next.UnchangedCount != 1 {
		t.Fatalf("second unchanged_count = %d, want 1", second.Next.UnchangedCount)
	}
	if second.Detail["unchanged_retry"] != true {
		t.Fatalf("unchanged_retry = %v, want true", second.Detail["unchanged_retry"])
	}
	if second.Detail["unchanged_count"] != 1 {
		t.Fatalf("detail unchanged_count = %v, want 1", second.Detail["unchanged_count"])
	}
	if second.Detail["retry"] != retry {
		t.Fatalf("retry in detail = %v, want %d", second.Detail["retry"], retry)
	}

	// (3) unchanged_count > retry → miss（列表完成）
	third, err := applyFingerprint(store, currentNode, targetNode, fingerprint, retry)
	if err != nil {
		t.Fatalf("third applyFingerprint: %v", err)
	}
	if third.Hit {
		t.Fatal("third call should miss when unchanged_count > retry")
	}
	if third.Next.UnchangedCount != 2 {
		t.Fatalf("third unchanged_count = %d, want 2", third.Next.UnchangedCount)
	}
	if third.Detail != nil {
		t.Fatalf("complete outcome detail = %v, want nil", third.Detail)
	}

	state, err := loadAttachState(store, currentNode)
	if err != nil {
		t.Fatalf("loadAttachState after complete: %v", err)
	}
	if state.LastText != fingerprint {
		t.Fatalf("persisted last_text = %q, want %q", state.LastText, fingerprint)
	}
	if state.UnchangedCount != 2 {
		t.Fatalf("persisted unchanged_count = %d, want 2", state.UnchangedCount)
	}
}

func TestApplyFingerprintRetryZeroCompletesImmediately(t *testing.T) {
	t.Parallel()

	const (
		currentNode = "ScrollList"
		targetNode  = "NameOCR"
		fingerprint = "A" + fingerprintSep + "B"
	)

	store := newFakeNodeStore()
	first, err := applyFingerprint(store, currentNode, targetNode, fingerprint, 0)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if !first.Hit {
		t.Fatal("first call should hit")
	}

	second, err := applyFingerprint(store, currentNode, targetNode, fingerprint, 0)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Hit {
		t.Fatal("retry=0 should complete on first unchanged hit")
	}
	if second.Next.UnchangedCount != 1 {
		t.Fatalf("unchanged_count = %d, want 1", second.Next.UnchangedCount)
	}
}

func TestEvaluateListCompleteResetsCountOnFingerprintChange(t *testing.T) {
	t.Parallel()

	outcome := evaluateListComplete(
		attachState{LastText: "old", UnchangedCount: 3},
		"new",
		"ScrollList",
		"NameOCR",
		2,
	)
	if !outcome.Hit || outcome.UnchangedRetry {
		t.Fatalf("changed fingerprint should accept: hit=%v unchanged_retry=%v", outcome.Hit, outcome.UnchangedRetry)
	}
	if outcome.Next.UnchangedCount != 0 || outcome.Next.LastText != "new" {
		t.Fatalf("next state = %+v, want last_text=new unchanged_count=0", outcome.Next)
	}
	if outcome.Detail["first_run"] != false {
		t.Fatalf("first_run = %v, want false", outcome.Detail["first_run"])
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

type fakeNodeStore struct {
	nodes map[string]map[string]any
}

func newFakeNodeStore() *fakeNodeStore {
	return &fakeNodeStore{nodes: make(map[string]map[string]any)}
}

func (f *fakeNodeStore) GetNodeJSON(nodeName string) (string, error) {
	node, ok := f.nodes[nodeName]
	if !ok {
		return `{}`, nil
	}
	raw, err := json.Marshal(node)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (f *fakeNodeStore) OverridePipeline(pipelineOverride any) error {
	overrideMap, ok := pipelineOverride.(map[string]any)
	if !ok {
		return fmt.Errorf("pipeline override must be map[string]any, got %T", pipelineOverride)
	}
	for name, rawPatch := range overrideMap {
		patch, ok := rawPatch.(map[string]any)
		if !ok {
			return fmt.Errorf("override for %s must be object", name)
		}
		node := f.nodes[name]
		if node == nil {
			node = make(map[string]any)
			f.nodes[name] = node
		}
		for key, value := range patch {
			node[key] = value
		}
	}
	return nil
}
