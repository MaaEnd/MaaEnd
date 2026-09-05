package autoessence

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/resource"
)

type skillEntry struct {
	ID int
	CN string
	TC string
	EN string
	JP string
	KR string
}

type skillCatalog struct {
	slot1ByID map[int]skillEntry
	slot2ByID map[int]skillEntry
}

type skillPoolJSON struct {
	ID int    `json:"id"`
	CN string `json:"cn"`
	TC string `json:"tc"`
	EN string `json:"en"`
	JP string `json:"jp"`
	KR string `json:"kr"`
	// Legacy fields in skill_pools.json.
	Chinese string `json:"chinese"`
	English string `json:"english"`
}

func loadSkillCatalog() (*skillCatalog, error) {
	dataDir, err := matchapi.FindDefaultDataDir()
	if err != nil {
		return nil, err
	}

	var raw struct {
		Slot1 []skillPoolJSON `json:"slot1"`
		Slot2 []skillPoolJSON `json:"slot2"`
	}
	if err := resource.ReadJsonResource(filepath.Join(dataDir, "skill_pools.json"), &raw); err != nil {
		return nil, err
	}

	catalog := &skillCatalog{
		slot1ByID: make(map[int]skillEntry, len(raw.Slot1)),
		slot2ByID: make(map[int]skillEntry, len(raw.Slot2)),
	}
	for _, item := range raw.Slot1 {
		entry := normalizeSkillEntry(item)
		catalog.slot1ByID[entry.ID] = entry
	}
	for _, item := range raw.Slot2 {
		entry := normalizeSkillEntry(item)
		catalog.slot2ByID[entry.ID] = entry
	}
	return catalog, nil
}

func normalizeSkillEntry(item skillPoolJSON) skillEntry {
	entry := skillEntry{ID: item.ID}
	entry.CN = firstNonEmpty(item.CN, item.Chinese)
	entry.TC = firstNonEmpty(item.TC, item.CN, item.Chinese)
	entry.EN = firstNonEmpty(item.EN, item.English)
	entry.JP = firstNonEmpty(item.JP, item.CN, item.Chinese)
	entry.KR = firstNonEmpty(item.KR, item.CN, item.Chinese)
	return entry
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (c *skillCatalog) slot1Entry(id int) (skillEntry, error) {
	entry, ok := c.slot1ByID[id]
	if !ok {
		return skillEntry{}, fmt.Errorf("slot1 skill id %d not found", id)
	}
	return entry, nil
}

func (c *skillCatalog) slot2Entry(id int) (skillEntry, error) {
	entry, ok := c.slot2ByID[id]
	if !ok {
		return skillEntry{}, fmt.Errorf("slot2 skill id %d not found", id)
	}
	return entry, nil
}

func skillExpectedTexts(entry skillEntry) []string {
	return uniqueNonEmptyStrings(entry.CN, entry.TC, entry.EN, entry.JP, entry.KR)
}

func combinedBaseExpectedPatterns(entries [3]skillEntry) []string {
	return uniqueNonEmptyStrings(buildCrossLocaleCombinedBaseRegexes(entries)...)
}

func buildCrossLocaleCombinedBaseRegexes(entries [3]skillEntry) []string {
	alts := [3]string{
		skillWordAlternation(entries[0]),
		skillWordAlternation(entries[1]),
		skillWordAlternation(entries[2]),
	}
	if alts[0] == "" || alts[1] == "" || alts[2] == "" {
		return nil
	}

	patterns := make([]string, 0, len(permutations3Indices()))
	for _, idxPerm := range permutations3Indices() {
		parts := []string{alts[idxPerm[0]], alts[idxPerm[1]], alts[idxPerm[2]]}
		patterns = append(patterns, ".*"+strings.Join(parts, ".*")+".*")
	}
	return patterns
}

func skillWordAlternation(entry skillEntry) string {
	texts := skillExpectedTexts(entry)
	if len(texts) == 0 {
		return ""
	}
	if len(texts) == 1 {
		return quoteOCRToken(texts[0])
	}

	parts := make([]string, 0, len(texts))
	for _, text := range texts {
		parts = append(parts, quoteOCRToken(text))
	}
	return "(?:" + strings.Join(parts, "|") + ")"
}

func quoteOCRToken(text string) string {
	escaped := regexp.QuoteMeta(text)
	if isASCIIOnly(text) {
		return "(?i:" + escaped + ")"
	}
	return escaped
}

func isASCIIOnly(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= 0x80 {
			return false
		}
	}
	return text != ""
}

func permutations3Indices() [][3]int {
	return [][3]int{
		{0, 1, 2},
		{0, 2, 1},
		{1, 0, 2},
		{1, 2, 0},
		{2, 0, 1},
		{2, 1, 0},
	}
}

func uniqueNonEmptyStrings(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}
