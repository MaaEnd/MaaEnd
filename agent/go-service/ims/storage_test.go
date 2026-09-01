package ims

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func withTempRecord(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "IMS.json")
	oldPathFunc := recordPathFunc
	recordPathFunc = func() string { return path }
	t.Cleanup(func() {
		recordPathFunc = oldPathFunc
		ClearCache()
	})
	ClearCache()
	return path
}

func mustCorruptBackup(t *testing.T, recordPath string, want []byte) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(recordPath), "IMS.json.corrupt-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("corrupt backups=%v, want 1", matches)
	}
	got, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("backup %s bytes mismatch, len=%d want=%d", matches[0], len(got), len(want))
	}
}

func TestEnsureHydratedResetsNULFile(t *testing.T) {
	path := withTempRecord(t)
	if err := os.WriteFile(path, make([]byte, 3354), 0o644); err != nil {
		t.Fatal(err)
	}
	simulateRestart()

	if err := ensureHydrated(); err != nil {
		t.Fatalf("ensureHydrated: %v", err)
	}
	hasData, _ := globalCache.snapshot()
	if hasData {
		t.Fatal("reset cache must not be marked ready")
	}
	if len(ItemsSnapshot()) != 0 {
		t.Fatalf("reset cache items=%v", ItemsSnapshot())
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte{0}) {
		t.Fatal("reset file still contains NUL")
	}
	var rec recordFile
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("reset file is not JSON: %v", err)
	}
	mustCorruptBackup(t, path, make([]byte, 3354))
}

func TestEnsureHydratedResetsInvalidJSON(t *testing.T) {
	path := withTempRecord(t)
	if err := os.WriteFile(path, []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	simulateRestart()

	if err := ensureHydrated(); err != nil {
		t.Fatalf("ensureHydrated: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec recordFile
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("reset file is not JSON: %v", err)
	}
	mustCorruptBackup(t, path, []byte("{not-json"))
}

func TestEnsureHydratedKeepsValidRecord(t *testing.T) {
	path := withTempRecord(t)
	at := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	if err := persistSynced(at, map[string]int{"item_diamond": 12}); err != nil {
		t.Fatal(err)
	}
	simulateRestart()

	if err := ensureHydrated(); err != nil {
		t.Fatalf("ensureHydrated: %v", err)
	}
	if ItemsSnapshot()["item_diamond"] != 12 {
		t.Fatalf("items=%v", ItemsSnapshot())
	}
	hasData, lastSync := globalCache.snapshot()
	if !hasData || !lastSync.Equal(at) {
		t.Fatalf("hasData=%v lastSync=%v", hasData, lastSync)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec recordFile
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Items["item_diamond"] != 12 {
		t.Fatalf("disk items=%v", rec.Items)
	}
}

func TestIMSComponentsContinueAfterCorruptReset(t *testing.T) {
	path := withTempRecord(t)
	if err := os.WriteFile(path, []byte("\x00\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	simulateRestart()

	ready := &ItemDataReady{}
	if _, ok := ready.Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"refresh_days":7}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}); ok {
		t.Fatal("ItemDataReady should miss after reset (no_data)")
	}

	qty := &ItemQuantitySatisfied{}
	if _, ok := qty.Run(nil, &maa.CustomRecognitionArg{
		CustomRecognitionParam: `{"expression":"{item_diamond}>=0"}`,
		Roi:                    maa.Rect{0, 0, 1, 1},
	}); !ok {
		t.Fatal("ItemQuantitySatisfied should run against empty reset cache")
	}

	upd := &UpdateItemQuantity{}
	if !upd.Run(nil, &maa.CustomActionArg{
		CustomActionParam: `{"item":"item_diamond","delta":4}`,
	}) {
		t.Fatal("UpdateItemQuantity should succeed after reset")
	}
	if got := globalCache.quantity("item_diamond"); got != 4 {
		t.Fatalf("quantity=%d", got)
	}

	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := persistSynced(at, map[string]int{"item_diamond": 4, "item_originium_recharge": 8}); err != nil {
		t.Fatal(err)
	}
	hasData, _ := globalCache.snapshot()
	if !hasData {
		t.Fatal("A2 persist after reset should mark cache ready")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec recordFile
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("rebuilt file is not JSON: %v", err)
	}
	if rec.Items["item_diamond"] != 4 || rec.Items["item_originium_recharge"] != 8 {
		t.Fatalf("rebuilt items=%v", rec.Items)
	}
	mustCorruptBackup(t, path, []byte("\x00\x00\x00"))
}
