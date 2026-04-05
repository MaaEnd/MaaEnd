package essencefilter

import (
	"strconv"
	"strings"
)

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

// futurePromisingSummaryKey 将「未来可期」按 OCR 三槽词条与等级区分。
// MatchFuturePromising 时引擎返回的 SkillIDs 恒为 0,0,0，若仍用 skillCombinationKey 会把所有未来可期合并成一行且详情错乱。
func futurePromisingSummaryKey(skills [3]string, levels [3]int) string {
	const sep = "\x1f" // unit separator，避免与技能名冲突
	var b strings.Builder
	b.WriteString("fp:")
	for i := 0; i < 3; i++ {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(strings.TrimSpace(skills[i]))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(levels[i]))
	}
	return b.String()
}
