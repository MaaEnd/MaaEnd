package matchapi

import "strings"

// Input locales supported by EssenceFilter data and matching.
const (
	LocaleCN = "CN"
	LocaleTC = "TC"
	LocaleEN = "EN"
	LocaleJP = "JP"
	LocaleKR = "KR"
)

// NormalizeInputLocale maps UI / attach strings to a canonical locale code (CN|TC|EN|JP|KR).
// Unknown or empty values default to CN for backward compatibility.
func NormalizeInputLocale(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return LocaleCN
	}
	u := strings.ToUpper(s)
	u = strings.ReplaceAll(u, "-", "_")
	switch u {
	case "CN", "ZH_CN", "ZHS", "CHS":
		return LocaleCN
	case "TC", "ZH_TW", "ZH_HK", "ZHT", "CHT":
		return LocaleTC
	case "EN", "EN_US", "ENG":
		return LocaleEN
	case "JP", "JA", "JA_JP", "JPN":
		return LocaleJP
	case "KR", "KO", "KO_KR", "KOR":
		return LocaleKR
	default:
		return LocaleCN
	}
}
