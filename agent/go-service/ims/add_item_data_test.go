package ims

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAddItemDataParamEmpty(t *testing.T) {
	params, err := parseAddItemDataParam("")
	if err != nil {
		t.Fatal(err)
	}
	if params.GridType != "" || len(params.ItemFilters) != 0 || len(params.ItemIDs) != 0 {
		t.Fatalf("params=%+v", params)
	}

	params, err = parseAddItemDataParam(`{"item_filters":["Isolate:*"],"item_ids":["item_spaceship_credit"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(params.ItemFilters) != 1 || params.ItemFilters[0] != "Isolate:*" {
		t.Fatalf("params=%+v", params)
	}
	if len(params.ItemIDs) != 1 || params.ItemIDs[0] != "item_spaceship_credit" {
		t.Fatalf("params=%+v", params)
	}
}

func TestResolveAddItemDataCandidatesUnion(t *testing.T) {
	resetRecognitionItemsForTest()
	oldPath := recognitionItemsPathFunc
	t.Cleanup(func() {
		recognitionItemsPathFunc = oldPath
		resetRecognitionItemsForTest()
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "recognition_items.json")
	content := `{
		"item_spaceship_credit": {"storageKind":"Isolate","categoryType":"SpaceshipGold"},
		"item_weapon_expcard_low": {"storageKind":"ValuableDepot","categoryType":"SpecialItem"},
		"item_weapon_expcard_mid": {"storageKind":"ValuableDepot","categoryType":"SpecialItem"},
		"item_weapon_break_low": {"storageKind":"ValuableDepot","categoryType":"SpecialItem"}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recognitionItemsPathFunc = func() string { return path }

	filters, ids, err := resolveAddItemDataCandidates(
		[]string{"Isolate:SpaceshipGold"},
		[]string{"item_weapon_expcard_low", "item_weapon_expcard_mid"},
	)
	if err != nil {
		t.Fatal(err)
	}
	wantFilters := map[string]struct{}{
		"Isolate:SpaceshipGold":     {},
		"ValuableDepot:SpecialItem": {},
	}
	if len(filters) != len(wantFilters) {
		t.Fatalf("filters=%v", filters)
	}
	for _, f := range filters {
		if _, ok := wantFilters[f]; !ok {
			t.Fatalf("unexpected filter %s in %v", f, filters)
		}
	}
	wantIDs := map[string]struct{}{
		"item_spaceship_credit":   {},
		"item_weapon_expcard_low": {},
		"item_weapon_expcard_mid": {},
	}
	if len(ids) != len(wantIDs) {
		t.Fatalf("ids=%v", ids)
	}
	for _, id := range ids {
		if _, ok := wantIDs[id]; !ok {
			t.Fatalf("unexpected id %s in %v", id, ids)
		}
	}
}
