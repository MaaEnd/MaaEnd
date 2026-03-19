package matchapi

// WeaponData represents a weapon entry after canonicalizing its three skills.
type WeaponData struct {
	InternalID    string   `json:"internal_id"`
	ChineseName   string   `json:"chinese_name"`
	TypeID        int      `json:"type_id"`
	Rarity        int      `json:"rarity"`
	SkillIDs      []int    `json:"skill_ids"`      // [slot1_id, slot2_id, slot3_id]
	SkillsChinese []string `json:"skills_chinese"` // [slot1_cn, slot2_cn, slot3_cn]
}

// SkillPool is a canonical skill pool entry for a slot.
type SkillPool struct {
	ID      int    `json:"id"`
	English string `json:"english"`
	Chinese string `json:"chinese"`
}

// Location records optional extra pools for a stage location.
type Location struct {
	Name     string `json:"name"`
	Slot2IDs []int  `json:"slot2_ids"`
	Slot3IDs []int  `json:"slot3_ids"`
}

type SkillPools struct {
	Slot1 []SkillPool `json:"slot1"`
	Slot2 []SkillPool `json:"slot2"`
	Slot3 []SkillPool `json:"slot3"`
}

// SkillCombination is the canonical target combination for exact matching.
// Each weapon corresponds to one target combination row in EssenceFilter.
type SkillCombination struct {
	Weapon        WeaponData
	SkillsChinese []string // [slot1_cn, slot2_cn, slot3_cn]
	SkillIDs      []int    // [slot1_id, slot2_id, slot3_id]
}

// SkillCombinationMatch is the runtime match result for one OCR input.
// For extension rules, Weapons may be empty.
type SkillCombinationMatch struct {
	SkillIDs      []int
	SkillsChinese []string
	Weapons       []WeaponData
}

// SkillCombinationSummary is a per-run aggregation item (used by UI).
type SkillCombinationSummary struct {
	SkillIDs      []int
	SkillsChinese []string // from target configuration (for debug)
	OCRSkills     []string // actual OCR skill texts (for display)
	Weapons       []WeaponData
	Count         int
}

// MatcherConfig is the data driving fuzzy OCR->skill-id mapping.
type MatcherConfig struct {
	DataVersion        string              `json:"data_version"`
	SimilarWordMap     map[string]string   `json:"similarWordMap"`
	SuffixStopwords    []string            `json:"-"`
	SuffixStopwordsMap map[string][]string `json:"suffixStopwords"`
}

// EssenceFilterOptions is the subset of EssenceFilter attach options needed for matching.
type EssenceFilterOptions struct {
	// Rarity selection; if none selected, exact matching is disabled.
	Rarity6Weapon bool `json:"rarity6_weapon"`
	Rarity5Weapon bool `json:"rarity5_weapon"`
	Rarity4Weapon bool `json:"rarity4_weapon"`

	// Future Promising extension.
	KeepFuturePromising     bool `json:"keep_future_promising"`
	FuturePromisingMinTotal int  `json:"future_promising_min_total"`
	LockFuturePromising     bool `json:"lock_future_promising"`

	// Slot3 Practical extension.
	KeepSlot3Level3Practical bool `json:"keep_slot3_level3_practical"`
	Slot3MinLevel            int  `json:"slot3_min_level"`
	LockSlot3Practical       bool `json:"lock_slot3_practical"`

	// No-match behavior.
	DiscardUnmatched bool `json:"discard_unmatched"`
}

// OCRInput is the caller-provided OCR result for one essence item.
type OCRInput struct {
	Skills [3]string // slot1..slot3 texts
	Levels [3]int    // slot1..slot3 levels
}

type MatchKind int

const (
	MatchNone MatchKind = iota
	MatchExact
	MatchFuturePromising
	MatchSlot3Level3Practical
)

// MatchResult is the single unified output of the matching engine.
// The caller (pipeline UI/action layer) decides the actual operation based on ShouldLock/ShouldDiscard.
type MatchResult struct {
	Kind MatchKind

	// Parsed skills (canonical for exact, OCR-derived for extension).
	SkillIDs      []int
	SkillsChinese []string
	Weapons       []WeaponData

	// Reason is always set for logging/UI: MatchExact uses "精准匹配：" + 武器中文名（、分隔）；
	// MatchFuturePromising / MatchSlot3Level3Practical use fixed Chinese prefixes; MatchNone is "未匹配".
	Reason string

	// Final directives for pipeline.
	ShouldLock    bool
	ShouldDiscard bool
}

// EngineData is the loaded dataset exposed to consumers (primarily UI/calculators).
type EngineData struct {
	SkillPools SkillPools
	Weapons    []WeaponData
	Locations  []Location
}
