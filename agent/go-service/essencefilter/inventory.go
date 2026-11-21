package essencefilter

import (
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
)

type inventoryEssence struct {
	Levels [3]int
	Count  int
	Locked int
}

type inventoryGroup struct {
	WeaponIDs []string
	Essences  []inventoryEssence
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
	log.Error().Err(err).Str("component", "EssenceInventory").Msg("inventory check failed")
	return false
}

// InventoryStock is the essence stock of one account, keyed by skill combination.
// Levels are ignored: every row of a combination is summed into the same count.
type InventoryStock struct {
	groups []stockGroup
}

type stockGroup struct {
	weaponIDs []string
	count     int
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
			count += essence.Locked
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

// MissingWeapons assigns one physical essence per selected weapon. Each exact
// skill combination groups every compatible weapon under the same stock count.
func (s *InventoryStock) MissingWeapons(selected []string) []string {
	missing := normalizeWeaponIDs(selected)
	if s == nil {
		return missing
	}
	for _, group := range s.groups {
		available := group.count
		missing = slices.DeleteFunc(missing, func(id string) bool {
			if available == 0 || !slices.Contains(group.weaponIDs, id) {
				return false
			}
			available--
			return true
		})
	}
	return missing
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
