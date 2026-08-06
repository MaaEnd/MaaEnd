package ims

import (
	"fmt"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

const itemsCatalogResourcePath = "data/IMS/items.json"

type itemsCatalogAPI string

const (
	itemsCatalogA2 itemsCatalogAPI = "a2"
	itemsCatalogA3 itemsCatalogAPI = "a3"
)

// itemsCatalog is the on-disk registry of IMS-supported items and their
// recognition nodes for A2 / A3. Path: assets/data/IMS/items.json.
type itemsCatalog struct {
	A2 map[string]string `json:"a2"`
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
			Int("a2_count", len(cat.A2)).
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
	if len(cat.A2) == 0 && len(cat.A3) == 0 {
		return fmt.Errorf("a2 and a3 are both empty")
	}
	if err := validateItemsMap("a2", cat.A2); err != nil {
		return err
	}
	if err := validateItemsMap("a3", cat.A3); err != nil {
		return err
	}
	return nil
}

func validateItemsMap(label string, items map[string]string) error {
	for id, node := range items {
		if id == "" || node == "" {
			return fmt.Errorf("%s contains empty item id or node name", label)
		}
	}
	return nil
}

// resolveItemsMap returns explicit items when non-empty; otherwise loads the
// catalog section for the given API (a2 / a3).
func resolveItemsMap(explicit map[string]string, api itemsCatalogAPI) (map[string]string, error) {
	if len(explicit) > 0 {
		return explicit, nil
	}
	cat, err := loadItemsCatalog()
	if err != nil {
		return nil, err
	}
	var items map[string]string
	switch api {
	case itemsCatalogA2:
		items = cat.A2
	case itemsCatalogA3:
		items = cat.A3
	default:
		return nil, fmt.Errorf("unknown items catalog api %q", api)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("IMS items catalog %s is empty", api)
	}
	// Copy so callers cannot mutate the cached catalog.
	out := make(map[string]string, len(items))
	for id, node := range items {
		out[id] = node
	}
	return out, nil
}
