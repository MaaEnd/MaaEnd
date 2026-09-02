package ims

import (
	"os"
	"testing"
	"time"
)

func TestItemQuantityAndHasData(t *testing.T) {
	_ = withTempRecord(t)

	if ItemQuantity("item_diamond") != 0 {
		t.Fatal("missing item should be 0")
	}
	if HasData() {
		t.Fatal("empty cache should not be ready")
	}

	at := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	MarkSynced(at, map[string]int{
		"item_diamond":            40,
		"item_originium_recharge": 1500,
	})
	if !HasData() {
		t.Fatal("expected hasData after MarkSynced")
	}
	if got := ItemQuantity("item_diamond"); got != 40 {
		t.Fatalf("item_diamond=%d, want 40", got)
	}
	if got := ItemQuantity("  item_originium_recharge  "); got != 1500 {
		t.Fatalf("trimmed originium=%d, want 1500", got)
	}
	if got := ItemQuantity("item_gold"); got != 0 {
		t.Fatalf("uncached item_gold=%d, want 0", got)
	}
	snap := ItemsSnapshot()
	if snap["item_diamond"] != 40 || snap["item_originium_recharge"] != 1500 {
		t.Fatalf("snapshot=%v", snap)
	}
}

func TestItemQuantityHydratesFromDisk(t *testing.T) {
	path := withTempRecord(t)

	at := time.Now().UTC().Add(-time.Hour)
	if err := persistSynced(at, map[string]int{"item_diamond": 7}); err != nil {
		t.Fatal(err)
	}

	simulateRestart()
	if got := ItemQuantity("item_diamond"); got != 7 {
		t.Fatalf("after restart item_diamond=%d, want 7", got)
	}
	if !HasData() {
		t.Fatal("expected hasData after hydrate")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := ItemQuantity("item_diamond"); got != 7 {
		t.Fatalf("memory should stay after disk removed, got=%d", got)
	}
}

func TestEnsureHydratedAfterClearCacheDoesNotReloadDisk(t *testing.T) {
	_ = withTempRecord(t)

	at := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	if err := persistSynced(at, map[string]int{"item_diamond": 9}); err != nil {
		t.Fatal(err)
	}
	ClearCache()

	if err := EnsureHydrated(); err != nil {
		t.Fatal(err)
	}
	if HasData() {
		t.Fatal("ClearCache should keep empty memory without reloading disk")
	}
	if got := ItemQuantity("item_diamond"); got != 0 {
		t.Fatalf("item_diamond=%d, want 0 after ClearCache", got)
	}
}
