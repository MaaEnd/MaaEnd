package ims

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveItemsMapUsesCatalogWhenEmpty(t *testing.T) {
	resetItemsCatalogForTest()
	oldPath := itemsCatalogPathFunc
	t.Cleanup(func() {
		itemsCatalogPathFunc = oldPath
		resetItemsCatalogForTest()
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	content := `{
		"a2": {"PROTODISK": "PROTODISK", "CAST_DIE": "CAST_DIE"},
		"a3": {"PROTODISK": "PROTODISK", "T_CREDS": "T_CREDS"}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	itemsCatalogPathFunc = func() string { return path }

	a2, err := resolveItemsMap(nil, itemsCatalogA2)
	if err != nil {
		t.Fatal(err)
	}
	if len(a2) != 2 || a2["PROTODISK"] != "PROTODISK" || a2["CAST_DIE"] != "CAST_DIE" {
		t.Fatalf("a2=%v", a2)
	}

	a3, err := resolveItemsMap(map[string]string{}, itemsCatalogA3)
	if err != nil {
		t.Fatal(err)
	}
	if len(a3) != 2 || a3["T_CREDS"] != "T_CREDS" {
		t.Fatalf("a3=%v", a3)
	}

	// Explicit map wins.
	explicit := map[string]string{"ONLY": "ONLY_NODE"}
	got, err := resolveItemsMap(explicit, itemsCatalogA3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["ONLY"] != "ONLY_NODE" {
		t.Fatalf("explicit=%v", got)
	}
}

func TestParseAddItemDataParamEmptyUsesCatalogDefaults(t *testing.T) {
	params, err := parseAddItemDataParam("")
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Items) != 0 {
		t.Fatalf("items=%v", params.Items)
	}
	if !params.maskHitRegionEnabled() {
		t.Fatal("mask_hit_region should default true")
	}

	params, err = parseAddItemDataParam(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(params.Items) != 0 {
		t.Fatalf("items=%v", params.Items)
	}
}

func TestParseSyncItemDataParamEmptyAllowed(t *testing.T) {
	params, err := parseSyncItemDataParam("")
	if err != nil {
		t.Fatal(err)
	}
	if params.PageDedup {
		t.Fatal("page_dedup should default false")
	}
	params, err = parseSyncItemDataParam(`{"page_dedup": true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !params.PageDedup || len(params.Items) != 0 {
		t.Fatalf("params=%+v", params)
	}
}
