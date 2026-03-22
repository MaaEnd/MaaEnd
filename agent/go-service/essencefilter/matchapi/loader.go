package matchapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const defaultLoadLocale = LocaleCN

var weaponTypeToID = map[string]int{
	"Sword":      1,
	"Claymores":  2,
	"Polearm":    3,
	"Handcannon": 4,
	"Pistol":     4,
	"Arts Unit":  5,
	"Wand":       5,
}

func findDefaultDataDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("MAAEND_ESSENCEFILTER_DATA_DIR")); v != "" {
		if fileExists(filepath.Join(v, "matcher_config.json")) &&
			fileExists(filepath.Join(v, "skill_pools.json")) &&
			fileExists(filepath.Join(v, "weapons_output.json")) &&
			fileExists(filepath.Join(v, "locations.json")) {
			return v, nil
		}
	}

	// Try from working directory and its parents.
	wd, err := os.Getwd()
	if err == nil {
		base := wd
		for i := 0; i < 8; i++ {
			cand := filepath.Join(base, "assets", "data", "EssenceFilter")
			if hasEssenceFilterFiles(cand) {
				return cand, nil
			}
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}

	// Try from executable dir as a fallback.
	if exePath, err2 := os.Executable(); err2 == nil {
		base := filepath.Dir(exePath)
		for i := 0; i < 8; i++ {
			cand := filepath.Join(base, "assets", "data", "EssenceFilter")
			if hasEssenceFilterFiles(cand) {
				return cand, nil
			}
			parent := filepath.Dir(base)
			if parent == base {
				break
			}
			base = parent
		}
	}

	return "", errors.New("cannot resolve default EssenceFilter data dir; set MAAEND_ESSENCEFILTER_DATA_DIR")
}

// FindDefaultDataDir resolves the EssenceFilter data directory (same rules as NewDefaultEngine).
func FindDefaultDataDir() (string, error) {
	return findDefaultDataDir()
}

func hasEssenceFilterFiles(dir string) bool {
	return fileExists(filepath.Join(dir, "matcher_config.json")) &&
		fileExists(filepath.Join(dir, "skill_pools.json")) &&
		fileExists(filepath.Join(dir, "weapons_output.json")) &&
		fileExists(filepath.Join(dir, "locations.json"))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func loadMatcherConfig(dataDir string, locale string) (MatcherConfig, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, "matcher_config.json"))
	if err != nil {
		return MatcherConfig{}, err
	}

	var withRaw struct {
		DataVersion        string            `json:"data_version"`
		SimilarWordMap     map[string]string `json:"similarWordMap"`
		SuffixStopwords    json.RawMessage   `json:"suffixStopwords"`
		SuffixStopwordsMap map[string][]string
	}

	if err := json.Unmarshal(b, &withRaw); err != nil {
		// matcher_config.json uses suffixStopwords as an object/map.
		return MatcherConfig{}, err
	}

	cfg := MatcherConfig{
		DataVersion:    withRaw.DataVersion,
		SimilarWordMap: withRaw.SimilarWordMap,
	}
	if cfg.SimilarWordMap == nil {
		cfg.SimilarWordMap = make(map[string]string)
	}

	loc := NormalizeInputLocale(locale)

	// Try to parse suffixStopwords as map first.
	var stopMap map[string][]string
	if err := json.Unmarshal(withRaw.SuffixStopwords, &stopMap); err == nil && len(stopMap) > 0 {
		cfg.SuffixStopwordsMap = stopMap
		cfg.SuffixStopwords = pickSuffixStopwords(stopMap, loc)
		cfg.SuffixStopwords = normalizeStopwordsForLocale(cfg.SuffixStopwords, loc)
		return cfg, nil
	}

	// Legacy: suffixStopwords is a plain array.
	var stopArr []string
	if err := json.Unmarshal(withRaw.SuffixStopwords, &stopArr); err != nil {
		return MatcherConfig{}, err
	}
	cfg.SuffixStopwords = normalizeStopwordsForLocale(stopArr, loc)
	return cfg, nil
}

func normalizeStopwordsForLocale(in []string, locale string) []string {
	if len(in) == 0 {
		return in
	}
	loc := NormalizeInputLocale(locale)
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if loc == LocaleEN {
			s = normalizeENToken(strings.ToLower(s))
		}
		out = append(out, s)
	}
	return out
}

func pickSuffixStopwords(stopMap map[string][]string, locale string) []string {
	if w, ok := stopMap[locale]; ok && len(w) > 0 {
		return w
	}
	if w, ok := stopMap[LocaleCN]; ok && len(w) > 0 {
		return w
	}
	for _, v := range stopMap {
		if len(v) > 0 {
			return v
		}
	}
	return nil
}

type skillPoolJSON struct {
	ID      int    `json:"id"`
	English string `json:"english"`
	Chinese string `json:"chinese"`
	CN      string `json:"cn"`
	TC      string `json:"tc"`
	EN      string `json:"en"`
	JP      string `json:"jp"`
	KR      string `json:"kr"`
}

func pickSkillPoolDisplayName(s skillPoolJSON, locale string) string {
	loc := NormalizeInputLocale(locale)
	switch loc {
	case LocaleCN:
		if s.CN != "" {
			return s.CN
		}
		if s.Chinese != "" {
			return s.Chinese
		}
		if s.TC != "" {
			return s.TC
		}
	case LocaleTC:
		if s.TC != "" {
			return s.TC
		}
		if s.CN != "" {
			return s.CN
		}
		if s.Chinese != "" {
			return s.Chinese
		}
	case LocaleEN:
		if s.EN != "" {
			return s.EN
		}
		if s.English != "" {
			return s.English
		}
		if s.CN != "" {
			return s.CN
		}
	case LocaleJP:
		if s.JP != "" {
			return s.JP
		}
		if s.CN != "" {
			return s.CN
		}
		if s.EN != "" {
			return s.EN
		}
	case LocaleKR:
		if s.KR != "" {
			return s.KR
		}
		if s.CN != "" {
			return s.CN
		}
		if s.EN != "" {
			return s.EN
		}
	}
	if s.CN != "" {
		return s.CN
	}
	if s.Chinese != "" {
		return s.Chinese
	}
	return s.EN
}

func pickSkillPoolEnglish(s skillPoolJSON) string {
	if s.EN != "" {
		return s.EN
	}
	if s.English != "" {
		return s.English
	}
	return ""
}

func loadSkillPools(dataDir string, locale string) (SkillPools, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, "skill_pools.json"))
	if err != nil {
		return SkillPools{}, err
	}

	var raw struct {
		Slot1 []skillPoolJSON `json:"slot1"`
		Slot2 []skillPoolJSON `json:"slot2"`
		Slot3 []skillPoolJSON `json:"slot3"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return SkillPools{}, err
	}

	toPool := func(in []skillPoolJSON) []SkillPool {
		out := make([]SkillPool, 0, len(in))
		for _, s := range in {
			display := pickSkillPoolDisplayName(s, locale)
			en := pickSkillPoolEnglish(s)
			out = append(out, SkillPool{
				ID:      s.ID,
				English: en,
				Chinese: display, // locale display string used for matching (field name kept for JSON compat)
			})
		}
		return out
	}

	return SkillPools{
		Slot1: toPool(raw.Slot1),
		Slot2: toPool(raw.Slot2),
		Slot3: toPool(raw.Slot3),
	}, nil
}

type WeaponOutputEntry struct {
	InternalID string              `json:"internal_id"`
	WeaponType string              `json:"weapon_type"`
	Rarity     int                 `json:"rarity"`
	Names      map[string]string   `json:"names"`
	Skills     map[string][]string `json:"skills"`
}

type WeaponsOutputRaw map[string]WeaponOutputEntry

func pickLocalizedString(m map[string]string, locale string) string {
	if len(m) == 0 {
		return ""
	}
	loc := NormalizeInputLocale(locale)
	order := []string{loc, LocaleCN, LocaleEN, LocaleTC, LocaleJP, LocaleKR}
	seen := map[string]bool{}
	for _, k := range order {
		if seen[k] {
			continue
		}
		seen[k] = true
		if v := strings.TrimSpace(m[k]); v != "" {
			return v
		}
	}
	for _, v := range m {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func pickLocalizedSkillSlice(m map[string][]string, locale string) []string {
	if len(m) == 0 {
		return nil
	}
	loc := NormalizeInputLocale(locale)
	order := []string{loc, LocaleCN, LocaleEN, LocaleTC, LocaleJP, LocaleKR}
	seen := map[string]bool{}
	for _, k := range order {
		if seen[k] {
			continue
		}
		seen[k] = true
		if s := m[k]; len(s) == 3 {
			return s
		}
	}
	for _, s := range m {
		if len(s) == 3 {
			return s
		}
	}
	return nil
}

func loadWeaponsOutputAndConvert(dataDir string, cfg MatcherConfig, pools SkillPools, locale string) ([]WeaponData, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, "weapons_output.json"))
	if err != nil {
		return nil, err
	}

	var raw WeaponsOutputRaw
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}

	weapons := make([]WeaponData, 0, len(raw))
	loc := NormalizeInputLocale(locale)
	for _, entry := range raw {
		name := pickLocalizedString(entry.Names, loc)

		skillStrs := pickLocalizedSkillSlice(entry.Skills, loc)
		if len(skillStrs) != 3 {
			continue
		}

		var ids [3]int
		var canonicals [3]string
		allOk := true
		for i := 0; i < 3; i++ {
			canonical, id, ok := cleanDisplayToCanonical(
				skillStrs[i],
				i+1,
				loc,
				cfg,
				pools,
			)
			if !ok {
				allOk = false
				break
			}
			ids[i] = id
			canonicals[i] = canonical
		}
		if !allOk {
			continue
		}

		typeID := weaponTypeToID[entry.WeaponType]
		weapons = append(weapons, WeaponData{
			InternalID:    entry.InternalID,
			ChineseName:   name,
			TypeID:        typeID,
			Rarity:        entry.Rarity,
			SkillIDs:      []int{ids[0], ids[1], ids[2]},
			SkillsChinese: []string{canonicals[0], canonicals[1], canonicals[2]},
		})
	}

	return weapons, nil
}

func loadLocations(dataDir string) ([]Location, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, "locations.json"))
	if err != nil {
		return nil, err
	}

	var locs []Location
	if err := json.Unmarshal(b, &locs); err != nil {
		return nil, err
	}
	return locs, nil
}

func poolBySlot(pools SkillPools, slot int) []SkillPool {
	switch slot {
	case 1:
		return pools.Slot1
	case 2:
		return pools.Slot2
	case 3:
		return pools.Slot3
	default:
		return nil
	}
}

// cleanDisplayToCanonical normalizes a display skill name to pool canonical name.
// It returns (canonical, poolID, ok).
func cleanDisplayToCanonical(display string, slot int, locale string, cfg MatcherConfig, pools SkillPools) (canonical string, id int, ok bool) {
	loc := NormalizeInputLocale(locale)
	candidate := skillCoreCandidate(display, loc)
	if candidate == "" {
		return "", 0, false
	}

	pool := poolBySlot(pools, slot)
	if len(pool) == 0 {
		return "", 0, false
	}

	stopwords := cfg.SuffixStopwords
	if cfg.SuffixStopwordsMap != nil {
		if w, has := cfg.SuffixStopwordsMap[loc]; has && len(w) > 0 {
			stopwords = normalizeStopwordsForLocale(w, loc)
		}
	}

	candidates := []string{candidate, trimStopSuffix(cfg, candidate, loc)}

	normCandidate := candidate
	if loc == LocaleCN || loc == LocaleTC {
		normCandidate = normalizeSimilar(cfg, candidate)
	}
	if normCandidate != candidate {
		candidates = append(candidates, normCandidate)
		candidates = append(candidates, trimStopSuffix(cfg, normCandidate, loc))
	}
	// Extra EN candidate: remove lightweight suffix token directly from OCR tail.
	if loc == LocaleEN {
		for _, suf := range stopwords {
			if suf == "" {
				continue
			}
			trimmed := strings.TrimSpace(strings.TrimSuffix(strings.ToLower(candidate), " "+suf))
			if trimmed != "" && trimmed != candidate {
				candidates = append(candidates, trimmed)
			}
		}
	}

	matchOne := func(a, b string) bool {
		return normalizeForMatch(a, loc) == normalizeForMatch(b, loc)
	}
	prefixOK := func(c, poolName string) bool {
		nc := normalizeForMatch(c, loc)
		np := normalizeForMatch(poolName, loc)
		return np != "" && strings.HasPrefix(nc, np)
	}

	// Full match inside pool.
	for _, c := range candidates {
		for _, e := range pool {
			if matchOne(e.Chinese, c) {
				return e.Chinese, e.ID, true
			}
		}
	}

	// Longest prefix match fallback (rune length for CJK / mixed scripts).
	var best struct {
		chinese string
		id      int
		length  int
	}
	for _, c := range candidates {
		for _, e := range pool {
			if e.Chinese == "" {
				continue
			}
			if !prefixOK(c, e.Chinese) {
				continue
			}
			plen := runeCount(normalizeForMatch(e.Chinese, loc))
			if plen > best.length {
				best.chinese = e.Chinese
				best.id = e.ID
				best.length = plen
			}
		}
	}

	if best.length > 0 {
		return best.chinese, best.id, true
	}
	return "", 0, false
}
