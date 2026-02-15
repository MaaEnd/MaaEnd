package essencefilter

import (
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// FilterWeaponsByConfig - 根据配置过滤武器
func FilterWeaponsByConfig(WeaponRarity []int) []WeaponData {
	result := []WeaponData{}

	for _, rarity := range WeaponRarity {
		for _, weapon := range weaponDB.Weapons {
			if weapon.Rarity == rarity {
				result = append(result, weapon)
			}
		}

	}

	return result
}

// ExtractSkillCombinations - 提取技能组合
func ExtractSkillCombinations(weapons []WeaponData) []SkillCombination {
	combinations := []SkillCombination{}

	for _, weapon := range weapons {
		combinations = append(combinations, SkillCombination{
			Weapon:        weapon,
			SkillsChinese: weapon.SkillsChinese,
			SkillIDs:      weapon.SkillIDs,
		})
	}

	return combinations
}

// logSkillPools - print all pools from DB
func logSkillPools() {
	for _, entry := range []struct {
		slot string
		pool []SkillPool
	}{
		{"Slot1", weaponDB.SkillPools.Slot1},
		{"Slot2", weaponDB.SkillPools.Slot2},
		{"Slot3", weaponDB.SkillPools.Slot3},
	} {
		for _, s := range entry.pool {
			log.Info().Str("slot", entry.slot).Int("id", s.ID).Str("skill", s.Chinese).Msg("<EssenceFilter> SkillPool")
		}
	}
}

// buildFilteredSkillStats - count skill IDs per slot after filter
func buildFilteredSkillStats(filtered []WeaponData) {
	for i := range filteredSkillStats {
		filteredSkillStats[i] = make(map[int]int)
	}
	for _, w := range filtered {
		for i, id := range w.SkillIDs {
			filteredSkillStats[i][id]++
		}
	}
}

// logFilteredSkillStats - log counts per slot
func logFilteredSkillStats() {
	for slotIdx, stat := range filteredSkillStats {
		slot := slotIdx + 1
		pool := getPoolBySlot(slot)
		ids := make([]int, 0, len(stat))
		for id := range stat {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		for _, id := range ids {
			name := skillNameByID(id, pool)
			log.Info().Int("slot", slot).Int("skill_id", id).Str("skill", name).Int("count", stat[id]).Msg("<EssenceFilter> FilteredSkillStats")
		}
	}
}

// skillCombinationKey - 将技能 ID 列表转换为稳定的 key，用于统计 map
func skillCombinationKey(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, "-")
}
