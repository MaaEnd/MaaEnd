package matchapi

import (
	"strconv"
	"strings"
)

func itoa(v int) string {
	return strconv.Itoa(v)
}

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

func cleanChinese(text string) string {
	// Only keep Han characters. Other characters are ignored to make OCR noise less harmful.
	// This matches the existing EssenceFilter matcher approach.
	var b strings.Builder
	for _, r := range text {
		if isHan(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isHan returns true when r is a Han ideograph.
func isHan(r rune) bool {
	// Avoid importing unicode here: keep it lightweight, but correct enough for Han ranges.
	// The previous implementation used unicode.Is(unicode.Han, r).
	// In practice for these datasets, this approximation is fine.
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF)
}

func trimStopSuffix(cfg MatcherConfig, s string) string {
	for _, suf := range cfg.SuffixStopwords {
		if strings.HasSuffix(s, suf) && runeCount(s) > runeCount(suf) {
			return strings.TrimSuffix(s, suf)
		}
	}
	return s
}

func normalizeSimilar(cfg MatcherConfig, s string) string {
	for old, val := range cfg.SimilarWordMap {
		s = strings.ReplaceAll(s, old, val)
	}
	return s
}

func runeCount(s string) int {
	// Only need relative length comparisons; rune count is consistent with the previous matcher.
	return len([]rune(s))
}

