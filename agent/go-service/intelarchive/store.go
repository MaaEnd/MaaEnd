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
	"investigate": {},
	"paper":       {},
	"digital":     {},
	"media":       {},
	"collection":  {},
	"document":    {},
	"report":      {},
}

const (
	component           = "intelarchive"
	catalogResourcePath = "data/IntelArchive/catalog.json"
	itemsResourcePath   = "data/IntelArchive/items.json"
	unlockedFileName    = "IntelArchiveUnlocked.json"
)

func recordOutputDir() string {
	return filepath.Join("debug", "record", "IntelArchive")
}

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

type itemPage struct {
	ID    string            `json:"id"`
	Order int               `json:"order,omitempty"`
	Names map[string]string `json:"names"`
}

type itemGroupEntry struct {
	ItemID string `json:"itemId"`
	PageID string `json:"pageId,omitempty"`
}

type itemGroup struct {
	ID      string            `json:"id"`
	Names   map[string]string `json:"names,omitempty"`
	Entries []itemGroupEntry  `json:"entries,omitempty"`
}

type item struct {
	ID             string            `json:"id"`
	Names          map[string]string `json:"names"`
	Page           string            `json:"page"`
	FileCategory   string            `json:"fileCategory"`
	TagIDs         []string          `json:"tagIds,omitempty"`
	Pages          []itemPage        `json:"pages,omitempty"`
	RelatedItemIDs []string          `json:"relatedItemIds,omitempty"`
	Groups         []itemGroup       `json:"groups,omitempty"`
	Order          int               `json:"order,omitempty"`
}

type catalogIndex struct {
	NameToIDs      map[string][]string
	NameToItems    map[string][]string
	PageToItem     map[string]string
	ItemPageCount  map[string]int
	NormToOrig     map[string]string
	UnlockCategory map[string]string
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
	unlockedPathFunc = func() string { return filepath.Join(recordOutputDir(), unlockedFileName) }

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

	idx := &catalogIndex{
		NameToIDs:      make(map[string][]string, len(items.Items)*2),
		NameToItems:    make(map[string][]string, len(items.Items)*2),
		PageToItem:     make(map[string]string, len(items.Items)*2),
		ItemPageCount:  make(map[string]int, len(items.Items)),
		NormToOrig:     make(map[string]string, len(items.Items)*2),
		UnlockCategory: make(map[string]string, len(items.Items)*2),
	}
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
		if it.Page == "" {
			return nil, fmt.Errorf("item %q page is empty", id)
		}
		if _, ok := tagIDs[it.Page]; !ok {
			return nil, fmt.Errorf("item %q references unknown page %q", id, it.Page)
		}
		pageInTags := false
		for _, tagID := range it.TagIDs {
			if tagID == "" {
				return nil, fmt.Errorf("item %q has empty tag id", id)
			}
			if _, ok := tagIDs[tagID]; !ok {
				return nil, fmt.Errorf("item %q references unknown tagId %q", id, tagID)
			}
			if tagID == it.Page {
				pageInTags = true
			}
		}
		if !pageInTags {
			return nil, fmt.Errorf("item %q tagIds does not include page %q", id, it.Page)
		}
		idx.ItemPageCount[id] = len(it.Pages)
		idx.UnlockCategory[id] = it.FileCategory
		indexNamed(idx, idx.NameToItems, zhCN, id)
		if zhTW := strings.TrimSpace(it.Names[i18n.LangZhTW]); zhTW != "" {
			indexNamed(idx, idx.NameToItems, zhTW, id)
		}
		for i, page := range it.Pages {
			if page.ID == "" {
				return nil, fmt.Errorf("item %q pages[%d] id is empty", id, i)
			}
			if page.Names == nil {
				return nil, fmt.Errorf("item %q pages[%d] names is empty", id, i)
			}
			pageCN := strings.TrimSpace(page.Names[i18n.LangZhCN])
			if pageCN == "" {
				return nil, fmt.Errorf("item %q pages[%d] names.zh_cn is empty", id, i)
			}
			idx.PageToItem[page.ID] = id
			idx.UnlockCategory[page.ID] = it.FileCategory
			indexItemName(idx, pageCN, page.ID)
			if pageTW := strings.TrimSpace(page.Names[i18n.LangZhTW]); pageTW != "" {
				indexItemName(idx, pageTW, page.ID)
			}
		}
		indexItemTitles(idx, it, zhCN)
	}

	return idx, nil
}

func itemIDLess(a, b string) bool {
	ai, aErr := strconv.Atoi(a)
	bi, bErr := strconv.Atoi(b)
	if aErr == nil && bErr == nil {
		return ai < bi
	}
	return a < b
}

func indexItemTitles(idx *catalogIndex, it item, zhCN string) {
	switch len(it.Pages) {
	case 0:
		indexItemName(idx, zhCN, it.ID)
		if zhTW := strings.TrimSpace(it.Names[i18n.LangZhTW]); zhTW != "" {
			indexItemName(idx, zhTW, it.ID)
		}
	case 1:
		pageID := it.Pages[0].ID
		indexItemName(idx, zhCN, pageID)
		if zhTW := strings.TrimSpace(it.Names[i18n.LangZhTW]); zhTW != "" {
			indexItemName(idx, zhTW, pageID)
		}
	}
}

func indexItemName(idx *catalogIndex, name, id string) {
	indexNamed(idx, idx.NameToIDs, name, id)
}

func indexNamed(idx *catalogIndex, dst map[string][]string, name, id string) {
	name = strings.TrimSpace(name)
	key := normalizeTitle(name)
	if idx == nil || dst == nil || key == "" || id == "" {
		return
	}
	for _, existing := range dst[key] {
		if existing == id {
			return
		}
	}
	dst[key] = append(dst[key], id)
	if idx.NormToOrig[key] == "" {
		idx.NormToOrig[key] = name
	}
}

// normalizeTitle folds OCR punctuation so half-width parens and dash lookalikes match the catalog.
func normalizeTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "　", "")
	s = strings.ReplaceAll(s, "一一", "-")
	s = strings.NewReplacer(
		"（", "(",
		"）", ")",
		"—", "-",
		"–", "-",
		"―", "-",
		"－", "-",
	).Replace(s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}

func (idx *catalogIndex) displayName(norm string) string {
	if idx == nil {
		return norm
	}
	if orig := idx.NormToOrig[norm]; orig != "" {
		return orig
	}
	return norm
}

func (idx *catalogIndex) matchOCR(ocr, fileCategory string) (ids []string, full string) {
	ocr = normalizeTitle(ocr)
	if idx == nil || ocr == "" {
		return nil, ""
	}
	ids, matchedName := uniquePrefixIDs(idx.NameToIDs, ocr)
	ids = idx.filterUnlockIDs(ids, fileCategory)
	if len(ids) == 0 {
		return nil, ""
	}
	if matchedName == "" {
		matchedName = ocr
	}
	return ids, idx.displayName(matchedName)
}

func (idx *catalogIndex) shouldOpenFromList(ocr, fileCategory string) bool {
	if idx == nil {
		return false
	}
	ocr = normalizeTitle(ocr)
	if ocr == "" {
		return false
	}
	itemIDs, _ := uniquePrefixIDs(idx.NameToItems, ocr)
	for _, itemID := range idx.filterUnlockIDs(itemIDs, fileCategory) {
		if idx.ItemPageCount[itemID] > 1 {
			return true
		}
	}
	unlockIDs, _ := idx.matchOCR(ocr, fileCategory)
	for _, id := range unlockIDs {
		itemID := idx.PageToItem[id]
		if itemID == "" {
			itemID = id
		}
		if idx.ItemPageCount[itemID] > 1 {
			return true
		}
	}
	return false
}

func (idx *catalogIndex) filterUnlockIDs(ids []string, fileCategory string) []string {
	if fileCategory == "" || idx == nil {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if idx.UnlockCategory[id] == fileCategory {
			out = append(out, id)
		}
	}
	return out
}

func uniquePrefixIDs(table map[string][]string, ocr string) (ids []string, matchedName string) {
	if table == nil || ocr == "" {
		return nil, ""
	}
	if exact := table[ocr]; len(exact) > 0 {
		return append([]string(nil), exact...), ocr
	}
	for name := range table {
		if !strings.HasPrefix(name, ocr) {
			continue
		}
		if matchedName == "" {
			matchedName = name
			continue
		}
		if name != matchedName {
			return nil, ""
		}
	}
	if matchedName == "" {
		return nil, ""
	}
	return append([]string(nil), table[matchedName]...), matchedName
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

// unlockItems appends unlock IDs (page IDs, or item IDs when the catalog has no pages) under the given UID.
// Missing file/dir is created. Returns newly added IDs.
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
