package essencefilter

// FilterWeaponsByConfig - 根据配置过滤武器
func FilterWeaponsByConfig(config FilterConfig) []WeaponData {
	result := []WeaponData{}

	for _, weapon := range weaponDB.Weapons {
		// 类型过滤
		if len(config.TypeIDs) > 0 {
			matched := false
			for _, typeID := range config.TypeIDs {
				if weapon.TypeID == typeID {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		// 稀有度过滤
		if config.MinRarity > 0 && weapon.Rarity < config.MinRarity {
			continue
		}
		if config.MaxRarity > 0 && weapon.Rarity > config.MaxRarity {
			continue
		}

		result = append(result, weapon)
	}

	return result
}

// ExtractSkillCombinations - 提取技能组合
func ExtractSkillCombinations(weapons []WeaponData) []SkillCombination {
	combinations := []SkillCombination{}

	for _, weapon := range weapons {
		combinations = append(combinations, SkillCombination{
			Weapon:    weapon,
			SkillsChinese: weapon.SkillsChinese,
			SkillIDs:      weapon.SkillIDs,
		})
	}

	return combinations
}
