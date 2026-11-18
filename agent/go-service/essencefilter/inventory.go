package essencefilter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	"github.com/rs/zerolog/log"
)

const (
	inventoryExportPath     = "EssenceInventory.json"
	inventoryFullExportPath = "EssenceInventoryFull.json"
)

type inventoryEssence struct {
	Levels [3]int `json:"levels"`
	Count  int    `json:"count"`
	Locked int    `json:"locked"`
}

type inventoryGroup struct {
	WeaponIDs []string           `json:"weapon_ids"`
	Essences  []inventoryEssence `json:"essences"`
}

// sortInventoryGroups orders rows by levels and groups by weapon IDs.
func sortInventoryGroups(groups []inventoryGroup) {
	for i := range groups {
		slices.SortFunc(groups[i].Essences, func(a, b inventoryEssence) int {
			return slices.Compare(a.Levels[:], b.Levels[:])
		})
	}
	slices.SortFunc(groups, func(a, b inventoryGroup) int {
		return slices.Compare(a.WeaponIDs, b.WeaponIDs)
	})
}

func inventoryTotals(groups []inventoryGroup) (int, int) {
	total, locked := 0, 0
	for _, group := range groups {
		for _, essence := range group.Essences {
			total += essence.Count
			locked += essence.Locked
		}
	}
	return total, locked
}

// normalizeWeaponIDs trims, dedupes and sorts weapon IDs so that one skill combination
// always maps to a single group key.
func normalizeWeaponIDs(weaponIDs []string) []string {
	seen := make(map[string]struct{}, len(weaponIDs))
	ids := make([]string, 0, len(weaponIDs))
	for _, id := range weaponIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func inventoryFailed(err error) bool {
	log.Error().Err(err).Str("component", "EssenceInventory").Msg("inventory not exported")
	return false
}

// InventoryPath is where EssenceInventory.json lives: <user data>/debug/record.
func InventoryPath() string {
	return fsutil.UserDataPath("debug", "record", inventoryExportPath)
}

// loadInventoryGroups reads an inventory export. A missing or empty file is an empty inventory.
func loadInventoryGroups(path string) ([]inventoryGroup, error) {
	groups, err := loadInventoryGroupsRequired(path)
	switch {
	case err == nil:
		return groups, nil
	case errors.Is(err, os.ErrNotExist), errors.Is(err, errInventoryEmpty):
		return nil, nil
	default:
		return nil, err
	}
}

var errInventoryEmpty = errors.New("inventory export is empty")

// loadInventoryGroupsRequired reads an inventory export, failing when it is missing, empty or malformed.
// The lock-limit filter must not degrade into "lock nothing" when its ranking source is gone.
func loadInventoryGroupsRequired(path string) ([]inventoryGroup, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: %s", errInventoryEmpty, path)
	}
	var groups *[]inventoryGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, fmt.Errorf("parse inventory export %s: %w", path, err)
	}
	if groups == nil {
		return nil, fmt.Errorf("%w: %s", errInventoryEmpty, path)
	}
	return *groups, nil
}

// writeInventoryFile replaces the export only after the temporary file has been written,
// synced and closed. A failed write never deletes the previous export.
func writeInventoryFile(path string, groups []inventoryGroup) error {
	return writeInventoryJSON(path, groups)
}

// InventoryStock is the essence stock of one inventory export, keyed by skill combination.
// Levels are ignored: every row of a combination is summed into the same count.
type InventoryStock struct {
	groups []stockGroup
}

type stockGroup struct {
	weaponIDs []string
	count     int
}

// LoadInventoryStock reads EssenceInventory.json; a missing file yields an empty stock.
func LoadInventoryStock(path string) (*InventoryStock, error) {
	groups, err := loadInventoryGroups(path)
	if err != nil {
		return nil, err
	}
	return stockFromGroups(groups), nil
}

func stockFromGroups(groups []inventoryGroup) *InventoryStock {
	stock := &InventoryStock{}
	for _, group := range groups {
		ids := normalizeWeaponIDs(group.WeaponIDs)
		if len(ids) == 0 {
			continue
		}
		count := 0
		for _, essence := range group.Essences {
			if essence.Count > 0 {
				count += essence.Count
			}
		}
		stock.groups = append(stock.groups, stockGroup{weaponIDs: ids, count: count})
	}
	return stock
}

// Count returns how many held essences match the combination identified by weaponIDs.
// Only groups containing all listed IDs are summed; empty weaponIDs match nothing.
func (s *InventoryStock) Count(weaponIDs []string) int {
	if s == nil {
		return 0
	}
	ids := normalizeWeaponIDs(weaponIDs)
	if len(ids) == 0 {
		return 0
	}
	total := 0
	for _, group := range s.groups {
		if containsAll(group.weaponIDs, ids) {
			total += group.count
		}
	}
	return total
}

func containsAll(have, need []string) bool {
	if len(need) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(have))
	for _, id := range have {
		set[id] = struct{}{}
	}
	for _, id := range need {
		if _, ok := set[id]; !ok {
			return false
		}
	}
	return true
}

// mergeInventoryRows adds rows to the group identified by weaponIDs, creating the group
// and its rows on demand.
func mergeInventoryRows(merged map[string]*inventoryGroup, weaponIDs []string, essences []inventoryEssence) {
	ids := normalizeWeaponIDs(weaponIDs)
	if len(ids) == 0 {
		return
	}
	key := strings.Join(ids, "\x00")
	group := merged[key]
	if group == nil {
		group = &inventoryGroup{WeaponIDs: ids}
		merged[key] = group
	}
	for _, essence := range essences {
		idx := slices.IndexFunc(group.Essences, func(row inventoryEssence) bool {
			return row.Levels == essence.Levels
		})
		if idx < 0 {
			group.Essences = append(group.Essences, essence)
			continue
		}
		group.Essences[idx].Count += essence.Count
		group.Essences[idx].Locked += essence.Locked
	}
}
