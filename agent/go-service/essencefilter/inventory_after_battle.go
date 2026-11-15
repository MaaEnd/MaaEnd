package essencefilter

import (
	"fmt"
	"slices"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// inventoryDelta is one after-battle inventory increment: one skill combination, one
// level triple, and how many of them are held and locked.
type inventoryDelta struct {
	WeaponIDs []string
	Levels    [3]int
	Count     int
	Locked    int
}

// inventoryIncrement says what one after-battle decision adds to the inventory: holding the
// essence in this loot round, locking it in this run, or both.
type inventoryIncrement struct {
	Held   bool
	Locked bool
}

// afterBattleInventoryDelta maps one after-battle decision to an inventory increment.
// Only exact matches and kept slot-3 practical matches have a weapon combination and
// levels that EssenceInventory.json can express; future-promising, unmatched and
// discarded essences produce no entry.
func afterBattleInventoryDelta(result *matchapi.MatchResult, increment inventoryIncrement) (inventoryDelta, bool) {
	if result == nil {
		return inventoryDelta{}, false
	}
	switch result.Kind {
	case matchapi.MatchExact, matchapi.MatchSlot3Level3Practical:
	default:
		return inventoryDelta{}, false
	}

	ids := make([]string, 0, len(result.Weapons))
	for _, weapon := range result.Weapons {
		if weapon.InternalID != "" {
			ids = append(ids, weapon.InternalID)
		}
	}
	ids = normalizeWeaponIDs(ids)
	if len(ids) == 0 {
		return inventoryDelta{}, false
	}

	delta := inventoryDelta{WeaponIDs: ids, Levels: result.Levels}
	if increment.Held {
		delta.Count = 1
	}
	if increment.Locked {
		delta.Locked = 1
	}
	if delta.Count == 0 && delta.Locked == 0 {
		return inventoryDelta{}, false
	}
	return delta, true
}

// mergeInventoryGroups merges increments into existing groups. Existing groups and rows
// are kept; the result is normalized: groups sorted by weapon IDs, rows sorted by levels.
func mergeInventoryGroups(groups []inventoryGroup, deltas []inventoryDelta) []inventoryGroup {
	merged := make(map[string]*inventoryGroup, len(groups))
	for _, group := range groups {
		mergeInventoryRows(merged, group.WeaponIDs, group.Essences)
	}
	for _, delta := range deltas {
		mergeInventoryRows(merged, delta.WeaponIDs, []inventoryEssence{{
			Levels: delta.Levels,
			Count:  delta.Count,
			Locked: delta.Locked,
		}})
	}

	out := make([]inventoryGroup, 0, len(merged))
	for _, group := range merged {
		out = append(out, *group)
	}
	sortInventoryGroups(out)
	return out
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

// mergeInventoryFile merges after-battle increments into the export at path and returns
// the merged groups. A missing or empty file is merged from scratch; an unreadable or
// invalid file is left untouched.
func mergeInventoryFile(path string, deltas []inventoryDelta) ([]inventoryGroup, error) {
	existing, err := loadInventoryGroups(path)
	if err != nil {
		return nil, fmt.Errorf("load inventory: %w", err)
	}
	groups := mergeInventoryGroups(existing, deltas)
	if err := writeInventoryFile(path, groups); err != nil {
		return nil, fmt.Errorf("write inventory: %w", err)
	}
	return groups, nil
}

// commitAfterBattleInventory writes the increments of this loot run into EssenceInventory.json.
func commitAfterBattleInventory(ctx *maa.Context, st *RunState) bool {
	// 盘点模式由 Finish 覆盖写全量快照，这里不参与。
	if st.Inventory != nil || len(st.PendingInventory) == 0 {
		return true
	}
	path := InventoryPath()
	groups, err := mergeInventoryFile(path, st.PendingInventory)
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceInventory").Str("path", path).
			Msg("after-battle inventory not committed")
		reportFocusByKey(ctx, "inventory.failed")
		return false
	}
	st.PendingInventory = nil
	total, locked := inventoryTotals(groups)
	log.Info().Str("component", "EssenceInventory").Str("path", path).
		Int("groups", len(groups)).Int("count", total).Int("locked", locked).
		Msg("after-battle inventory committed")
	return true
}
