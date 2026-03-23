package matchapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

type i18nCatalog map[string]map[string]string

//go:embed i18n_messages.json
var embeddedI18nMessages []byte

func loadI18nCatalog(_ string) i18nCatalog {
	var raw i18nCatalog
	if err := json.Unmarshal(embeddedI18nMessages, &raw); err != nil {
		return i18nCatalog{}
	}
	catalog := make(i18nCatalog, len(raw))
	for key, byLocale := range raw {
		if len(byLocale) == 0 {
			continue
		}
		catalog[key] = make(map[string]string, len(byLocale))
		for loc, text := range byLocale {
			nloc := NormalizeInputLocale(strings.TrimSpace(loc))
			catalog[key][nloc] = text
		}
	}
	return catalog
}

func (e *Engine) msg(key string) string {
	return lookupI18nText(e.i18n, e.locale, key)
}

func (e *Engine) msgf(key string, args ...any) string {
	return fmt.Sprintf(e.msg(key), args...)
}

func lookupI18nText(c i18nCatalog, locale string, key string) string {
	if c == nil {
		return key
	}
	byLocale, ok := c[key]
	if !ok || len(byLocale) == 0 {
		return key
	}
	loc := NormalizeInputLocale(locale)
	if v, ok := byLocale[loc]; ok && v != "" {
		return v
	}
	if v, ok := byLocale[LocaleCN]; ok && v != "" {
		return v
	}
	if v, ok := byLocale[LocaleEN]; ok && v != "" {
		return v
	}
	for _, v := range byLocale {
		if v != "" {
			return v
		}
	}
	return key
}

func joinByLocale(locale string, names []string) string {
	if NormalizeInputLocale(locale) == LocaleEN || NormalizeInputLocale(locale) == LocaleKR {
		return strings.Join(names, ", ")
	}
	return strings.Join(names, "、")
}

func (e *Engine) exactMatchReason(weapons []WeaponData) string {
	if len(weapons) == 0 {
		return e.msg("reason.exact.no_weapons")
	}
	names := make([]string, len(weapons))
	for i, w := range weapons {
		names[i] = w.ChineseName
	}
	return e.msgf("reason.exact.with_weapons", joinByLocale(e.locale, names))
}

func (e *Engine) reasonNoMatch() string {
	return e.msg("reason.no_match")
}

func (e *Engine) reasonFuturePromising(sum, min int) string {
	return e.msgf("reason.future_promising", sum, min)
}

func (e *Engine) reasonSlot3Practical(slot3Name string, slot3Lv, minLv int) string {
	return e.msgf("reason.slot3_practical", slot3Name, slot3Lv, minLv)
}

func (e *Engine) FocusOCRSkills(skills []string, levels [3]int) string {
	return e.msgf("focus.ocr_skills", skills[0], levels[0], skills[1], levels[1], skills[2], levels[2])
}

func (e *Engine) FocusMatchedWeapons(weaponsHTML string) string {
	return e.msgf("focus.matched_weapons", weaponsHTML)
}

func (e *Engine) FocusExtRuleLock(reasonHTML string) string {
	return e.msgf("focus.ext_rule_lock", reasonHTML)
}

func (e *Engine) FocusExtRuleNoop(reasonHTML string) string {
	return e.msgf("focus.ext_rule_noop", reasonHTML)
}

func (e *Engine) FocusNoMatchDiscard() string {
	return e.msg("focus.no_match_discard")
}

func (e *Engine) FocusNoMatchSkip() string {
	return e.msg("focus.no_match_skip")
}
