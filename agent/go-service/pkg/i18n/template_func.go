package i18n

import "fmt"

func createTFunc(key string) func(string) string {
	return func(suffix string) string {
		fullKey := key + "." + suffix
		v, found := lookupMessage(fullKey)
		if !found {
			return fullKey
		}
		return v
	}
}

func spanColorFunc(color, text string) string {
	return fmt.Sprintf(`<span style="color:%s;">%s</span>`, color, text)
}
