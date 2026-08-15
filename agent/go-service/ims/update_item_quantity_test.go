package ims

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestParseUpdateItemQuantityParam(t *testing.T) {
	if _, err := parseUpdateItemQuantityParam(""); err == nil {
		t.Fatal("expected error for empty param")
	}
	if _, err := parseUpdateItemQuantityParam(`{"item":"","delta":1}`); err == nil {
		t.Fatal("expected error for empty item")
	}
	params, err := parseUpdateItemQuantityParam(`{"item":" item_char_break_stage_1_2 ","delta":-3}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Item != "item_char_break_stage_1_2" || params.Delta != -3 {
		t.Fatalf("got %+v", params)
	}
}

func TestUpdateItemQuantityRun(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	dir := t.TempDir()
	prev := recordPathFunc
	recordPathFunc = func() string {
		return filepath.Join(dir, "IMS.json")
	}
	t.Cleanup(func() { recordPathFunc = prev })

	a := &UpdateItemQuantity{}
	arg := &maa.CustomActionArg{
		CustomActionParam: `{"item":"item_char_break_stage_1_2","delta":5}`,
	}
	if !a.Run(nil, arg) {
		t.Fatal("expected success from empty cache +delta")
	}
	if got := globalCache.quantity("item_char_break_stage_1_2"); got != 5 {
		t.Fatalf("quantity=%d, want 5", got)
	}
	hasData, _ := globalCache.snapshot()
	if hasData {
		t.Fatal("A1 must not mark cache ready")
	}

	MarkSynced(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), map[string]int{"item_char_break_stage_1_2": 5})
	if !a.Run(nil, &maa.CustomActionArg{CustomActionParam: `{"item":"item_char_break_stage_1_2","delta":-2}`}) {
		t.Fatal("expected success for -delta")
	}
	if got := globalCache.quantity("item_char_break_stage_1_2"); got != 3 {
		t.Fatalf("quantity=%d, want 3", got)
	}
	hasData, lastSync := globalCache.snapshot()
	if !hasData {
		t.Fatal("expected hasData preserved after A1")
	}
	if !lastSync.Equal(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("lastSync changed: %v", lastSync)
	}

	if !a.Run(nil, &maa.CustomActionArg{CustomActionParam: `{"item":"item_char_break_stage_1_2","delta":-100}`}) {
		t.Fatal("expected success when clamping")
	}
	if got := globalCache.quantity("item_char_break_stage_1_2"); got != 0 {
		t.Fatalf("quantity=%d, want 0 after clamp", got)
	}

	raw, err := os.ReadFile(recordPathFunc())
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty IMS.json")
	}
}
