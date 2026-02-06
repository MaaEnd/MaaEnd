package essence

import "strings"

// extractPreferredWeaponsFromFlags filters UI switch flags to selected weapon names.
// extractPreferredWeaponsFromFlags 过滤 UI 开关，得到选中的武器名。
func extractPreferredWeaponsFromFlags(flags map[string]string) []string {
	if len(flags) == 0 {
		return nil
	}
	result := make([]string, 0, len(flags))
	for name, value := range flags {
		if isTruthy(value) {
			result = append(result, name)
		}
	}
	return result
}

// isTruthy interprets MXU string values as boolean.
// isTruthy 将 MXU 字符串值解析为布尔。
func isTruthy(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	switch v {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}
