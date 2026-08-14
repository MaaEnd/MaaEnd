package intelarchive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
	"github.com/rs/zerolog/log"
)

var fileCategories = map[string]struct{}{
	"paper":      {},
	"digital":    {},
	"media":      {},
	"collection": {},
}

const (
	component           = "intelarchive"
	catalogResourcePath = "data/IntelArchive/catalog.json"
	itemsResourcePath   = "data/IntelArchive/items.json"
	unlockedFileName    = "intel_archive_unlocked.json"
)

type catalogFile struct {
	Version    int        `json:"version"`
	Categories []category `json:"categories"`
}

type category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type itemsFile struct {
	Version int             `json:"version"`
	Items   map[string]item `json:"items"`
}

type item struct {
	ID               string            `json:"id"`
	Names            map[string]string `json:"names"`
	FileCategory     string            `json:"fileCategory"`
	SklandWikiItemID string            `json:"sklandWikiItemId,omitempty"`
	TagIDs           []string          `json:"tagIds,omitempty"`
	Pages            any               `json:"pages,omitempty"`
}

type catalogIndex struct {
	NameToID map[string]string
}

type unlockedFile struct {
	Version  int                        `json:"version"`
	Accounts map[string]accountUnlocked `json:"accounts"`
}

type accountUnlocked struct {
	Unlocked []string `json:"unlocked"`
}

var (
	catalogPathFunc  = func() string { return catalogResourcePath }
	itemsPathFunc    = func() string { return itemsResourcePath }
	unlockedPathFunc = func() string { return filepath.Join("debug", "record", unlockedFileName) }

	catalogCache *catalogIndex
	catalogErr   error
)

func loadCatalogIndex() (*catalogIndex, error) {
	if catalogCache != nil || catalogErr != nil {
		return catalogCache, catalogErr
	}

	var cat catalogFile
	catPath := catalogPathFunc()
	if err := resource.ReadJsonResource(catPath, &cat); err != nil {
		catalogErr = fmt.Errorf("load intel archive catalog %s: %w", catPath, err)
		log.Error().Err(catalogErr).Str("component", component).Str("path", catPath).Msg("failed to load catalog")
		return nil, catalogErr
	}

	var items itemsFile
	itPath := itemsPathFunc()
	if err := resource.ReadJsonResource(itPath, &items); err != nil {
		catalogErr = fmt.Errorf("load intel archive items %s: %w", itPath, err)
		log.Error().Err(catalogErr).Str("component", component).Str("path", itPath).Msg("failed to load items")
		return nil, catalogErr
	}

	idx, err := buildCatalogIndex(&cat, &items)
	if err != nil {
		catalogErr = err
		log.Error().Err(err).Str("component", component).Msg("catalog validation failed")
		return nil, catalogErr
	}
	catalogCache = idx
	log.Info().
		Str("component", component).
		Int("category_count", len(cat.Categories)).
		Int("item_count", len(items.Items)).
		Msg("intel archive catalog loaded")
	return catalogCache, nil
}

func buildCatalogIndex(cat *catalogFile, items *itemsFile) (*catalogIndex, error) {
	if cat == nil {
		return nil, fmt.Errorf("catalog is nil")
	}
	if items == nil || items.Items == nil {
		return nil, fmt.Errorf("items is nil")
	}

	tagIDs := make(map[string]string, len(cat.Categories))
	for _, c := range cat.Categories {
		if c.ID == "" {
			return nil, fmt.Errorf("category id is empty")
		}
		if c.Name == "" {
			return nil, fmt.Errorf("category %q name is empty", c.ID)
		}
		if prev, exists := tagIDs[c.ID]; exists {
			return nil, fmt.Errorf("duplicate category id %q (%q and %q)", c.ID, prev, c.Name)
		}
		tagIDs[c.ID] = c.Name
	}

	ids := make([]string, 0, len(items.Items))
	for id := range items.Items {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return itemIDLess(ids[i], ids[j])
	})

	nameToID := make(map[string]string, len(items.Items)*2)
	wikiToID := make(map[string]string, len(items.Items))
	for _, id := range ids {
		it := items.Items[id]
		if it.ID == "" {
			it.ID = id
			items.Items[id] = it
		}
		if it.ID != id {
			return nil, fmt.Errorf("item key %q mismatches id %q", id, it.ID)
		}
		if it.Names == nil {
			return nil, fmt.Errorf("item %q names is empty", id)
		}
		zhCN := strings.TrimSpace(it.Names[i18n.LangZhCN])
		if zhCN == "" {
			return nil, fmt.Errorf("item %q names.zh_cn is empty", id)
		}
		if _, ok := fileCategories[it.FileCategory]; !ok {
			return nil, fmt.Errorf("item %q has unknown fileCategory %q", id, it.FileCategory)
		}
		for _, tagID := range it.TagIDs {
			if tagID == "" {
				return nil, fmt.Errorf("item %q has empty tag id", id)
			}
			if _, ok := tagIDs[tagID]; !ok {
				return nil, fmt.Errorf("item %q references unknown tagId %q", id, tagID)
			}
		}
		indexItemName(nameToID, zhCN, id)
		if zhTW := strings.TrimSpace(it.Names[i18n.LangZhTW]); zhTW != "" {
			indexItemName(nameToID, zhTW, id)
		}
		if it.SklandWikiItemID == "" {
			continue
		}
		if prev, exists := wikiToID[it.SklandWikiItemID]; exists {
			return nil, fmt.Errorf("duplicate sklandWikiItemId %q for %q and %q", it.SklandWikiItemID, prev, id)
		}
		wikiToID[it.SklandWikiItemID] = id
	}

	return &catalogIndex{NameToID: nameToID}, nil
}

func itemIDLess(a, b string) bool {
	ai, aErr := strconv.Atoi(a)
	bi, bErr := strconv.Atoi(b)
	if aErr == nil && bErr == nil {
		return ai < bi
	}
	return a < b
}

func indexItemName(nameToID map[string]string, name, id string) {
	if prev, exists := nameToID[name]; exists {
		if prev != id {
			log.Warn().
				Str("component", component).
				Str("name", name).
				Str("keep_id", prev).
				Str("skip_id", id).
				Msg("duplicate item name, keep first id")
		}
		return
	}
	nameToID[name] = id
}

func loadUnlocked() (unlockedFile, error) {
	path := unlockedPathFunc()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return unlockedFile{
				Version:  1,
				Accounts: map[string]accountUnlocked{},
			}, nil
		}
		return unlockedFile{}, fmt.Errorf("read unlocked file: %w", err)
	}
	if len(raw) == 0 {
		return unlockedFile{
			Version:  1,
			Accounts: map[string]accountUnlocked{},
		}, nil
	}

	var doc unlockedFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return unlockedFile{}, fmt.Errorf("unmarshal unlocked file: %w", err)
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	if doc.Accounts == nil {
		doc.Accounts = map[string]accountUnlocked{}
	}
	return doc, nil
}

func saveUnlocked(doc unlockedFile) error {
	path := unlockedPathFunc()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create unlocked dir: %w", err)
	}
	if doc.Version == 0 {
		doc.Version = 1
	}
	if doc.Accounts == nil {
		doc.Accounts = map[string]accountUnlocked{}
	}

	raw, err := json.MarshalIndent(doc, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal unlocked file: %w", err)
	}
	raw = append(raw, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+unlockedFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("create unlocked temp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write unlocked temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close unlocked temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename unlocked file: %w", err)
	}
	cleanup = false
	return nil
}

// unlockItems appends item IDs under the given UID. Missing file/dir is created.
// Returns newly added IDs.
func unlockItems(uid string, itemIDs []string) ([]string, error) {
	if uid == "" {
		return nil, fmt.Errorf("uid is empty")
	}
	if len(itemIDs) == 0 {
		return nil, nil
	}

	doc, err := loadUnlocked()
	if err != nil {
		return nil, err
	}

	account := doc.Accounts[uid]
	owned := make(map[string]struct{}, len(account.Unlocked))
	for _, id := range account.Unlocked {
		owned[id] = struct{}{}
	}

	added := make([]string, 0, len(itemIDs))
	for _, id := range itemIDs {
		if id == "" {
			continue
		}
		if _, exists := owned[id]; exists {
			continue
		}
		owned[id] = struct{}{}
		account.Unlocked = append(account.Unlocked, id)
		added = append(added, id)
	}
	if len(added) == 0 {
		return nil, nil
	}

	doc.Accounts[uid] = account
	if err := saveUnlocked(doc); err != nil {
		return nil, err
	}

	log.Info().
		Str("component", component).
		Str("uid", uid).
		Strs("added", added).
		Int("unlocked_count", len(account.Unlocked)).
		Str("path", unlockedPathFunc()).
		Msg("unlocked items persisted")
	return added, nil
}
