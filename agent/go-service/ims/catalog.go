package ims

import (
	"fmt"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconqty"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

const recognitionItemsResourcePath = "data/IconRecognition/recognition_items.json"

type recognitionItemMeta struct {
	StorageKind  string `json:"storageKind"`
	CategoryType string `json:"categoryType"`
}

var (
	recognitionItemsPathFunc = defaultRecognitionItemsPath
	recognitionItemsOnce     sync.Once
	recognitionItemsCache    map[string]recognitionItemMeta
	recognitionItemsErr      error
)

func defaultRecognitionItemsPath() string {
	return recognitionItemsResourcePath
}

func resetRecognitionItemsForTest() {
	recognitionItemsOnce = sync.Once{}
	recognitionItemsCache = nil
	recognitionItemsErr = nil
}

func loadRecognitionItems() (map[string]recognitionItemMeta, error) {
	recognitionItemsOnce.Do(func() {
		path := recognitionItemsPathFunc()
		var raw map[string]recognitionItemMeta
		if err := resource.ReadJsonResource(path, &raw); err != nil {
			recognitionItemsErr = fmt.Errorf("load IconRecognition catalog %s: %w", path, err)
			log.Error().
				Err(recognitionItemsErr).
				Str("path", path).
				Msg("failed to load IconRecognition recognition_items")
			return
		}
		if len(raw) == 0 {
			recognitionItemsErr = fmt.Errorf("IconRecognition catalog %s is empty", path)
			return
		}
		recognitionItemsCache = raw
		log.Info().
			Str("path", path).
			Int("item_count", len(raw)).
			Msg("IconRecognition catalog loaded for IMS region rebuild")
	})
	if recognitionItemsErr != nil {
		return nil, recognitionItemsErr
	}
	if recognitionItemsCache == nil {
		return nil, fmt.Errorf("IconRecognition catalog not loaded")
	}
	return recognitionItemsCache, nil
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
