package ims

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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
	if params.MergeMode != syncMergeModeReplace {
		t.Fatalf("merge_mode=%q, want replace", params.MergeMode)
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

	params, err = parseSyncItemDataParam(`{
		"items": {"A": "A"},
		"merge_mode": "sum"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if params.MergeMode != syncMergeModeSum {
		t.Fatalf("merge_mode=%q, want sum", params.MergeMode)
	}
	if _, err := parseSyncItemDataParam(`{
		"items": {"A": "A"},
		"merge_mode": "append"
	}`); err == nil {
		t.Fatal("expected unsupported merge_mode to fail")
	}

	params, err = parseSyncItemDataParam(`{
		"transaction_mode": "commit"
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if params.TransactionMode != syncTransactionModeCommit {
		t.Fatalf("transaction_mode=%q, want commit", params.TransactionMode)
	}
	if _, err := parseSyncItemDataParam(`{
		"items": {"A": "A"},
		"transaction_mode": "finish"
	}`); err == nil {
		t.Fatal("expected unsupported transaction_mode to fail")
	}
}

func TestSyncItemDataSumModeUsesImmutableBaseline(t *testing.T) {
	oldItems := ItemsSnapshot()
	oldReady, oldSync := globalCache.snapshot()
	t.Cleanup(func() {
		globalCache.markSynced(oldSync, oldItems)
		if !oldReady {
			globalCache.clear()
		}
	})
	globalCache.markSynced(time.Now().UTC(), map[string]int{
		"A":     8,
		"B":     5,
		"D":     7,
		"OTHER": 9,
	})

	action := &SyncItemData{}
	firstPage := syncItemDataParam{MergeMode: syncMergeModeSum}
	merged, sumBase, err := action.prepareSyncBase(1001, firstPage, []string{"A", "B", "C", "D", "E"})
	if err != nil {
		t.Fatal(err)
	}
	if merged["A"] != 8 || merged["B"] != 5 || merged["OTHER"] != 9 {
		t.Fatalf("first-page merged=%v", merged)
	}
	if sumBase["A"] != 8 || sumBase["B"] != 5 || sumBase["C"] != 0 || sumBase["D"] != 7 || sumBase["E"] != 0 {
		t.Fatalf("sum base=%v", sumBase)
	}

	merged["A"] = mergedSyncQuantity(firstPage.MergeMode, sumBase, "A", 10)
	merged["C"] = mergedSyncQuantity(firstPage.MergeMode, sumBase, "C", 4)
	globalCache.markSynced(time.Now().UTC(), merged)

	nextPage := syncItemDataParam{MergeMode: syncMergeModeSum, PageDedup: true}
	merged, sumBase, err = action.prepareSyncBase(1001, nextPage, []string{"A", "B", "C", "D", "E"})
	if err != nil {
		t.Fatal(err)
	}
	// A is visible again on an overlapping page. The result must stay 8+10,
	// rather than adding 10 to the already merged value 18.
	merged["A"] = mergedSyncQuantity(nextPage.MergeMode, sumBase, "A", 10)
	merged["B"] = mergedSyncQuantity(nextPage.MergeMode, sumBase, "B", 6)
	if merged["A"] != 18 {
		t.Fatalf("overlapping A=%d, want 18", merged["A"])
	}
	if merged["B"] != 11 {
		t.Fatalf("B=%d, want 11", merged["B"])
	}
	if merged["C"] != 4 {
		t.Fatalf("unseen-on-next-page C=%d, want 4", merged["C"])
	}
	if merged["D"] != 7 {
		t.Fatalf("unseen-in-second-depot D=%d, want first-depot quantity 7", merged["D"])
	}
	if _, ok := merged["E"]; ok {
		t.Fatalf("missing-in-both-depots E should stay absent, got=%v", merged)
	}
	if merged["OTHER"] != 9 {
		t.Fatalf("unrelated item changed: %v", merged)
	}
}

func TestSyncItemDataSumContinuationRequiresMatchingTask(t *testing.T) {
	oldItems := ItemsSnapshot()
	oldReady, oldSync := globalCache.snapshot()
	t.Cleanup(func() {
		globalCache.markSynced(oldSync, oldItems)
		if !oldReady {
			globalCache.clear()
		}
	})
	globalCache.markSynced(time.Now().UTC(), map[string]int{"A": 8})

	action := &SyncItemData{}
	if _, _, err := action.prepareSyncBase(1001, syncItemDataParam{
		MergeMode: syncMergeModeSum,
		PageDedup: true,
	}, []string{"A"}); err == nil {
		t.Fatal("expected continuation without baseline to fail")
	}
	if _, _, err := action.prepareSyncBase(1001, syncItemDataParam{
		MergeMode: syncMergeModeSum,
	}, []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := action.prepareSyncBase(2002, syncItemDataParam{
		MergeMode: syncMergeModeSum,
		PageDedup: true,
	}, []string{"A"}); err == nil {
		t.Fatal("expected continuation from another task to fail")
	}

	if _, _, err := action.prepareSyncBase(1001, syncItemDataParam{
		MergeMode: syncMergeModeReplace,
	}, []string{"A"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := action.prepareSyncBase(1001, syncItemDataParam{
		MergeMode: syncMergeModeSum,
		PageDedup: true,
	}, []string{"A"}); err == nil {
		t.Fatal("expected replace rebuild to clear the sum baseline")
	}
}

func TestSyncItemDataTransactionStagesTwoDepotsUntilCommit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMS.json")
	oldPathFunc := recordPathFunc
	recordPathFunc = func() string { return path }
	t.Cleanup(func() {
		recordPathFunc = oldPathFunc
		ClearCache()
	})
	ClearCache()

	at := time.Now().UTC().Add(-time.Hour)
	if err := persistSynced(at, map[string]int{
		"A":     99,
		"D":     7,
		"OTHER": 9,
	}); err != nil {
		t.Fatal(err)
	}

	action := &SyncItemData{}
	scanIDs := []string{"A", "B", "C", "D", "E"}
	firstPage := syncItemDataParam{
		MergeMode:       syncMergeModeReplace,
		TransactionMode: syncTransactionModeBegin,
	}
	merged, sumBase, err := action.prepareSyncBase(1001, firstPage, scanIDs)
	if err != nil {
		t.Fatal(err)
	}
	if sumBase != nil {
		t.Fatalf("replace begin sumBase=%v, want nil", sumBase)
	}
	if len(merged) != 1 || merged["OTHER"] != 9 {
		t.Fatalf("first-depot rebuild base=%v", merged)
	}
	merged["A"] = 8
	merged["B"] = 5
	merged["D"] = 7
	if err := action.storeTransactionResult(1001, merged); err != nil {
		t.Fatal(err)
	}
	if got := ItemsSnapshot()["A"]; got != 99 {
		t.Fatalf("formal cache changed before commit: A=%d", got)
	}
	if ready, syncedAt := globalCache.snapshot(); !ready || !syncedAt.Equal(at) {
		t.Fatalf("staging changed readiness timestamp: ready=%v synced_at=%v", ready, syncedAt)
	}

	nextFirstDepotPage := syncItemDataParam{
		MergeMode:       syncMergeModeReplace,
		PageDedup:       true,
		TransactionMode: syncTransactionModeContinue,
	}
	merged, _, err = action.prepareSyncBase(1001, nextFirstDepotPage, scanIDs)
	if err != nil {
		t.Fatal(err)
	}
	merged["C"] = 2
	if err := action.storeTransactionResult(1001, merged); err != nil {
		t.Fatal(err)
	}

	secondDepotFirstPage := syncItemDataParam{
		MergeMode:       syncMergeModeSum,
		TransactionMode: syncTransactionModeContinue,
	}
	merged, sumBase, err = action.prepareSyncBase(1001, secondDepotFirstPage, scanIDs)
	if err != nil {
		t.Fatal(err)
	}
	if sumBase["A"] != 8 || sumBase["B"] != 5 || sumBase["C"] != 2 || sumBase["D"] != 7 || sumBase["E"] != 0 {
		t.Fatalf("second-depot baseline=%v", sumBase)
	}
	merged["A"] = mergedSyncQuantity(secondDepotFirstPage.MergeMode, sumBase, "A", 10)
	merged["C"] = mergedSyncQuantity(secondDepotFirstPage.MergeMode, sumBase, "C", 4)
	if err := action.storeTransactionResult(1001, merged); err != nil {
		t.Fatal(err)
	}

	secondDepotNextPage := syncItemDataParam{
		MergeMode:       syncMergeModeSum,
		PageDedup:       true,
		TransactionMode: syncTransactionModeContinue,
	}
	merged, sumBase, err = action.prepareSyncBase(1001, secondDepotNextPage, scanIDs)
	if err != nil {
		t.Fatal(err)
	}
	merged["A"] = mergedSyncQuantity(secondDepotNextPage.MergeMode, sumBase, "A", 10)
	merged["B"] = mergedSyncQuantity(secondDepotNextPage.MergeMode, sumBase, "B", 6)
	if err := action.storeTransactionResult(1001, merged); err != nil {
		t.Fatal(err)
	}
	if got := ItemsSnapshot()["A"]; got != 99 {
		t.Fatalf("formal cache changed during second depot: A=%d", got)
	}

	if !action.commitTransaction(1001) {
		t.Fatal("expected transaction commit to succeed")
	}
	committed := ItemsSnapshot()
	if committed["A"] != 18 || committed["B"] != 11 || committed["C"] != 6 || committed["D"] != 7 {
		t.Fatalf("committed two-depot quantities=%v", committed)
	}
	if _, ok := committed["E"]; ok {
		t.Fatalf("missing-in-both-depots E should stay absent, got=%v", committed)
	}
	if committed["OTHER"] != 9 {
		t.Fatalf("unrelated item changed: %v", committed)
	}
	if ready, syncedAt := globalCache.snapshot(); !ready || !syncedAt.After(at) {
		t.Fatalf("commit did not refresh readiness timestamp: ready=%v synced_at=%v", ready, syncedAt)
	}
	if action.transactionItems != nil || action.transactionTaskID != 0 || action.transactionSumBase != nil {
		t.Fatal("successful commit should clear transaction state")
	}
}

func TestSyncItemDataTransactionRejectsContinuationAndDiscardsInterruptedStage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "IMS.json")
	oldPathFunc := recordPathFunc
	recordPathFunc = func() string { return path }
	t.Cleanup(func() {
		recordPathFunc = oldPathFunc
		ClearCache()
	})
	ClearCache()

	if err := persistSynced(time.Now().UTC(), map[string]int{"A": 50}); err != nil {
		t.Fatal(err)
	}
	action := &SyncItemData{}
	continueParams := syncItemDataParam{
		MergeMode:       syncMergeModeReplace,
		PageDedup:       true,
		TransactionMode: syncTransactionModeContinue,
	}
	if _, _, err := action.prepareSyncBase(1001, continueParams, []string{"A"}); err == nil {
		t.Fatal("expected continuation without staging snapshot to fail")
	}

	beginParams := syncItemDataParam{
		MergeMode:       syncMergeModeReplace,
		TransactionMode: syncTransactionModeBegin,
	}
	merged, _, err := action.prepareSyncBase(1001, beginParams, []string{"A"})
	if err != nil {
		t.Fatal(err)
	}
	merged["A"] = 1
	if err := action.storeTransactionResult(1001, merged); err != nil {
		t.Fatal(err)
	}
	if got := ItemsSnapshot()["A"]; got != 50 {
		t.Fatalf("interrupted stage changed formal cache: A=%d", got)
	}

	merged, _, err = action.prepareSyncBase(2002, beginParams, []string{"A"})
	if err != nil {
		t.Fatal(err)
	}
	merged["A"] = 2
	if err := action.storeTransactionResult(2002, merged); err != nil {
		t.Fatal(err)
	}
	if action.commitTransaction(1001) {
		t.Fatal("stale task should not commit a replacement transaction")
	}
	if !action.commitTransaction(2002) {
		t.Fatal("replacement transaction should commit")
	}
	if got := ItemsSnapshot()["A"]; got != 2 {
		t.Fatalf("committed A=%d, want 2", got)
	}
}

func TestSyncItemDataTransactionCommitFailureKeepsStageAndFormalCache(t *testing.T) {
	dir := t.TempDir()
	validPath := filepath.Join(dir, "IMS.json")
	oldPathFunc := recordPathFunc
	recordPathFunc = func() string { return validPath }
	t.Cleanup(func() {
		recordPathFunc = oldPathFunc
		ClearCache()
	})
	ClearCache()

	if err := persistSynced(time.Now().UTC(), map[string]int{"A": 50}); err != nil {
		t.Fatal(err)
	}
	action := &SyncItemData{}
	params := syncItemDataParam{
		MergeMode:       syncMergeModeReplace,
		TransactionMode: syncTransactionModeBegin,
	}
	merged, _, err := action.prepareSyncBase(1001, params, []string{"A"})
	if err != nil {
		t.Fatal(err)
	}
	merged["A"] = 7
	if err := action.storeTransactionResult(1001, merged); err != nil {
		t.Fatal(err)
	}

	recordPathFunc = func() string { return dir }
	if action.commitTransaction(1001) {
		t.Fatal("commit to a directory path should fail")
	}
	if got := ItemsSnapshot()["A"]; got != 50 {
		t.Fatalf("failed commit changed formal cache: A=%d", got)
	}
	if action.transactionItems == nil || action.transactionItems["A"] != 7 {
		t.Fatalf("failed commit discarded staging: %v", action.transactionItems)
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

	if err := persistSynced(at, map[string]int{"A": 1, "OTHER": 9}); err != nil {
		t.Fatal(err)
	}
	regionBase, err := baseItemsForSync(false, []string{"A"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := regionBase["A"]; ok {
		t.Fatalf("region rebuild should drop scanned miss keys, got=%v", regionBase)
	}
	if regionBase["OTHER"] != 9 {
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
