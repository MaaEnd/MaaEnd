package matchapi

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

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
	// 先在原始字符串上按 "·"/"・" 截断，再对核心部分做标点规范化与英文冒号分割。
	raw := strings.TrimSpace(display)

	core := raw
	for _, sep := range []string{"·", "・"} {
		if idx := strings.Index(core, sep); idx >= 0 {
			core = strings.TrimSpace(core[:idx])
			break
		}
	}

	// 对截断后的核心部分做标点归一，保持与其他调用点一致。
	core = strings.TrimSpace(normalizePunctuation(core))

	loc := NormalizeInputLocale(locale)
	if loc == LocaleEN {
		if idx := strings.Index(core, ":"); idx >= 0 {
			core = strings.TrimSpace(core[:idx])
		}
	}

	return core
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
