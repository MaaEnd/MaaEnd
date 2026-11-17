package ims

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

// errRecognitionCatalogUnavailable marks a missing, unreadable, or empty
// recognition_items.json. A3 logs it and continues; A2 still fails the sync.
var errRecognitionCatalogUnavailable = errors.New("IconRecognition catalog unavailable")

const recognitionItemsResourcePath = "data/IconRecognition/recognition_items.json"

type recognitionItemMeta struct {
	StorageKind  string `json:"storageKind"`
	CategoryType string `json:"categoryType"`
	Rarity       int    `json:"rarity"`
	IconID       string `json:"iconId"`
}

var (
	recognitionItemsPathFunc = defaultRecognitionItemsPath
	recognitionItemsMu       sync.Mutex
	recognitionItemsCache    map[string]recognitionItemMeta
	// recognitionItemsLogged guards the failure log: retries must stay quiet
	// because every A2 page rebuild resolves filters again.
	recognitionItemsLogged bool
)

func defaultRecognitionItemsPath() string {
	return recognitionItemsResourcePath
}

func resetRecognitionItemsForTest() {
	recognitionItemsMu.Lock()
	defer recognitionItemsMu.Unlock()
	recognitionItemsCache = nil
	recognitionItemsLogged = false
}

// loadRecognitionItems loads the IconRecognition catalog once and caches it.
// A failed load is NOT cached: the next call retries, so a Resource that is not
// ready yet during startup cannot disable catalog-dependent components (A4's
// inventory writes) for the rest of the process.
func loadRecognitionItems() (map[string]recognitionItemMeta, error) {
	recognitionItemsMu.Lock()
	defer recognitionItemsMu.Unlock()
	if recognitionItemsCache != nil {
		return recognitionItemsCache, nil
	}

	path := recognitionItemsPathFunc()
	var raw map[string]recognitionItemMeta
	if err := resource.ReadJsonResource(path, &raw); err != nil {
		return nil, reportCatalogLoadFailure(path, fmt.Errorf("%w: load IconRecognition catalog %s: %w", errRecognitionCatalogUnavailable, path, err))
	}
	if len(raw) == 0 {
		return nil, reportCatalogLoadFailure(path, fmt.Errorf("%w: IconRecognition catalog %s is empty", errRecognitionCatalogUnavailable, path))
	}

	recognitionItemsCache = raw
	recognitionItemsLogged = false
	log.Info().
		Str("path", path).
		Int("item_count", len(raw)).
		Msg("IconRecognition catalog loaded for IMS region rebuild")
	return raw, nil
}

// reportCatalogLoadFailure logs the first failure only. Callers retry on the next
// call, and a missing catalog would otherwise spam one error per scanned page.
func reportCatalogLoadFailure(path string, err error) error {
	if !recognitionItemsLogged {
		recognitionItemsLogged = true
		log.Error().
			Err(err).
			Str("path", path).
			Msg("failed to load IconRecognition recognition_items, retrying on next call")
	}
	return err
}

// validateInventoryItemID reports whether item is a known IconRecognition catalog key.
// A4 uses it to catch typos before writing an inventory quantity; virtual keys skip it.
// A missing or empty catalog is an error: without it an id cannot be told apart from a typo.
func validateInventoryItemID(item string) error {
	catalog, err := loadRecognitionItems()
	if err != nil {
		return fmt.Errorf("cannot validate item %q without the IconRecognition catalog: %w", item, err)
	}
	if _, ok := catalog[item]; !ok {
		return fmt.Errorf("item %q is not in the IconRecognition catalog; use a %q prefixed key for virtual items", item, scratchPrefix)
	}
	return nil
}

// itemIDsMatchingFilters returns recognition_items.json top-level keys matching
// any of the IconRecognition item_filters (union). Used only for A2 region
// rebuild miss-clear — recognition itself is not filtered by this list.
func itemIDsMatchingFilters(filters []string) ([]string, error) {
	filters, err := iconqty.NormalizeStringList(filters, "item_filters")
	if err != nil {
		return nil, err
	}
	if len(filters) == 0 {
		return nil, nil
	}
	catalog, err := loadRecognitionItems()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0)
	seen := make(map[string]struct{})
	for id, meta := range catalog {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !itemMatchesAnyFilter(meta.StorageKind, meta.CategoryType, filters) {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func itemMatchesAnyFilter(storageKind, categoryType string, filters []string) bool {
	for _, f := range filters {
		if itemMatchesFilter(storageKind, categoryType, f) {
			return true
		}
	}
	return false
}

// itemMatchesFilter parses storageKind:categoryType; categoryType may be "*".
func itemMatchesFilter(storageKind, categoryType, filter string) bool {
	filter = strings.TrimSpace(filter)
	kind, cat, ok := strings.Cut(filter, ":")
	if !ok || strings.TrimSpace(kind) == "" || strings.TrimSpace(cat) == "" {
		return false
	}
	kind = strings.TrimSpace(kind)
	cat = strings.TrimSpace(cat)
	if storageKind != kind {
		return false
	}
	if cat == "*" {
		return true
	}
	return categoryType == cat
}

// resolveRegionRebuildIDs expands item_filters (or grid defaults) against the
// IconRecognition catalog so page_dedup=false can clear misses without an
// IMS-owned allowlist.
func resolveRegionRebuildIDs(gridType string, filters []string) ([]string, error) {
	effective := filters
	if len(effective) == 0 {
		effective = iconqty.DefaultItemFilters(gridType)
	}
	return itemIDsMatchingFilters(effective)
}
