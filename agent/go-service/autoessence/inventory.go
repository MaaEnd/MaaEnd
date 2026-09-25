package autoessence

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"
)

const (
	componentInventory = "AutoEssenceInventory"
	inventoryFilePath  = "EssenceInventory.json"
)

// fileEssence is one levels/count row in EssenceInventory.json.
// levels are ignored for AutoEssence stock checks (词条对应即可).
type fileEssence struct {
	Levels [3]int `json:"levels"`
	Count  int    `json:"count"`
}

// fileGroup is one weapon-combo group in EssenceInventory.json.
type fileGroup struct {
	WeaponIDs []string      `json:"weapon_ids"`
	Essences  []fileEssence `json:"essences"`
}

type groupCounts struct {
	weaponIDs []string
	count     int
}

// Inventory is the AutoEssence in-memory essence stock keyed by skill combo
// (represented as the export group's weapon_ids). Levels are not used.
type Inventory struct {
	groups map[string]*groupCounts
	loaded bool
}

var current = &Inventory{}

func groupKey(weaponIDs []string) string {
	ids := append([]string(nil), weaponIDs...)
	slices.Sort(ids)
	return strings.Join(ids, "\x00")
}

func normalizeWeaponIDs(weaponIDs []string) []string {
	seen := make(map[string]struct{}, len(weaponIDs))
	out := make([]string, 0, len(weaponIDs))
	for _, id := range weaponIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Reset clears the in-memory inventory (not loaded).
func Reset() {
	current = &Inventory{}
}

// Loaded reports whether Load succeeded at least once in this process session.
func Loaded() bool {
	return current != nil && current.loaded
}

// LoadFile replaces in-memory stock from EssenceInventory.json.
// Missing file yields an empty loaded inventory. Per-group counts sum all levels.
func LoadFile(path string) error {
	if path == "" {
		path = inventoryFilePath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			current = &Inventory{
				groups: make(map[string]*groupCounts),
				loaded: true,
			}
			log.Info().
				Str("component", componentInventory).
				Str("path", path).
				Msg("inventory file missing, starting empty")
			return nil
		}
		return fmt.Errorf("read essence inventory: %w", err)
	}
	if len(raw) == 0 {
		current = &Inventory{
			groups: make(map[string]*groupCounts),
			loaded: true,
		}
		return nil
	}

	var groups []fileGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return fmt.Errorf("unmarshal essence inventory: %w", err)
	}

	inv := &Inventory{
		groups: make(map[string]*groupCounts, len(groups)),
		loaded: true,
	}
	total := 0
	for _, g := range groups {
		ids := normalizeWeaponIDs(g.WeaponIDs)
		if len(ids) == 0 {
			continue
		}
		key := groupKey(ids)
		gc := inv.groups[key]
		if gc == nil {
			gc = &groupCounts{weaponIDs: ids}
			inv.groups[key] = gc
		}
		for _, e := range g.Essences {
			if e.Count <= 0 {
				continue
			}
			gc.count += e.Count
			total += e.Count
		}
	}
	current = inv
	log.Info().
		Str("component", componentInventory).
		Str("path", path).
		Int("groups", len(inv.groups)).
		Int("count", total).
		Msg("inventory loaded")
	return nil
}

// Add increments stock for one locked essence skill combo (any levels).
func Add(weaponIDs []string, n int) {
	if current == nil {
		current = &Inventory{groups: make(map[string]*groupCounts)}
	}
	if current.groups == nil {
		current.groups = make(map[string]*groupCounts)
	}
	ids := normalizeWeaponIDs(weaponIDs)
	if len(ids) == 0 {
		log.Warn().
			Str("component", componentInventory).
			Msg("skip add: empty weapon_ids")
		return
	}
	if n < 1 {
		n = 1
	}
	key := groupKey(ids)
	gc := current.groups[key]
	if gc == nil {
		gc = &groupCounts{weaponIDs: ids}
		current.groups[key] = gc
	}
	gc.count += n
	log.Info().
		Str("component", componentInventory).
		Strs("weapon_ids", ids).
		Int("delta", n).
		Int("after", gc.count).
		Msg("inventory added")
}

// Count returns how many essences match the skill combo identified by weaponIDs.
// weaponIDs must be non-empty; only groups that contain all listed IDs are summed.
func Count(weaponIDs []string) int {
	if current == nil || current.groups == nil {
		return 0
	}
	ids := normalizeWeaponIDs(weaponIDs)
	if len(ids) == 0 {
		return 0
	}
	total := 0
	for _, gc := range current.groups {
		if !containsAll(gc.weaponIDs, ids) {
			continue
		}
		total += gc.count
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
