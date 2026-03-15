package essencefilter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// Default locale for loading weapon skills
const defaultLoadLocale = "CN"

// weaponDB holds the loaded weapon database. Written by loader; read by filter, matcher, ui.
var weaponDB WeaponDatabase

// weaponTypeToID maps weapons_output.json weapon_type string to type_id (1-5)
var weaponTypeToID = map[string]int{
	"Sword":      1,
	"Claymores":  2,
	"Polearm":    3,
	"Handcannon": 4,
	"Pistol":     4,
	"Arts Unit":  5,
	"Wand":       5,
}

// GetPoolBySlot returns the skill pool for the given slot (1, 2, or 3).
func GetPoolBySlot(slot int) []SkillPool {
	switch slot {
	case 1:
		return weaponDB.SkillPools.Slot1
	case 2:
		return weaponDB.SkillPools.Slot2
	case 3:
		return weaponDB.SkillPools.Slot3
	default:
		return nil
	}
}

// SkillNameByID returns the Chinese name for the skill ID in the given pool.
func SkillNameByID(id int, pool []SkillPool) string {
	for _, s := range pool {
		if s.ID == id {
			return s.Chinese
		}
	}
	return ""
}

func normalizeSimilarConvert(s string) string {
	cfg := GetMatcherConfig()
	for old, val := range cfg.SimilarWordMap {
		s = strings.ReplaceAll(s, old, val)
	}
	return s
}

// cleanDisplayToCanonical normalizes a display skill name to a pool canonical name and returns (canonical, poolID, true) or ("", 0, false).
func cleanDisplayToCanonical(display string, slot int, locale string) (canonical string, id int, ok bool) {
	candidate := display
	if idx := strings.Index(display, "·"); idx >= 0 {
		candidate = strings.TrimSpace(display[:idx])
	}
	if candidate == "" {
		return "", 0, false
	}
	pool := GetPoolBySlot(slot)
	if len(pool) == 0 {
		return "", 0, false
	}
	cfg := GetMatcherConfig()
	stopwords := cfg.SuffixStopwords
	if cfg.SuffixStopwordsMap != nil {
		if w, has := cfg.SuffixStopwordsMap[locale]; has {
			stopwords = w
		}
	}
	candidates := []string{candidate}
	for _, suf := range stopwords {
		if strings.HasSuffix(candidate, suf) && len(candidate) > len(suf) {
			trimmed := strings.TrimSuffix(candidate, suf)
			if trimmed != "" {
				candidates = append(candidates, trimmed)
			}
		}
	}
	normCandidate := normalizeSimilarConvert(candidate)
	if normCandidate != candidate {
		candidates = append(candidates, normCandidate)
		for _, suf := range stopwords {
			if strings.HasSuffix(normCandidate, suf) && len(normCandidate) > len(suf) {
				trimmed := strings.TrimSuffix(normCandidate, suf)
				if trimmed != "" {
					candidates = append(candidates, trimmed)
				}
			}
		}
	}
	for _, c := range candidates {
		for _, e := range pool {
			if e.Chinese == c {
				return e.Chinese, e.ID, true
			}
		}
	}
	// 池内无完整匹配时，尝试「候选以池内基名开头」的最长匹配（如「灼热伤害」→「灼热」id7，「物理伤害」→「物理」id8）
	var best struct {
		chinese string
		id      int
		length  int
	}
	for _, c := range candidates {
		for i := range pool {
			e := &pool[i]
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

// LoadSkillPoolsNew - load skill_pools.json (cn/tc/en) into weaponDB.SkillPools
func LoadSkillPoolsNew(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw struct {
		Slot1 []SkillPool `json:"slot1"`
		Slot2 []SkillPool `json:"slot2"`
		Slot3 []SkillPool `json:"slot3"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	weaponDB.SkillPools.Slot1 = raw.Slot1
	weaponDB.SkillPools.Slot2 = raw.Slot2
	weaponDB.SkillPools.Slot3 = raw.Slot3
	return nil
}

// LoadWeaponsOutputAndConvert - load weapons_output.json and convert to weaponDB.Weapons with cleaning
func LoadWeaponsOutputAndConvert(weaponsOutputPath string, locale string) error {
	data, err := os.ReadFile(weaponsOutputPath)
	if err != nil {
		return err
	}
	var raw WeaponsOutputRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if locale == "" {
		locale = defaultLoadLocale
	}
	var weapons []WeaponData
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
			log.Debug().Str("internal_id", entry.InternalID).Int("skills_len", len(skillStrs)).Msg("[EssenceFilter] skip weapon: skills count != 3")
			continue
		}
		var ids [3]int
		var canonicals [3]string
		allOk := true
		for i := 0; i < 3; i++ {
			canonical, id, ok := cleanDisplayToCanonical(skillStrs[i], i+1, locale)
			if !ok {
				log.Debug().Str("internal_id", entry.InternalID).Int("slot", i+1).Str("display", skillStrs[i]).Msg("[EssenceFilter] skip weapon: skill not resolved")
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
			InternalID:     entry.InternalID,
			ChineseName:    name,
			TypeID:         typeID,
			Rarity:         entry.Rarity,
			SkillIDs:       []int{ids[0], ids[1], ids[2]},
			SkillsChinese:  []string{canonicals[0], canonicals[1], canonicals[2]},
		})
	}
	weaponDB.Weapons = weapons
	log.Info().Str("component", "EssenceFilter").Int("weapons_loaded", len(weapons)).Str("source", "weapons_output").Msg("weapons loaded with cleaning")
	return nil
}

// LoadLocations - load locations.json (root array) into weaponDB.Locations
func LoadLocations(locationsPath string) error {
	data, err := os.ReadFile(locationsPath)
	if err != nil {
		return err
	}
	var locs []Location
	if err := json.Unmarshal(data, &locs); err != nil {
		return err
	}
	weaponDB.Locations = locs
	log.Info().Str("component", "EssenceFilter").Int("locations_loaded", len(locs)).Str("source", "locations.json").Msg("locations loaded")
	return nil
}

// LoadNewFormat - load skill_pools.json + weapons_output.json + locations.json
func LoadNewFormat(gameDataDir string) error {
	skillPoolsPath := filepath.Join(gameDataDir, "skill_pools.json")
	weaponsOutputPath := filepath.Join(gameDataDir, "weapons_output.json")
	locationsPath := filepath.Join(gameDataDir, "locations.json")
	if err := LoadSkillPoolsNew(skillPoolsPath); err != nil {
		return err
	}
	if err := LoadWeaponsOutputAndConvert(weaponsOutputPath, defaultLoadLocale); err != nil {
		return err
	}
	if err := LoadLocations(locationsPath); err != nil {
		return err
	}
	return nil
}
