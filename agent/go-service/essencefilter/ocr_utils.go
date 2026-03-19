package essencefilter

import (
	"strings"
	"unicode"
)

// cleanChinese keeps only Han characters.
// Used to reduce OCR noise before matching/level parsing.
func cleanChinese(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

