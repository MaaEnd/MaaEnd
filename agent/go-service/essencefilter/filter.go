package essencefilter

import (
	"strconv"
	"strings"
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

// buildFilteredSkillStats - count skill IDs per slot after filter; writes to RunState.FilteredSkillStats
func buildFilteredSkillStats(filtered []WeaponData) {
	st := getRunState()
	if st == nil {
		return
	}
	for i := range st.FilteredSkillStats {
		st.FilteredSkillStats[i] = make(map[int]int)
	}
	for _, w := range filtered {
		for i, id := range w.SkillIDs {
			st.FilteredSkillStats[i][id]++
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
