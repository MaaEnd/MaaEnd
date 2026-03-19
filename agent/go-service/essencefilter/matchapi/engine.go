package matchapi

import (
	"errors"
	"sync"
)

// Engine is a pure matching engine: OCR -> skill-id -> exact/extension match.
// It does not know anything about UI/actions.
type Engine struct {
	cfg     MatcherConfig
	data    EngineData
	slotIdx [3]slotIndex

	slotIndicesOnce sync.Once

	// Cache exact targets by rarity selection.
	targetsCacheMu sync.Mutex
	targetsCache   map[string][]SkillCombination
}

// DataVersion returns matcher data version string.
func (e *Engine) DataVersion() string {
	return e.cfg.DataVersion
}

// SkillPools returns the skill pool tables currently loaded in this engine.
// The returned struct aliases the engine's internal slices; callers must treat it as read-only.
func (e *Engine) SkillPools() SkillPools {
	return e.data.SkillPools
}

// Weapons returns the weapon rows currently loaded in this engine.
// The returned slice aliases the engine's backing array; callers must treat it as read-only.
func (e *Engine) Weapons() []WeaponData {
	return e.data.Weapons
}

// Locations returns the location extra-pool rows currently loaded in this engine.
// The returned slice aliases the engine's backing array; callers must treat it as read-only.
func (e *Engine) Locations() []Location {
	return e.data.Locations
}

// NewDefaultEngine loads built-in EssenceFilter data from repo's assets directory.
// It may require the caller to run with a working directory where assets can be resolved.
func NewDefaultEngine() (*Engine, error) {
	dataDir, err := findDefaultDataDir()
	if err != nil {
		return nil, err
	}
	return NewEngineFromDir(dataDir)
}

// NewEngineFromDir loads matcher_config.json + skill_pools.json + weapons_output.json + locations.json
// from the given directory.
func NewEngineFromDir(dataDir string) (*Engine, error) {
	if dataDir == "" {
		return nil, errors.New("dataDir is empty")
	}

	cfg, err := loadMatcherConfig(dataDir)
	if err != nil {
		return nil, err
	}
	pools, err := loadSkillPools(dataDir)
	if err != nil {
		return nil, err
	}
	weapons, err := loadWeaponsOutputAndConvert(dataDir, cfg, pools)
	if err != nil {
		return nil, err
	}
	locations, err := loadLocations(dataDir)
	if err != nil {
		return nil, err
	}

	return &Engine{
		cfg: cfg,
		data: EngineData{
			SkillPools: pools,
			Weapons:    weapons,
			Locations:  locations,
		},
		targetsCache: make(map[string][]SkillCombination),
	}, nil
}

// MatchOCR matches one OCR result and returns a unified MatchResult.
func (e *Engine) MatchOCR(ocr OCRInput, opts EssenceFilterOptions) (*MatchResult, error) {
	targets, err := e.getTargetsByRarity(opts)
	if err != nil {
		return nil, err
	}

	// 1) Exact matching on (slot1,slot2,slot3) skill IDs.
	ocrSkills := [3]string{ocr.Skills[0], ocr.Skills[1], ocr.Skills[2]}
	ocrLevels := [3]int{ocr.Levels[0], ocr.Levels[1], ocr.Levels[2]}
	ocrSkills, ocrLevels = e.reorderByPoolAssignmentIfPossible(ocrSkills, ocrLevels)

	// If no rarity is selected, exact matching must be disabled.
	var exact *SkillCombinationMatch
	if len(targets) > 0 {
		exactMatched, ok := e.matchEssenceSkills(ocrSkills, targets)
		if ok {
			exact = exactMatched
		}
	}
	if exact != nil {
		return &MatchResult{
			Kind:          MatchExact,
			SkillIDs:      exact.SkillIDs,
			SkillsChinese: exact.SkillsChinese,
			Weapons:       exact.Weapons,
			Reason:        exactMatchReason(exact.Weapons),
			ShouldLock:    true,
			ShouldDiscard: false,
		}, nil
	}

	// 2) Future Promising extension rule.
	if opts.KeepFuturePromising && opts.FuturePromisingMinTotal > 0 {
		if e.matchFuturePromising(ocrSkills, ocrLevels, opts.FuturePromisingMinTotal) {
			sum := ocrLevels[0] + ocrLevels[1] + ocrLevels[2]
			return &MatchResult{
				Kind:          MatchFuturePromising,
				SkillIDs:      []int{0, 0, 0},
				SkillsChinese: []string{ocrSkills[0], ocrSkills[1], ocrSkills[2]},
				Weapons:       []WeaponData{},
				Reason:        "未来可期：总等级 " + itoa(sum) + " ≥ " + itoa(opts.FuturePromisingMinTotal),
				ShouldLock:    opts.LockFuturePromising,
				ShouldDiscard: false,
			}, nil
		}
	}

	// 3) Slot3 Level3 Practical extension rule.
	if opts.KeepSlot3Level3Practical {
		minLv := opts.Slot3MinLevel
		if minLv <= 0 {
			minLv = 3
		}

		if match, slot3Lv, ok := e.matchSlot3Level3Practical(ocrSkills, ocrLevels, minLv); ok {
			return &MatchResult{
				Kind:          MatchSlot3Level3Practical,
				SkillIDs:      match.SkillIDs,
				SkillsChinese: match.SkillsChinese,
				Weapons:       match.Weapons,
				Reason:        "实用基质：词条3(" + match.SkillsChinese[2] + ")等级 " + itoa(slot3Lv) + " ≥ " + itoa(minLv),
				ShouldLock:    opts.LockSlot3Practical,
				ShouldDiscard: false,
			}, nil
		}
	}

	// 4) No match.
	return &MatchResult{
		Kind:          MatchNone,
		SkillIDs:      []int{},
		SkillsChinese: []string{ocrSkills[0], ocrSkills[1], ocrSkills[2]},
		Weapons:       []WeaponData{},
		Reason:        "未匹配",
		ShouldLock:    false,
		ShouldDiscard: opts.DiscardUnmatched,
	}, nil
}

// reorderByPoolAssignmentIfPossible reorders OCR skills/levels into slot1/2/3 order
// by inferring which slot-pool each OCR skill belongs to.
//
// If the inference is not unique (e.g. ambiguous match or duplicate slot assignment),
// it falls back to the original input order.
func (e *Engine) reorderByPoolAssignmentIfPossible(inSkills [3]string, inLevels [3]int) ([3]string, [3]int) {
	// Default: keep input order.
	outSkills := inSkills
	outLevels := inLevels

	e.ensureSlotIndices()

	assignedSlots := [3]int{}
	used := [4]bool{}

	for i := 0; i < 3; i++ {
		slot, ok := e.assignSlotForOCRText(inSkills[i])
		if !ok {
			return outSkills, outLevels
		}
		if used[slot] {
			// Duplicate pool assignment (e.g. 2x slot1 + 1x slot2) => keep default order.
			return outSkills, outLevels
		}
		used[slot] = true
		assignedSlots[i] = slot
	}

	if !used[1] || !used[2] || !used[3] {
		return outSkills, outLevels
	}

	// Build ordered arrays: [slot1, slot2, slot3].
	var skillsOrdered [3]string
	var levelsOrdered [3]int
	for i := 0; i < 3; i++ {
		slot := assignedSlots[i] // 1..3
		orderedIdx := slot - 1
		skillsOrdered[orderedIdx] = inSkills[i]
		levelsOrdered[orderedIdx] = inLevels[i]
	}
	return skillsOrdered, levelsOrdered
}

// assignSlotForOCRText returns which slot pool the given OCR skill text belongs to.
// It prefers strict exact (full/core) matches; if those are not unique, it falls back to fuzzy matching.
func (e *Engine) assignSlotForOCRText(text string) (int, bool) {
	cleanedRaw := cleanChinese(text)
	if cleanedRaw == "" {
		return 0, false
	}

	coreRaw := trimStopSuffix(e.cfg, cleanedRaw)
	cleanedNorm := normalizeSimilar(e.cfg, cleanedRaw)
	coreNorm := trimStopSuffix(e.cfg, cleanedNorm)

	exactFullSlots := make([]int, 0, 3)
	exactCoreSlots := make([]int, 0, 3)

	for slot := 1; slot <= 3; slot++ {
		idx := e.slotIdx[slot-1]
		if ids, ok := idx.rawFullIndex[cleanedRaw]; ok && len(ids) > 0 {
			exactFullSlots = append(exactFullSlots, slot)
			continue
		}
		if ids, ok := idx.normFullIndex[cleanedNorm]; ok && len(ids) > 0 {
			exactFullSlots = append(exactFullSlots, slot)
			continue
		}
		if ids, ok := idx.rawCoreIndex[coreRaw]; ok && len(ids) > 0 {
			exactCoreSlots = append(exactCoreSlots, slot)
			continue
		}
		if ids, ok := idx.normCoreIndex[coreNorm]; ok && len(ids) > 0 {
			exactCoreSlots = append(exactCoreSlots, slot)
			continue
		}
	}

	if len(exactFullSlots) == 1 {
		return exactFullSlots[0], true
	}
	if len(exactFullSlots) > 1 {
		return 0, false
	}

	if len(exactCoreSlots) == 1 {
		return exactCoreSlots[0], true
	}
	if len(exactCoreSlots) > 1 {
		return 0, false
	}

	// Fallback: fuzzy matching (may be ambiguous, so we still require uniqueness).
	fuzzySlots := make([]int, 0, 3)
	for slot := 1; slot <= 3; slot++ {
		if _, ok := e.matchSkillIDEnhanced(slot, text); ok {
			fuzzySlots = append(fuzzySlots, slot)
		}
	}

	if len(fuzzySlots) == 1 {
		return fuzzySlots[0], true
	}
	return 0, false
}

// BuildTargets builds exact-matching target combinations based on rarity toggles.
func (e *Engine) BuildTargets(opts EssenceFilterOptions) []SkillCombination {
	targets, _ := e.getTargetsByRarity(opts)
	return targets
}

func (e *Engine) getTargetsByRarity(opts EssenceFilterOptions) ([]SkillCombination, error) {
	key := rarityKey(opts)

	e.targetsCacheMu.Lock()
	defer e.targetsCacheMu.Unlock()

	if v, ok := e.targetsCache[key]; ok {
		return v, nil
	}

	var selected []int
	if opts.Rarity6Weapon {
		selected = append(selected, 6)
	}
	if opts.Rarity5Weapon {
		selected = append(selected, 5)
	}
	if opts.Rarity4Weapon {
		selected = append(selected, 4)
	}

	if len(selected) == 0 {
		e.targetsCache[key] = []SkillCombination{}
		return e.targetsCache[key], nil
	}

	weapons := make([]WeaponData, 0, len(e.data.Weapons))
	for _, w := range e.data.Weapons {
		for _, r := range selected {
			if w.Rarity == r {
				weapons = append(weapons, w)
				break
			}
		}
	}

	targets := make([]SkillCombination, 0, len(weapons))
	for _, w := range weapons {
		targets = append(targets, SkillCombination{
			Weapon:        w,
			SkillsChinese: w.SkillsChinese,
			SkillIDs:      w.SkillIDs,
		})
	}
	e.targetsCache[key] = targets
	return targets, nil
}

func rarityKey(opts EssenceFilterOptions) string {
	var b [3]bool
	b[0] = opts.Rarity6Weapon
	b[1] = opts.Rarity5Weapon
	b[2] = opts.Rarity4Weapon
	// stable key to simplify caching.
	if !b[0] && !b[1] && !b[2] {
		return "none"
	}
	// e.g. "6-4" / "5" / "6-5-4"
	key := ""
	if b[0] {
		key += "6"
	}
	if b[1] {
		if key != "" {
			key += "-"
		}
		key += "5"
	}
	if b[2] {
		if key != "" {
			key += "-"
		}
		key += "4"
	}
	return key
}
