package matchapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const defaultLoadLocale = "CN"

var weaponTypeToID = map[string]int{
	"Sword":      1,
	"Claymores": 2,
	"Polearm":    3,
	"Handcannon": 4,
	"Pistol":     4,
	"Arts Unit": 5,
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

func loadMatcherConfig(dataDir string) (MatcherConfig, error) {
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

	// Try to parse suffixStopwords as map first.
	var stopMap map[string][]string
	if err := json.Unmarshal(withRaw.SuffixStopwords, &stopMap); err == nil && len(stopMap) > 0 {
		cfg.SuffixStopwordsMap = stopMap
		if cn, ok := stopMap[defaultLoadLocale]; ok && len(cn) > 0 {
			cfg.SuffixStopwords = cn
		} else {
			// Fallback: take the first non-empty value.
			for _, v := range stopMap {
				if len(v) > 0 {
					cfg.SuffixStopwords = v
					break
				}
			}
		}
		return cfg, nil
	}

	// Legacy: suffixStopwords is a plain array.
	var stopArr []string
	if err := json.Unmarshal(withRaw.SuffixStopwords, &stopArr); err != nil {
		return MatcherConfig{}, err
	}
	cfg.SuffixStopwords = stopArr
	return cfg, nil
}

type skillPoolJSON struct {
	ID      int    `json:"id"`
	English string `json:"english"`
	Chinese string `json:"chinese"`
	CN      string `json:"cn"`
	TC      string `json:"tc"`
	EN      string `json:"en"`
}

func loadSkillPools(dataDir string) (SkillPools, error) {
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
			ch := s.Chinese
			if ch == "" {
				ch = s.CN
			}
			if ch == "" {
				ch = s.TC
			}

			en := s.English
			if en == "" {
				en = s.EN
			}
			out = append(out, SkillPool{
				ID:      s.ID,
				English: en,
				Chinese: ch,
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

func loadWeaponsOutputAndConvert(dataDir string, cfg MatcherConfig, pools SkillPools) ([]WeaponData, error) {
	b, err := os.ReadFile(filepath.Join(dataDir, "weapons_output.json"))
	if err != nil {
		return nil, err
	}

	var raw WeaponsOutputRaw
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}

	weapons := make([]WeaponData, 0, len(raw))
	locale := defaultLoadLocale
	for _, entry := range raw {
		name := entry.Names[locale]
		if name == "" {
			name = entry.Names["CN"]
		}

		skillStrs := entry.Skills[locale]
		if len(skillStrs) == 0 {
			skillStrs = entry.Skills["CN"]
		}
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
				locale,
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
	candidate := display
	if idx := strings.Index(display, "·"); idx >= 0 {
		candidate = strings.TrimSpace(display[:idx])
	}
	if candidate == "" {
		return "", 0, false
	}

	pool := poolBySlot(pools, slot)
	if len(pool) == 0 {
		return "", 0, false
	}

	stopwords := cfg.SuffixStopwords
	if cfg.SuffixStopwordsMap != nil {
		if w, has := cfg.SuffixStopwordsMap[locale]; has && len(w) > 0 {
			stopwords = w
		}
	}

	candidates := []string{candidate}
	for _, suf := range stopwords {
		if strings.HasSuffix(candidate, suf) && runeCount(candidate) > runeCount(suf) {
			trimmed := strings.TrimSuffix(candidate, suf)
			if trimmed != "" {
				candidates = append(candidates, trimmed)
			}
		}
	}

	normCandidate := normalizeSimilar(cfg, candidate)
	if normCandidate != candidate {
		candidates = append(candidates, normCandidate)
		for _, suf := range stopwords {
			if strings.HasSuffix(normCandidate, suf) && runeCount(normCandidate) > runeCount(suf) {
				trimmed := strings.TrimSuffix(normCandidate, suf)
				if trimmed != "" {
					candidates = append(candidates, trimmed)
				}
			}
		}
	}

	// Full match inside pool.
	for _, c := range candidates {
		for _, e := range pool {
			if e.Chinese == c {
				return e.Chinese, e.ID, true
			}
		}
	}

	// Longest prefix match fallback.
	var best struct {
		chinese string
		id      int
		length  int
	}
	for _, c := range candidates {
		for _, e := range pool {
			if e.Chinese != "" && strings.HasPrefix(c, e.Chinese) && len(e.Chinese) > best.length {
				best.chinese = e.Chinese
				best.id = e.ID
				best.length = len(e.Chinese)
			}
		}
	}

	if best.length > 0 {
		return best.chinese, best.id, true
	}
	return "", 0, false
}

