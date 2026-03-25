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

// NormalizeInputLocale accepts only canonical locale codes (CN|TC|EN|JP|KR).
// Unknown or empty values default to CN.
func NormalizeInputLocale(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return LocaleCN
	}
	u := strings.ToUpper(s)
	switch u {
	case LocaleCN:
		return LocaleCN
	case LocaleTC:
		return LocaleTC
	case LocaleEN:
		return LocaleEN
	case LocaleJP:
		return LocaleJP
	case LocaleKR:
		return LocaleKR
	default:
		return LocaleCN
	}
}
