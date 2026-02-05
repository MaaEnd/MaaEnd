package essence

import "strings"

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

func isTruthy(value string) bool {
	v := strings.TrimSpace(strings.ToLower(value))
	switch v {
	case "yes", "true", "1", "on":
		return true
	default:
		return false
	}
}
