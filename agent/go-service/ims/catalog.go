package ims

import (
	"fmt"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

const itemsCatalogResourcePath = "data/IMS/items.json"

// itemsCatalog is the on-disk registry of A3-supported reward items and their
// recognition nodes. Path: assets/data/IMS/items.json.
// A2 (SyncItemData) always requires an explicit items map and does not use this file.
type itemsCatalog struct {
	A3 map[string]string `json:"a3"`
}

var (
	itemsCatalogPathFunc = defaultItemsCatalogPath
	itemsCatalogOnce     sync.Once
	itemsCatalogCache    *itemsCatalog
	itemsCatalogErr      error
)

func defaultItemsCatalogPath() string {
	return itemsCatalogResourcePath
}

func resetItemsCatalogForTest() {
	itemsCatalogOnce = sync.Once{}
	itemsCatalogCache = nil
	itemsCatalogErr = nil
}

func loadItemsCatalog() (*itemsCatalog, error) {
	itemsCatalogOnce.Do(func() {
		path := itemsCatalogPathFunc()
		var cat itemsCatalog
		if err := resource.ReadJsonResource(path, &cat); err != nil {
			itemsCatalogErr = fmt.Errorf("load IMS items catalog %s: %w", path, err)
			log.Error().
				Err(itemsCatalogErr).
				Str("path", path).
				Msg("failed to load IMS items catalog")
			return
		}
		if err := validateItemsCatalog(&cat); err != nil {
			itemsCatalogErr = fmt.Errorf("invalid IMS items catalog %s: %w", path, err)
			log.Error().
				Err(itemsCatalogErr).
				Str("path", path).
				Msg("IMS items catalog validation failed")
			return
		}
		itemsCatalogCache = &cat
		log.Info().
			Str("path", path).
			Int("a3_count", len(cat.A3)).
			Msg("IMS items catalog loaded")
	})
	if itemsCatalogErr != nil {
		return nil, itemsCatalogErr
	}
	if itemsCatalogCache == nil {
		return nil, fmt.Errorf("IMS items catalog not loaded")
	}
	return itemsCatalogCache, nil
}

func validateItemsCatalog(cat *itemsCatalog) error {
	if cat == nil {
		return fmt.Errorf("catalog is nil")
	}
	if len(cat.A3) == 0 {
		return fmt.Errorf("a3 is empty")
	}
	return validateItemsMap("a3", cat.A3)
}

func validateItemsMap(label string, items map[string]string) error {
	for id, node := range items {
		if id == "" || node == "" {
			return fmt.Errorf("%s contains empty item id or node name", label)
		}
	}
	return nil
}

// resolveA3ItemsMap returns explicit items when non-empty; otherwise loads the
// a3 section of assets/data/IMS/items.json.
func resolveA3ItemsMap(explicit map[string]string) (map[string]string, error) {
	if len(explicit) > 0 {
		if err := validateItemsMap("explicit", explicit); err != nil {
			return nil, err
		}
		return explicit, nil
	}
	cat, err := loadItemsCatalog()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(cat.A3))
	for id, node := range cat.A3 {
		out[id] = node
	}
	return out, nil
}
