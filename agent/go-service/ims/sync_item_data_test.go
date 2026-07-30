package ims

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseOCRNumericValue(t *testing.T) {
	got, err := parseOCRNumericValue("x12")
	if err != nil || got != 12 {
		t.Fatalf("got=%d err=%v", got, err)
	}
}

func TestParseSyncItemDataParamMap(t *testing.T) {
	params, err := parseSyncItemDataParam(`{
		"items": {
			"ADVANCED_COGNITIVE_CARRIER": "ADVANCED_COGNITIVE_CARRIER"
		},
		"page_dedup": true
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if !params.PageDedup {
		t.Fatal("expected page_dedup true")
	}
	if params.Items["ADVANCED_COGNITIVE_CARRIER"] != "ADVANCED_COGNITIVE_CARRIER" {
		t.Fatalf("items=%v", params.Items)
	}
}

func TestItemDisplayNameFallback(t *testing.T) {
	if got := itemDisplayName("UNKNOWN_ITEM_XYZ"); got != "UNKNOWN_ITEM_XYZ" {
		t.Fatalf("got %q", got)
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
