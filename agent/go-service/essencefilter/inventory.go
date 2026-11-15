package essencefilter

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/fsutil"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const inventoryExportPath = "EssenceInventory.json"

type inventoryEssence struct {
	Levels [3]int `json:"levels"`
	Count  int    `json:"count"`
	Locked int    `json:"locked"`
}

type inventoryGroup struct {
	WeaponIDs []string           `json:"weapon_ids"`
	Essences  []inventoryEssence `json:"essences"`
}

type inventoryLevelCounts struct {
	total  int
	locked int
}

type inventoryCounts struct {
	weaponIDs []string
	levels    map[[3]int]*inventoryLevelCounts
}

type inventoryState struct {
	groups map[[3]int]*inventoryCounts
}

// record aggregates one essence selected by the grid scanner.
func (s *inventoryState) record(match *matchapi.InventoryMatch, locked bool) {
	if match == nil {
		return
	}
	group := s.groups[match.SkillIDs]
	if group == nil {
		group = &inventoryCounts{levels: make(map[[3]int]*inventoryLevelCounts)}
		for _, weapon := range match.Weapons {
			group.weaponIDs = append(group.weaponIDs, weapon.InternalID)
		}
		slices.Sort(group.weaponIDs)
		s.groups[match.SkillIDs] = group
	}
	counts := group.levels[match.Levels]
	if counts == nil {
		counts = &inventoryLevelCounts{}
		group.levels[match.Levels] = counts
	}
	counts.total++
	if locked {
		counts.locked++
	}
}

func (s *inventoryState) exportGroups() ([]inventoryGroup, int, int) {
	groups := make([]inventoryGroup, 0, len(s.groups))
	for _, counts := range s.groups {
		group := inventoryGroup{WeaponIDs: counts.weaponIDs}
		for levels, levelCounts := range counts.levels {
			group.Essences = append(group.Essences, inventoryEssence{
				Levels: levels,
				Count:  levelCounts.total,
				Locked: levelCounts.locked,
			})
		}
		groups = append(groups, group)
	}
	sortInventoryGroups(groups)
	total, locked := inventoryTotals(groups)
	return groups, total, locked
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

func finishInventory(ctx *maa.Context, st *RunState) bool {
	groups, total, locked := st.Inventory.exportGroups()
	path := InventoryPath()
	if err := writeInventoryFile(path, groups); err != nil {
		return inventoryFailed(err)
	}
	log.Info().Str("component", "EssenceInventory").Str("path", path).
		Int("groups", len(groups)).Int("count", total).Int("locked", locked).Msg("inventory exported")
	reportSimpleByKey(ctx, "inventory.saved", path, len(groups), total)
	return true
}

// loadInventoryGroups reads an inventory export. A missing or empty file is an empty inventory.
func loadInventoryGroups(path string) ([]inventoryGroup, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var groups []inventoryGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// writeInventoryFile replaces the export only after the temporary file has been written,
// synced and closed. A failed write never deletes the previous export.
func writeInventoryFile(path string, groups []inventoryGroup) error {
	data, err := json.MarshalIndent(groups, "", "    ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".EssenceInventory-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err = tmp.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
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
	return stock, nil
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
