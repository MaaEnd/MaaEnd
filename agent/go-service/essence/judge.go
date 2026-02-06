package essence

import "strings"

// JudgeResult 表示一次基质判定的结果。
type JudgeResult struct {
	Decision           string   `json:"decision"`           // "Treasure" / "Material"
	MatchedWeaponNames []string `json:"matchedWeaponNames"` // 命中的武器名称
}

// JudgeEssence 根据基质的三条属性（s1/s2/s3）进行简单判定。
// 这里使用 endfield-essence-planner 中的 WEAPONS 作为规则基础：
// - 若存在武器 (S1,S2,S3) 完全匹配，则视作 "Treasure"
// - 否则视作 "Material"
func JudgeEssence(s1, s2, s3 string) JudgeResult {
	_ = EnsureDataReady() // 若失败会在日志中体现，这里仍按空数据继续

	attrs := []string{
		normalizeAttr(s1),
		normalizeAttr(s2),
		normalizeAttr(s3),
	}

	var matchedNames []string
	for _, w := range allWeapons() {
		if normalizeAttr(w.S1) == attrs[0] &&
			normalizeAttr(w.S2) == attrs[1] &&
			normalizeAttr(w.S3) == attrs[2] {
			matchedNames = append(matchedNames, w.Name)
		}
	}

	decision := "Material"
	if len(matchedNames) > 0 {
		decision = "Treasure"
	}

	return JudgeResult{
		Decision:           decision,
		MatchedWeaponNames: matchedNames,
	}
}

// JudgeEssenceWithPreferredWeapons 根据指定武器名单进行判定：
// - 若基质 (S1,S2,S3) 与任一选中武器完全一致，则视作 "Treasure"
// - 否则视作 "Material"
func JudgeEssenceWithPreferredWeapons(s1, s2, s3 string, preferredWeapons []string) JudgeResult {
	if len(preferredWeapons) == 0 {
		return JudgeEssence(s1, s2, s3)
	}

	_ = EnsureDataReady()

	attrs := []string{
		normalizeAttr(s1),
		normalizeAttr(s2),
		normalizeAttr(s3),
	}

	// 构建选中武器名的精确匹配集合
	// Build exact-match set from selected weapon names
	weaponNameSet := make(map[string]struct{}, len(preferredWeapons))
	for _, name := range preferredWeapons {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			weaponNameSet[trimmed] = struct{}{}
		}
	}

	var matchedNames []string
	for _, w := range allWeapons() {
		// 精确匹配武器名
		// Exact match on weapon name
		if _, ok := weaponNameSet[w.Name]; !ok {
			continue
		}
		if normalizeAttr(w.S1) == attrs[0] &&
			normalizeAttr(w.S2) == attrs[1] &&
			normalizeAttr(w.S3) == attrs[2] {
			matchedNames = append(matchedNames, w.Name)
		}
	}

	decision := "Material"
	if len(matchedNames) > 0 {
		decision = "Treasure"
	}

	return JudgeResult{
		Decision:           decision,
		MatchedWeaponNames: matchedNames,
	}
}

// normalizeAttr trims whitespace for stable comparisons.
// normalizeAttr 去除前后空白，保证稳定比较。
func normalizeAttr(s string) string {
	return strings.TrimSpace(s)
}