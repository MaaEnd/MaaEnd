package ims

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
)

func TestItemIDsMatchingFilters(t *testing.T) {
	resetRecognitionItemsForTest()
	oldPath := recognitionItemsPathFunc
	t.Cleanup(func() {
		recognitionItemsPathFunc = oldPath
		resetRecognitionItemsForTest()
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "recognition_items.json")
	content := `{
		"item_char_break_stage_1_2": {"storageKind":"ValuableDepot","categoryType":"SpecialItem"},
		"item_ticketgacha_special_single": {"storageKind":"ValuableDepot","categoryType":"CommercialItem"},
		"item_gold": {"storageKind":"Isolate","categoryType":"Gold"}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recognitionItemsPathFunc = func() string { return path }

	got, err := itemIDsMatchingFilters([]string{"ValuableDepot:SpecialItem"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "item_char_break_stage_1_2" {
		t.Fatalf("SpecialItem=%v", got)
	}

	got, err = itemIDsMatchingFilters([]string{"ValuableDepot:*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ValuableDepot:*=%v", got)
	}

	got, err = resolveRegionRebuildIDs(iconqty.GridValuables, []string{"ValuableDepot:CommercialItem"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "item_ticketgacha_special_single" {
		t.Fatalf("rebuild=%v", got)
	}

	got, err = resolveRegionRebuildIDs(iconqty.GridRewards, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("rewards defaults should cover all fixtures, got=%v", got)
	}
}

func TestParseSyncItemDataParamRequiresBody(t *testing.T) {
	if _, err := parseSyncItemDataParam(""); err == nil {
		t.Fatal("expected error for empty param")
	}
	params, err := parseSyncItemDataParam(`{"page_dedup": true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !params.PageDedup || len(params.Items) != 0 || params.GridType != "" {
		t.Fatalf("params=%+v", params)
	}

	params, err = parseSyncItemDataParam(`{
		"grid_type":"valuables",
		"item_filters":["ValuableDepot:SpecialItem"],
		"items":{"item_gold":"item_gold_NUMBER"}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if params.GridType != "valuables" ||
		len(params.ItemFilters) != 1 ||
		params.Items["item_gold"] != "item_gold_NUMBER" {
		t.Fatalf("params=%+v", params)
	}
}

func TestItemMatchesFilter(t *testing.T) {
	if !itemMatchesFilter("ValuableDepot", "SpecialItem", "ValuableDepot:SpecialItem") {
		t.Fatal("exact match")
	}
	if !itemMatchesFilter("ValuableDepot", "SpecialItem", "ValuableDepot:*") {
		t.Fatal("wildcard match")
	}
	if itemMatchesFilter("Isolate", "Gold", "ValuableDepot:*") {
		t.Fatal("kind mismatch")
	}
}
