package matchapi

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func itoa(v int) string {
	return strconv.Itoa(v)
}

func cleanChinese(text string) string {
	return normalizeForMatch(text, LocaleCN)
}

// NormalizeInputForMatch normalizes OCR or pool text for matching for the given locale.
// Exported for EssenceFilter actions and tests.
func NormalizeInputForMatch(text string, locale string) string {
	return normalizeForMatch(text, locale)
}

func normalizeForMatch(text string, locale string) string {
	text = strings.TrimSpace(normalizePunctuation(text))
	loc := NormalizeInputLocale(locale)
	switch loc {
	case LocaleEN:
		return normalizeForMatchEN(text)
	case LocaleJP:
		return normalizeForMatchJP(text)
	case LocaleKR:
		return normalizeForMatchKR(text)
	case LocaleTC, LocaleCN:
		return normalizeForMatchHan(text)
	default:
		return normalizeForMatchHan(text)
	}
}

func normalizeForMatchHan(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeForMatchEN(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	for i, tok := range fields {
		fields[i] = normalizeENToken(tok)
	}
	return strings.Join(fields, " ")
}

func normalizeForMatchJP(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Han, r) ||
			(r >= 0x3040 && r <= 0x309F) || // Hiragana
			(r >= 0x30A0 && r <= 0x30FF) || // Katakana
			(r >= 0xFF66 && r <= 0xFF9F) { // Halfwidth Katakana
			b.WriteRune(r)
		}
	}
	return b.String()
}

func normalizeForMatchKR(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.Is(unicode.Han, r) || (r >= 0xAC00 && r <= 0xD7A3) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func trimStopSuffix(cfg MatcherConfig, s string, locale string) string {
	loc := NormalizeInputLocale(locale)
	if s == "" {
		return s
	}

	// EN uses token-based suffix trimming; trim repeatedly for chained tails.
	if loc == LocaleEN {
		parts := strings.Fields(normalizeForMatchEN(s))
		if len(parts) == 0 {
			return ""
		}
		changed := true
		for changed && len(parts) > 1 {
			changed = false
			last := normalizeENToken(parts[len(parts)-1])
			for _, suf := range cfg.SuffixStopwords {
				snorm := normalizeENToken(strings.TrimSpace(suf))
				if snorm == "" {
					continue
				}
				if last == snorm {
					parts = parts[:len(parts)-1]
					changed = true
					break
				}
			}
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, " ")
	}

	// CJK-like locales keep existing suffix semantics.
	for _, suf := range cfg.SuffixStopwords {
		if strings.HasSuffix(s, suf) && runeCount(s) > runeCount(suf) {
			return strings.TrimSuffix(s, suf)
		}
	}
	return s
}

func normalizeSimilarIfLocale(cfg MatcherConfig, s string, locale string) string {
	loc := NormalizeInputLocale(locale)
	if loc == LocaleCN || loc == LocaleTC {
		return normalizeSimilar(cfg, s)
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
	return utf8.RuneCountInString(s)
}

// skillCoreCandidate strips game UI suffix after a separator (e.g. rank/size) for matching.
func skillCoreCandidate(display string, locale string) string {
	display = strings.TrimSpace(normalizePunctuation(display))
	for _, sep := range []string{"·", "・"} {
		if idx := strings.Index(display, sep); idx >= 0 {
			return strings.TrimSpace(display[:idx])
		}
	}
	loc := NormalizeInputLocale(locale)
	if loc == LocaleEN {
		if idx := strings.Index(display, ":"); idx >= 0 {
			return strings.TrimSpace(display[:idx])
		}
	}
	return display
}

func normalizePunctuation(s string) string {
	repl := strings.NewReplacer(
		"：", ":", "；", ";", "，", ",", "。", ".", "！", "!", "？", "?",
		"（", "(", "）", ")", "【", "[", "】", "]", "「", "\"", "」", "\"",
		"『", "\"", "』", "\"", "　", " ",
		"·", " ", "・", " ", "/", " ", "\\", " ", "-", " ", "_", " ", "|", " ",
	)
	return repl.Replace(s)
}

func normalizeENToken(tok string) string {
	if tok == "" {
		return ""
	}
	switch tok {
	case "atk", "atq":
		return "attack"
	case "crit":
		return "critical"
	case "dmg":
		return "damage"
	case "effic":
		return "efficiency"
	case "boos":
		return "boost"
	default:
		return tok
	}
}

// exactMatchReason builds a human-readable reason for MatchExact (prefix + weapon names).
func exactMatchReason(locale string, weapons []WeaponData) string {
	loc := NormalizeInputLocale(locale)
	if len(weapons) == 0 {
		switch loc {
		case LocaleEN:
			return "Exact match"
		case LocaleTC:
			return "精準匹配"
		case LocaleJP:
			return "完全一致"
		case LocaleKR:
			return "정확 일치"
		default:
			return "精准匹配"
		}
	}
	names := make([]string, len(weapons))
	for i, w := range weapons {
		names[i] = w.ChineseName
	}
	switch loc {
	case LocaleEN:
		return "Exact match: " + strings.Join(names, ", ")
	case LocaleTC:
		return "精準匹配：" + strings.Join(names, "、")
	case LocaleJP:
		return "完全一致：" + strings.Join(names, "、")
	case LocaleKR:
		return "정확 일치: " + strings.Join(names, ", ")
	default:
		return "精准匹配：" + strings.Join(names, "、")
	}
}

func reasonNoMatch(locale string) string {
	switch NormalizeInputLocale(locale) {
	case LocaleEN:
		return "No match"
	case LocaleTC:
		return "未匹配"
	case LocaleJP:
		return "不一致"
	case LocaleKR:
		return "불일치"
	default:
		return "未匹配"
	}
}

func reasonFuturePromising(locale string, sum, min int) string {
	switch NormalizeInputLocale(locale) {
	case LocaleEN:
		return "Future-promising: total level " + itoa(sum) + " ≥ " + itoa(min)
	case LocaleTC:
		return "未來可期：總等級 " + itoa(sum) + " ≥ " + itoa(min)
	case LocaleJP:
		return "将来有望：合計レベル " + itoa(sum) + " ≥ " + itoa(min)
	case LocaleKR:
		return "미래 유망: 총 레벨 " + itoa(sum) + " ≥ " + itoa(min)
	default:
		return "未来可期：总等级 " + itoa(sum) + " ≥ " + itoa(min)
	}
}

func reasonSlot3Practical(locale string, slot3Name string, slot3Lv, minLv int) string {
	switch NormalizeInputLocale(locale) {
	case LocaleEN:
		return "Practical: slot 3 (" + slot3Name + ") level " + itoa(slot3Lv) + " ≥ " + itoa(minLv)
	case LocaleTC:
		return "實用基質：詞條3(" + slot3Name + ")等級 " + itoa(slot3Lv) + " ≥ " + itoa(minLv)
	case LocaleJP:
		return "実用：スロット3(" + slot3Name + ")レベル " + itoa(slot3Lv) + " ≥ " + itoa(minLv)
	case LocaleKR:
		return "실용: 슬롯3(" + slot3Name + ") 레벨 " + itoa(slot3Lv) + " ≥ " + itoa(minLv)
	default:
		return "实用基质：词条3(" + slot3Name + ")等级 " + itoa(slot3Lv) + " ≥ " + itoa(minLv)
	}
}
