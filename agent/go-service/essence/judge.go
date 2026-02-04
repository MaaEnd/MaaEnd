package essence

import "strings"

// JudgeResult 表示一次基质判定的结果。
type JudgeResult struct {
	Decision          string   `json:"decision"`            // "Treasure" / "Material"
	MatchedWeaponNames []string `json:"matchedWeaponNames"` // 命中的武器名称
	BestDungeonIDs    []string `json:"bestDungeonIds"`      // 推荐副本 ID
}

// JudgeEssence 根据基质的三条属性（s1/s2/s3）进行简单判定。
// 这里使用 endfield-essence-planner 中的 WEAPONS / DUNGEONS 作为规则基础：
// - 若存在武器 (S1,S2,S3) 完全匹配，则视作 "Treasure"
// - 否则视作 "Material"
// - 同时根据 s2/s3 推荐可刷副本（其池中同时包含该 s2/s3）
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

	// 推荐副本：s2/s3 同时在池中出现即可
	var bestDungeonIDs []string
	s2Norm := attrs[1]
	s3Norm := attrs[2]
	for _, d := range allDungeons() {
		if containsAttr(d.S2Pool, s2Norm) && containsAttr(d.S3Pool, s3Norm) {
			bestDungeonIDs = append(bestDungeonIDs, d.ID)
		}
	}

	return JudgeResult{
		Decision:          decision,
		MatchedWeaponNames: matchedNames,
		BestDungeonIDs:    bestDungeonIDs,
	}
}

func containsAttr(list []string, target string) bool {
	for _, v := range list {
		if normalizeAttr(v) == target {
			return true
		}
	}
	return false
}

func normalizeAttr(s string) string {
	return strings.TrimSpace(s)
}

