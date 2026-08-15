package ims

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseSyncItemDataParamMap(t *testing.T) {
	params, err := parseSyncItemDataParam(`{
		"items": {
			"item_expcard_stage2_high": "item_expcard_stage2_high"
		},
		"page_dedup": true
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if !params.PageDedup {
		t.Fatal("expected page_dedup true")
	}
	if params.Items["item_expcard_stage2_high"] != "item_expcard_stage2_high" {
		t.Fatalf("items=%v", params.Items)
	}
	if params.NotifyUI != nil {
		t.Fatal("omitted notify_ui should leave pointer nil (default true at resolve)")
	}
	if !resolveSyncNotifyUI(params.NotifyUI) {
		t.Fatal("notify_ui omitted should default true")
	}

	params, err = parseSyncItemDataParam(`{
		"items": {"A": "A"},
		"notify_ui": false
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if params.NotifyUI == nil || *params.NotifyUI {
		t.Fatal("expected notify_ui false")
	}
	if resolveSyncNotifyUI(params.NotifyUI) {
		t.Fatal("notify_ui=false should disable")
	}

	params, err = parseSyncItemDataParam(`{
		"items": {"  item_weapon_break_low  ": "  item_weapon_break_low  "}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Items) != 1 || params.Items["item_weapon_break_low"] != "item_weapon_break_low" {
		t.Fatalf("expected trimmed items, got %v", params.Items)
	}

	if _, err := parseSyncItemDataParam(`{
		"items": {"A": "A", " A ": "B"}
	}`); err == nil {
		t.Fatal("expected duplicate after trim to fail")
	}
	if _, err := parseSyncItemDataParam(`{
		"items": {"  ": "NODE"}
	}`); err == nil {
		t.Fatal("expected empty id after trim to fail")
	}
}

func TestItemDisplayNameFallback(t *testing.T) {
	if got := iconqty.ItemDisplayName("UNKNOWN_ITEM_XYZ"); got != "UNKNOWN_ITEM_XYZ" {
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

	base, err := baseItemsForSync(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if base["A"] != 1 {
		t.Fatalf("page_dedup base=%v", base)
	}
	createBase, err := baseItemsForSync(false, []string{"A"})
	if err != nil {
		t.Fatal(err)
	}
	if len(createBase) != 0 {
		t.Fatalf("create base for scanned keys should drop A, got=%v", createBase)
	}

	if err := persistSynced(at, map[string]int{"A": 1, "ORIGEOMETRY": 9}); err != nil {
		t.Fatal(err)
	}
	regionBase, err := baseItemsForSync(false, []string{"A"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := regionBase["A"]; ok {
		t.Fatalf("region rebuild should drop scanned miss keys, got=%v", regionBase)
	}
	if regionBase["ORIGEOMETRY"] != 9 {
		t.Fatalf("region rebuild should keep other IDs, got=%v", regionBase)
	}
}

func TestLazyHydrateFromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMS.json")
	oldPathFunc := recordPathFunc
	recordPathFunc = func() string { return path }
	t.Cleanup(func() {
		recordPathFunc = oldPathFunc
		ClearCache()
	})
	ClearCache()

	// Keep within refresh_days so ItemDataReady is not stale-dependent on wall clock.
	at := time.Now().UTC().Add(-24 * time.Hour)
	if err := persistSynced(at, map[string]int{"item_char_break_stage_1_2": 7, "item_weapon_break_low": 3}); err != nil {
		t.Fatal(err)
	}

	// Simulate process restart: memory empty, hydrate allowed again.
	simulateRestart()

	ready := &ItemDataReady{}
	readyArg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"refresh_days":7}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := ready.Run(nil, readyArg); !ok {
		t.Fatal("expected ready after hydrate from disk")
	}

	qty := &ItemQuantitySatisfied{}
	qtyArg := &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_char_break_stage_1_2}>=7"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}
	if _, ok := qty.Run(nil, qtyArg); !ok {
		t.Fatal("expected quantity hit after hydrate")
	}
	if got := ItemsSnapshot()["item_weapon_break_low"]; got != 3 {
		t.Fatalf("item_weapon_break_low=%d, want 3", got)
	}

	// Second access must not depend on re-reading a deleted disk file.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := ready.Run(nil, readyArg); !ok {
		t.Fatal("expected ready from memory after disk removed")
	}
}

func TestClearCacheDoesNotReloadDisk(t *testing.T) {
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
	if err := persistSynced(at, map[string]int{"item_char_break_stage_1_2": 9}); err != nil {
		t.Fatal(err)
	}
	ClearCache()

	ready := &ItemDataReady{}
	if _, ok := ready.Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"refresh_days":7}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}); ok {
		t.Fatal("ClearCache should keep empty memory without reloading disk")
	}
}

// simulateRestart clears memory and allows the next ensureHydrated to reload disk.
func simulateRestart() {
	recordMu.Lock()
	defer recordMu.Unlock()
	globalCache.clear()
	hydrated = false
}
