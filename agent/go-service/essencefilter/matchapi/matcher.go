package matchapi

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

type skillEntry struct {
	ID            int
	RawFull       string
	RawCore       string
	NormFull      string
	NormCore      string
	RawLen        int
	NormLen       int
	FirstCharRaw  string
	LastCharRaw   string
	FirstCharNorm string
	LastCharNorm  string
}

// slotIndex is a prebuilt lookup index for one slot.
type slotIndex struct {
	rawFullIndex  map[string][]int
	rawCoreIndex  map[string][]int
	normFullIndex map[string][]int
	normCoreIndex map[string][]int

	firstCharRaw  map[string][]int
	lastCharRaw   map[string][]int
	firstCharNorm map[string][]int
	lastCharNorm  map[string][]int

	entries []skillEntry
}

func (e *Engine) ensureSlotIndices() {
	e.slotIndicesOnce.Do(func() {
		e.buildSlotIndices()
	})
}

func (e *Engine) buildSlotIndices() {
	for i := 0; i < 3; i++ {
		slot := i + 1
		pool := e.poolBySlot(slot)
		idx := slotIndex{
			rawFullIndex:  make(map[string][]int),
			rawCoreIndex:  make(map[string][]int),
			normFullIndex: make(map[string][]int),
			normCoreIndex: make(map[string][]int),
			firstCharRaw:  make(map[string][]int),
			lastCharRaw:   make(map[string][]int),
			firstCharNorm: make(map[string][]int),
			lastCharNorm:  make(map[string][]int),
		}

		for _, s := range pool {
			rawFull := normalizeForMatch(s.Chinese, e.locale)
			rawCore := trimStopSuffix(e.cfg, rawFull, e.locale)

			normFull := normalizeSimilarIfLocale(e.cfg, rawFull, e.locale)
			normCore := trimStopSuffix(e.cfg, normFull, e.locale)

			ent := skillEntry{
				ID:            s.ID,
				RawFull:       rawFull,
				RawCore:       rawCore,
				NormFull:      normFull,
				NormCore:      normCore,
				RawLen:        runeCount(rawFull),
				NormLen:       runeCount(normFull),
				FirstCharRaw:  firstChar(rawFull),
				LastCharRaw:   lastChar(rawFull),
				FirstCharNorm: firstChar(normFull),
				LastCharNorm:  lastChar(normFull),
			}

			if ent.FirstCharRaw != "" {
				idx.firstCharRaw[ent.FirstCharRaw] = append(idx.firstCharRaw[ent.FirstCharRaw], s.ID)
			}
			if ent.LastCharRaw != "" {
				idx.lastCharRaw[ent.LastCharRaw] = append(idx.lastCharRaw[ent.LastCharRaw], s.ID)
			}
			if ent.FirstCharNorm != "" {
				idx.firstCharNorm[ent.FirstCharNorm] = append(idx.firstCharNorm[ent.FirstCharNorm], s.ID)
			}
			if ent.LastCharNorm != "" {
				idx.lastCharNorm[ent.LastCharNorm] = append(idx.lastCharNorm[ent.LastCharNorm], s.ID)
			}

			idx.entries = append(idx.entries, ent)
			idx.rawFullIndex[rawFull] = append(idx.rawFullIndex[rawFull], s.ID)
			idx.rawCoreIndex[rawCore] = append(idx.rawCoreIndex[rawCore], s.ID)
			idx.normFullIndex[normFull] = append(idx.normFullIndex[normFull], s.ID)
			idx.normCoreIndex[normCore] = append(idx.normCoreIndex[normCore], s.ID)
		}

		e.slotIdx[i] = idx
	}
}

func firstChar(s string) string {
	r := []rune(s)
	if len(r) == 2 {
		return string(r[0])
	}
	return ""
}

func lastChar(s string) string {
	r := []rune(s)
	if len(r) == 2 {
		return string(r[1])
	}
	return ""
}

func (e *Engine) poolBySlot(slot int) []SkillPool {
	return poolBySlot(e.data.SkillPools, slot)
}

func (e *Engine) skillNameByID(id int, pool []SkillPool) string {
	for _, s := range pool {
		if s.ID == id {
			return s.Chinese
		}
	}
	return ""
}

// matchEssenceSkills matches one OCR input to an exact target skill combination.
func (e *Engine) matchEssenceSkills(ocrSkills [3]string, targets []SkillCombination) (*SkillCombinationMatch, bool) {
	e.ensureSlotIndices()

	var ocrIDs [3]int
	for i, skill := range ocrSkills {
		id, ok := e.matchSkillIDEnhanced(i+1, skill)
		if !ok {
			return nil, false
		}
		ocrIDs[i] = id
	}

	var matchedWeapons []WeaponData
	var skillIDs []int
	var skillsChinese []string

	for _, combination := range targets {
		if len(combination.SkillIDs) == 3 &&
			ocrIDs[0] == combination.SkillIDs[0] &&
			ocrIDs[1] == combination.SkillIDs[1] &&
			ocrIDs[2] == combination.SkillIDs[2] {
			if len(matchedWeapons) == 0 {
				skillIDs = append([]int(nil), combination.SkillIDs...)
				skillsChinese = append([]string(nil), combination.SkillsChinese...)
			}
			matchedWeapons = append(matchedWeapons, combination.Weapon)
		}
	}

	if len(matchedWeapons) == 0 {
		return nil, false
	}

	return &SkillCombinationMatch{
		SkillIDs:      skillIDs,
		SkillsChinese: skillsChinese,
		Weapons:       matchedWeapons,
	}, true
}

func (e *Engine) matchFuturePromising(ocrSkills [3]string, levels [3]int, minTotal int) bool {
	if minTotal <= 0 {
		return false
	}
	for i, s := range ocrSkills {
		if s == "" {
			return false
		}
		if levels[i] < 1 {
			return false
		}
	}
	sum := levels[0] + levels[1] + levels[2]
	return sum >= minTotal
}

func (e *Engine) matchSlot3Level3Practical(ocrSkills [3]string, levels [3]int, minLevel int) (match *SkillCombinationMatch, slot3Level int, ok bool) {
	if minLevel <= 0 {
		return nil, 0, false
	}
	e.ensureSlotIndices()

	pool := e.poolBySlot(3)
	if len(pool) == 0 {
		return nil, 0, false
	}

	for i := 0; i < 3; i++ {
		id, matched := e.matchSkillIDEnhanced(3, ocrSkills[i])
		if !matched {
			continue
		}

		slot3Chinese := e.skillNameByID(id, pool)
		if slot3Chinese == "" {
			slot3Chinese = ocrSkills[i]
		}

		if levels[i] >= minLevel {
			// Put matched slot3 into SkillsChinese[2].
			// Other positions follow OCR order excluding i.
			skillsChinese := make([]string, 3)
			idx := 0
			for j := 0; j < 3; j++ {
				if j == i {
					continue
				}
				skillsChinese[idx] = ocrSkills[j]
				idx++
			}
			skillsChinese[2] = slot3Chinese

			return &SkillCombinationMatch{
				SkillIDs:      []int{0, 0, id},
				SkillsChinese: skillsChinese,
				Weapons:       []WeaponData{},
			}, levels[i], true
		}
	}
	return nil, 0, false
}

// matchSkillIDEnhanced is OCR text -> skill id (with raw matching, then similar-word normalized matching).
func (e *Engine) matchSkillIDEnhanced(slot int, ocrText string) (int, bool) {
	idx := e.slotIdx[slot-1]

	cleanedRaw := normalizeForMatch(ocrText, e.locale)
	if cleanedRaw == "" {
		e.traceMatch(slot, ocrText, "", "", "", "empty")
		return 0, false
	}
	coreRaw := trimStopSuffix(e.cfg, cleanedRaw, e.locale)

	if id, stage, ok := attemptMatch(e, "raw", cleanedRaw, coreRaw, idx); ok {
		e.traceMatch(slot, ocrText, cleanedRaw, coreRaw, stage, "ok")
		return id, true
	}

	cleanedNorm := normalizeSimilarIfLocale(e.cfg, cleanedRaw, e.locale)
	coreNorm := trimStopSuffix(e.cfg, cleanedNorm, e.locale)

	if id, stage, ok := attemptMatch(e, "norm", cleanedNorm, coreNorm, idx); ok {
		e.traceMatch(slot, ocrText, cleanedNorm, coreNorm, stage, "ok")
		return id, true
	}

	e.traceMatch(slot, ocrText, cleanedNorm, coreNorm, "none", "miss")
	return 0, false
}

func attemptMatch(e *Engine, phase string, cleaned string, core string, idx slotIndex) (int, string, bool) {
	useNorm := phase == "norm"

	var fullIndex, coreIndex map[string][]int
	var firstChar, lastChar map[string][]int
	if useNorm {
		fullIndex, coreIndex = idx.normFullIndex, idx.normCoreIndex
		firstChar, lastChar = idx.firstCharNorm, idx.lastCharNorm
	} else {
		fullIndex, coreIndex = idx.rawFullIndex, idx.rawCoreIndex
		firstChar, lastChar = idx.firstCharRaw, idx.lastCharRaw
	}

	cLen := runeCount(cleaned)
	coreLen := runeCount(core)
	lenDeltaLimit := fuzzyLenDeltaLimit(e.locale)

	// 1) Full exact.
	if ids, ok := fullIndex[cleaned]; ok && len(ids) > 0 {
		return ids[0], phase + ":full_exact", true
	}
	// 2) Core exact.
	if ids, ok := coreIndex[core]; ok && len(ids) > 0 {
		return ids[0], phase + ":core_exact", true
	}
	// 3) Full substring bidirectional.
	for _, ent := range idx.entries {
		tFull := ent.RawFull
		tLen := ent.RawLen
		if useNorm {
			tFull = ent.NormFull
			tLen = ent.NormLen
		}
		if abs(tLen-cLen) > lenDeltaLimit {
			continue
		}
		if strings.Contains(tFull, cleaned) || strings.Contains(cleaned, tFull) {
			return ent.ID, phase + ":full_substr", true
		}
	}
	// 4) Core substring bidirectional.
	if core != "" {
		for _, ent := range idx.entries {
			tCore := ent.RawCore
			tLen := ent.RawLen
			if useNorm {
				tCore = ent.NormCore
				tLen = ent.NormLen
			}
			if abs(tLen-coreLen) > lenDeltaLimit {
				continue
			}
			if strings.Contains(tCore, core) || strings.Contains(core, tCore) {
				return ent.ID, phase + ":core_substr", true
			}
		}
	}

	// 5) EN token-prefix fallback with high confidence.
	if NormalizeInputLocale(e.locale) == LocaleEN && cleaned != "" {
		bestID := 0
		bestScore := 0
		for _, ent := range idx.entries {
			target := ent.RawFull
			if useNorm {
				target = ent.NormFull
			}
			if score, ok := englishTokenPrefixScore(cleaned, target); ok {
				if score > bestScore {
					bestScore = score
					bestID = ent.ID
				}
			}
		}
		if bestID != 0 && bestScore >= 2 {
			return bestID, phase + ":en_token_prefix", true
		}
	}

	// 6) Single-char fallback (only when cleaned length == 1).
	if cLen == 1 {
		if ids := firstChar[cleaned]; len(ids) == 1 {
			return ids[0], phase + ":single_char_head", true
		}
		if ids := lastChar[cleaned]; len(ids) == 1 {
			return ids[0], phase + ":single_char_tail", true
		}
	}

	// 7) Edit distance fallback.
	// If matched by stop-suffix trimming (core != cleaned), prefer core distance.
	if core != "" && core != cleaned {
		maxEdCore := maxEditDistanceForLocale(e.locale, coreLen)
		bestID := 0
		bestDist := maxEdCore + 1
		for _, ent := range idx.entries {
			tCore := ent.RawCore
			if useNorm {
				tCore = ent.NormCore
			}
			dist := editDistance(core, tCore, maxEdCore)
			if dist <= maxEdCore && dist < bestDist {
				bestID, bestDist = ent.ID, dist
			}
		}
		if bestID != 0 {
			return bestID, fmt.Sprintf("%s:core_ed%d", phase, bestDist), true
		}
		return 0, phase + ":core_ed_miss", false
	}

	// Core didn't change; use full string edit distance.
	maxEd := maxEditDistanceForLocale(e.locale, cLen)
	bestID := 0
	bestDist := maxEd + 1
	for _, ent := range idx.entries {
		tFull := ent.RawFull
		if useNorm {
			tFull = ent.NormFull
		}
		dist := editDistance(cleaned, tFull, maxEd)
		if dist <= maxEd && dist < bestDist {
			bestID, bestDist = ent.ID, dist
		}
	}
	if bestID != 0 {
		return bestID, fmt.Sprintf("%s:full_ed%d", phase, bestDist), true
	}
	return 0, phase + ":full_ed_miss", false
}

func maxEditDistanceForLocale(locale string, l int) int {
	switch NormalizeInputLocale(locale) {
	case LocaleEN:
		switch {
		case l >= 12:
			return 3
		case l >= 6:
			return 2
		default:
			return 1
		}
	default:
		if l >= 4 {
			return 2
		}
		return 1
	}
}

func fuzzyLenDeltaLimit(locale string) int {
	if NormalizeInputLocale(locale) == LocaleEN {
		return 5
	}
	return 2
}

func englishTokenPrefixScore(a string, b string) (int, bool) {
	at := strings.Fields(a)
	bt := strings.Fields(b)
	if len(at) == 0 || len(bt) == 0 {
		return 0, false
	}
	// Require first token to be close (prefix in either direction).
	a0, b0 := at[0], bt[0]
	if len(a0) < 3 || len(b0) < 3 {
		return 0, false
	}
	if !(strings.HasPrefix(a0, b0) || strings.HasPrefix(b0, a0)) {
		return 0, false
	}
	score := 1
	minLen := len(at)
	if len(bt) < minLen {
		minLen = len(bt)
	}
	for i := 1; i < minLen; i++ {
		if at[i] == bt[i] || strings.HasPrefix(at[i], bt[i]) || strings.HasPrefix(bt[i], at[i]) {
			score++
		}
	}
	return score, true
}

func (e *Engine) traceMatch(slot int, ocrRaw string, cleaned string, core string, stage string, result string) {
	if e == nil || !e.matchTraceEnabled {
		return
	}
	log.Debug().
		Str("component", "EssenceFilterMatch").
		Str("locale", e.locale).
		Int("slot", slot).
		Str("ocr", ocrRaw).
		Str("cleaned", cleaned).
		Str("core", core).
		Str("stage", stage).
		Str("result", result).
		Msg("skill match trace")
}

// Damerau-Levenshtein with early stop by max.
func editDistance(a, b string, max int) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if abs(la-lb) > max {
		return max + 1
	}
	// dp dimensions: (la+1) x (lb+1)
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
	}
	for i := 0; i <= la; i++ {
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			cost := 0
			if ra[i-1] != rb[j-1] {
				cost = 1
			}
			dp[i][j] = min3(
				dp[i-1][j]+1,
				dp[i][j-1]+1,
				dp[i-1][j-1]+cost,
			)
			// Transposition (Damerau).
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				dp[i][j] = min(dp[i][j], dp[i-2][j-2]+cost)
			}
		}
	}
	if dp[la][lb] > max {
		return max + 1
	}
	return dp[la][lb]
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func min3(a, b, c int) int {
	return min(a, min(b, c))
}
