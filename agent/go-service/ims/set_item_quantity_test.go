package ims

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// withRecordPath points IMS.json at a temp file and resets in-process state, so
// each test starts from an empty, non-hydrated cache.
func withRecordPath(t *testing.T, path string) {
	t.Helper()
	previous := recordPathFunc
	recordPathFunc = func() string { return path }
	resetIMSStateForTest()
	t.Cleanup(func() {
		recordPathFunc = previous
		resetIMSStateForTest()
	})
}

func resetIMSStateForTest() {
	recordMu.Lock()
	hydrated = false
	recordMu.Unlock()
	globalCache.clear()
}

func runSetItemQuantity(t *testing.T, param string) bool {
	t.Helper()
	return (&SetItemQuantity{}).Run(nil, &maa.CustomActionArg{CustomActionParam: param})
}

func TestSetItemQuantityWritesLiteral(t *testing.T) {
	withRecordPath(t, filepath.Join(t.TempDir(), "IMS.json"))

	if !runSetItemQuantity(t, `{"item":"item_x","value":40}`) {
		t.Fatal("literal set should succeed")
	}
	if got := ItemsSnapshot()["item_x"]; got != 40 {
		t.Fatalf("item_x = %d, want 40", got)
	}
}

func TestSetItemQuantityExpressionReadsCache(t *testing.T) {
	withRecordPath(t, filepath.Join(t.TempDir(), "IMS.json"))
	globalCache.setAbsolute("item_a", 5)

	if !runSetItemQuantity(t, `{"item":"item_x","value":"{item_a}*2+1"}`) {
		t.Fatal("expression set should succeed")
	}
	if got := ItemsSnapshot()["item_x"]; got != 11 {
		t.Fatalf("item_x = %d, want 11", got)
	}
}

func TestSetItemQuantityExpressionRejectsBoolean(t *testing.T) {
	withRecordPath(t, filepath.Join(t.TempDir(), "IMS.json"))

	if runSetItemQuantity(t, `{"item":"item_x","value":"1>0"}`) {
		t.Fatal("boolean expression must fail")
	}
	if _, ok := ItemsSnapshot()["item_x"]; ok {
		t.Fatal("failed set must not write the item")
	}
}

func TestSetItemQuantityClampsNegative(t *testing.T) {
	withRecordPath(t, filepath.Join(t.TempDir(), "IMS.json"))
	globalCache.setAbsolute("item_x", 7)

	if !runSetItemQuantity(t, `{"item":"item_x","value":-5}`) {
		t.Fatal("negative set should still succeed")
	}
	if got := ItemsSnapshot()["item_x"]; got != 0 {
		t.Fatalf("item_x = %d, want clamped 0", got)
	}
}

func TestSetItemQuantityRejectsInvalidParams(t *testing.T) {
	withRecordPath(t, filepath.Join(t.TempDir(), "IMS.json"))

	for _, param := range []string{
		``,
		`{}`,
		`{"item":""}`,
		`{"item":"   "}`,
		`{"item":"item_x","value":1.5}`,
		`{"item":"item_x","value":""}`,
		`{"item":"item_x","value":"{unclosed"}`,
		`not json`,
	} {
		if runSetItemQuantity(t, param) {
			t.Fatalf("param %s should be rejected", param)
		}
	}
	if got := ItemsSnapshot(); len(got) != 0 {
		t.Fatalf("rejected params must not write cache, got %v", got)
	}
}

func TestSetItemQuantityWithoutValueNeedsRecognitionDetail(t *testing.T) {
	withRecordPath(t, filepath.Join(t.TempDir(), "IMS.json"))

	if runSetItemQuantity(t, `{"item":"item_x"}`) {
		t.Fatal("missing value and missing recognition detail must fail")
	}
	if _, ok := ItemsSnapshot()["item_x"]; ok {
		t.Fatal("failed set must not write the item")
	}
}

func TestSetItemQuantityWithoutValueRejectsNonOCRDetail(t *testing.T) {
	withRecordPath(t, filepath.Join(t.TempDir(), "IMS.json"))

	ok := (&SetItemQuantity{}).Run(nil, &maa.CustomActionArg{
		CustomActionParam: `{"item":"item_x"}`,
		RecognitionDetail: &maa.RecognitionDetail{Hit: true, Algorithm: "DirectHit"},
	})
	if ok {
		t.Fatal("a detail without an OCR result must fail")
	}
	if _, exists := ItemsSnapshot()["item_x"]; exists {
		t.Fatal("failed set must not write the item")
	}
}

func TestSetItemQuantityDoesNotPersistOrMarkReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "IMS.json")
	withRecordPath(t, path)

	if !runSetItemQuantity(t, `{"item":"item_x","value":9}`) {
		t.Fatal("set should succeed")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("A4 must not write IMS.json, stat err = %v", err)
	}
	if hasData, _ := globalCache.snapshot(); hasData {
		t.Fatal("A4 must not mark the cache ready")
	}

	// Simulated restart: the value exists only in memory, so a fresh hydrate drops it.
	resetIMSStateForTest()
	if err := ensureHydrated(); err != nil {
		t.Fatalf("hydrate failed: %v", err)
	}
	if _, ok := ItemsSnapshot()["item_x"]; ok {
		t.Fatal("A4 value must not survive a restart")
	}
}

func TestSetItemQuantityKeepsSyncTimestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "IMS.json")
	withRecordPath(t, path)

	syncedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	if err := saveRecord(recordFile{UpdatedAt: syncedAt, Items: map[string]int{"item_a": 3}}); err != nil {
		t.Fatalf("save record: %v", err)
	}
	resetIMSStateForTest()
	if err := ensureHydrated(); err != nil {
		t.Fatalf("hydrate failed: %v", err)
	}

	if !runSetItemQuantity(t, `{"item":"item_x","value":9}`) {
		t.Fatal("set should succeed")
	}

	hasData, lastSync := globalCache.snapshot()
	if !hasData || !lastSync.Equal(syncedAt) {
		t.Fatalf("readiness changed: hasData=%v lastSync=%v", hasData, lastSync)
	}

	record, err := loadRecord()
	if err != nil {
		t.Fatalf("load record: %v", err)
	}
	if !record.UpdatedAt.Equal(syncedAt) {
		t.Fatalf("updated_at changed: %v", record.UpdatedAt)
	}
	if _, ok := record.Items["item_x"]; ok {
		t.Fatal("A4 must not write the item to disk")
	}
	if record.Items["item_a"] != 3 {
		t.Fatalf("existing item_a = %d, want 3", record.Items["item_a"])
	}
}

func TestSetItemQuantityVisibleToR1(t *testing.T) {
	withRecordPath(t, filepath.Join(t.TempDir(), "IMS.json"))

	if !runSetItemQuantity(t, `{"item":"item_x","value":40}`) {
		t.Fatal("set should succeed")
	}
	if _, hit := (&ItemQuantitySatisfied{}).Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_x}>=40"}`,
	}); !hit {
		t.Fatal("R1 should hit right after A4 writes the value")
	}

	if !runSetItemQuantity(t, `{"item":"item_x","value":10}`) {
		t.Fatal("set should succeed")
	}
	if _, hit := (&ItemQuantitySatisfied{}).Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_x}>=40"}`,
	}); hit {
		t.Fatal("R1 must compare against the new absolute value")
	}
}
