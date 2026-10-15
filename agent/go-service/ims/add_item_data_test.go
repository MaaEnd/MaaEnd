package ims

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func withRecognitionItemsPath(t *testing.T, path string) {
	t.Helper()
	resetRecognitionItemsForTest()
	recognitionItemsPathFunc = func() string { return path }
	t.Cleanup(func() {
		recognitionItemsPathFunc = defaultRecognitionItemsPath
		resetRecognitionItemsForTest()
	})
}

func TestResolveAddItemDataCandidatesCatalogMissingIsUnavailable(t *testing.T) {
	withRecognitionItemsPath(t, filepath.Join(t.TempDir(), "missing.json"))

	_, _, err := resolveAddItemDataCandidates(nil, []string{"item_domain_jinlong_coupon"})
	if !errors.Is(err, errRecognitionCatalogUnavailable) {
		t.Fatalf("missing catalog should be unavailable, got %v", err)
	}
}

func TestResolveAddItemDataCandidatesUnknownIDStaysHard(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recognition_items.json")
	body := []byte(`{"item_known":{"storageKind":"ValuableDepot","categoryType":"SpecialItem"}}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	withRecognitionItemsPath(t, path)

	_, _, err := resolveAddItemDataCandidates(nil, []string{"item_missing"})
	if err == nil || errors.Is(err, errRecognitionCatalogUnavailable) {
		t.Fatalf("unknown item_id should stay a hard error, got %v", err)
	}
}

func TestResolveAddItemDataCandidatesDuplicateStaysHard(t *testing.T) {
	_, _, err := resolveAddItemDataCandidates(nil, []string{"item_a", "item_a"})
	if err == nil || errors.Is(err, errRecognitionCatalogUnavailable) {
		t.Fatalf("duplicate item_id should stay a hard error, got %v", err)
	}
}
