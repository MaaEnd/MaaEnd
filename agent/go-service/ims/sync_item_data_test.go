package ims

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveQuantityNodeNameFromJSON(t *testing.T) {
	raw := []byte(`{
		"recognition": {
			"type": "And",
			"param": {
				"all_of": ["IconNode", "QtyNode"],
				"box_index": 1
			}
		}
	}`)
	got, err := resolveQuantityNodeNameFromJSON("ItemAnd", raw)
	if err != nil {
		t.Fatal(err)
	}
	if got != "QtyNode" {
		t.Fatalf("got %q, want QtyNode", got)
	}
}

func TestResolveQuantityNodeNameRejectsInline(t *testing.T) {
	raw := []byte(`{
		"recognition": {
			"type": "And",
			"param": {
				"all_of": ["IconNode", {"type":"OCR","roi":[0,0,1,1]}],
				"box_index": 1
			}
		}
	}`)
	if _, err := resolveQuantityNodeNameFromJSON("ItemAnd", raw); err == nil {
		t.Fatal("expected error for inline box_index target")
	}
}

func TestParseOCRNumericValue(t *testing.T) {
	got, err := parseOCRNumericValue("x12")
	if err != nil || got != 12 {
		t.Fatalf("got=%d err=%v", got, err)
	}
}

func TestPersistSyncedAndPageDedupBase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMS.json")
	oldPathFunc := recordPathFunc
	recordPathFunc = func() string { return path }
	t.Cleanup(func() {
		recordPathFunc = oldPathFunc
		ClearCache()
	})
	ClearCache()

	at := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	if err := persistSynced(at, map[string]int{"A": 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if ItemsSnapshot()["A"] != 1 {
		t.Fatalf("cache A=%d", ItemsSnapshot()["A"])
	}

	base, err := baseItemsForSync(true)
	if err != nil {
		t.Fatal(err)
	}
	if base["A"] != 1 {
		t.Fatalf("page_dedup base=%v", base)
	}
	createBase, err := baseItemsForSync(false)
	if err != nil || len(createBase) != 0 {
		t.Fatalf("create base=%v err=%v", createBase, err)
	}
}
