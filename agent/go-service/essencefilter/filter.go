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
