package essencefilter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const inventoryExportPath = "EssenceInventory.json"

type inventoryEssence struct {
	Levels [3]int `json:"levels"`
	Count  int    `json:"count"`
}

type inventoryGroup struct {
	WeaponIDs []string           `json:"weapon_ids"`
	Essences  []inventoryEssence `json:"essences"`
}

type inventoryCounts struct {
	weaponIDs []string
	levels    map[[3]int]int
}

type inventoryState struct {
	groups map[[3]int]*inventoryCounts
}

// record aggregates one essence selected by the grid scanner.
func (s *inventoryState) record(match *matchapi.InventoryMatch) {
	if match == nil {
		return
	}
	group := s.groups[match.SkillIDs]
	if group == nil {
		group = &inventoryCounts{levels: make(map[[3]int]int)}
		for _, weapon := range match.Weapons {
			group.weaponIDs = append(group.weaponIDs, weapon.InternalID)
		}
		slices.Sort(group.weaponIDs)
		s.groups[match.SkillIDs] = group
	}
	group.levels[match.Levels]++
}

func (s *inventoryState) exportGroups() ([]inventoryGroup, int) {
	groups := make([]inventoryGroup, 0, len(s.groups))
	total := 0
	for _, counts := range s.groups {
		group := inventoryGroup{WeaponIDs: counts.weaponIDs}
		for levels, count := range counts.levels {
			group.Essences = append(group.Essences, inventoryEssence{Levels: levels, Count: count})
			total += count
		}
		slices.SortFunc(group.Essences, func(a, b inventoryEssence) int {
			return slices.Compare(a.Levels[:], b.Levels[:])
		})
		groups = append(groups, group)
	}
	slices.SortFunc(groups, func(a, b inventoryGroup) int {
		return slices.Compare(a.WeaponIDs, b.WeaponIDs)
	})
	return groups, total
}

func inventoryFailed(err error) bool {
	log.Error().Err(err).Str("component", "EssenceInventory").Msg("inventory not exported")
	return false
}

func finishInventory(ctx *maa.Context, st *RunState) bool {
	groups, total := st.Inventory.exportGroups()
	path, err := filepath.Abs(inventoryExportPath)
	if err == nil {
		err = writeInventoryFile(path, groups)
	}
	if err != nil {
		return inventoryFailed(err)
	}
	log.Info().Str("component", "EssenceInventory").Str("path", path).
		Int("groups", len(groups)).Int("count", total).Msg("inventory exported")
	reportSimpleByKey(ctx, "inventory.saved", path, len(groups), total)
	return true
}

func writeInventoryFile(path string, groups []inventoryGroup) error {
	data, err := json.MarshalIndent(groups, "", "    ")
	if err != nil {
		return err
	}
	// Follow the existing snapshot writers: replace only after the temporary
	// file has been written, synced and closed. Never delete the previous export.
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
