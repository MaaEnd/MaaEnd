package ims

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveA3ItemsMapUsesCatalogWhenEmpty(t *testing.T) {
	resetItemsCatalogForTest()
	oldPath := itemsCatalogPathFunc
	t.Cleanup(func() {
		itemsCatalogPathFunc = oldPath
		resetItemsCatalogForTest()
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "items.json")
	content := `{
		"a3": {"PROTODISK": "PROTODISK", "T_CREDS": "T_CREDS"}
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	itemsCatalogPathFunc = func() string { return path }

	a3, err := resolveA3ItemsMap(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(a3) != 2 || a3["PROTODISK"] != "PROTODISK" || a3["T_CREDS"] != "T_CREDS" {
		t.Fatalf("a3=%v", a3)
	}

	a3, err = resolveA3ItemsMap(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(a3) != 2 {
		t.Fatalf("empty map should load catalog, got %v", a3)
	}

	explicit := map[string]string{"ONLY": "ONLY_NODE"}
	got, err := resolveA3ItemsMap(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got["ONLY"] != "ONLY_NODE" {
		t.Fatalf("explicit=%v", got)
	}

	if _, err := resolveA3ItemsMap(map[string]string{"": "NODE"}); err == nil {
		t.Fatal("expected error for empty item id")
	}
	if _, err := resolveA3ItemsMap(map[string]string{"ITEM": ""}); err == nil {
		t.Fatal("expected error for empty node name")
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

func TestParseSyncItemDataParamRequiresBody(t *testing.T) {
	if _, err := parseSyncItemDataParam(""); err == nil {
		t.Fatal("expected error for empty param")
	}
	params, err := parseSyncItemDataParam(`{"page_dedup": true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !params.PageDedup || len(params.Items) != 0 {
		t.Fatalf("params=%+v", params)
	}
}
