package essencefilter

// WeaponData - weapon data
type WeaponData struct {
	InternalID    string   `json:"internal_id"`
	ChineseName   string   `json:"chinese_name"`
	TypeID        int      `json:"type_id"`
	Rarity        int      `json:"rarity"`
	SkillIDs      []int    `json:"skill_ids"`      // [slot1_id, slot2_id, slot3_id]
	SkillsChinese []string `json:"skills_chinese"` // for logging/matching
}

// SkillPool - skill pool entry
type SkillPool struct {
	ID      int    `json:"id"`
	English string `json:"english"`
	Chinese string `json:"chinese"`
}

// WeaponDatabase - weapon DB
type WeaponDatabase struct {
	WeaponTypes []struct {
		ID      int    `json:"id"`
		English string `json:"english"`
		Chinese string `json:"chinese"`
	} `json:"weapon_types"`
	SkillPools struct {
		Slot1 []SkillPool `json:"slot1"`
		Slot2 []SkillPool `json:"slot2"`
		Slot3 []SkillPool `json:"slot3"`
	} `json:"skill_pools"`
	Weapons []WeaponData `json:"weapons"`
}

// FilterPreset - preset config
type FilterPreset struct {
	Name   string       `json:"name"`
	Label  string       `json:"label"`
	Filter FilterConfig `json:"filter"`
}

// FilterConfig - filtering config
type FilterConfig struct {
	TypeIDs   []int `json:"type_ids"`   // optional weapon type filter
	MinRarity int   `json:"min_rarity"` // min rarity
	MaxRarity int   `json:"max_rarity"` // max rarity
}

// SkillCombination - target skill combination
type SkillCombination struct {
	Weapon        WeaponData
	SkillsChinese []string // [slot1_cn, slot2_cn, slot3_cn]
	SkillIDs      []int    // [slot1_id, slot2_id, slot3_id]
}

// MatcherConfig - 匹配器配置结构
type MatcherConfig struct {
	SimilarWordMap  map[string]string `json:"similarWordMap"`
	SuffixStopwords []string          `json:"suffixStopwords"`
}

// Global variables
var (
	weaponDB                WeaponDatabase
	targetSkillCombinations []SkillCombination
	visitedCount            int
	matchedCount            int
	filteredSkillStats      [3]map[int]int
	statsLogged             bool

	// Grid traversal state
	currentCol         int // 1~9
	currentRow         int // row index
	maxItemsPerRow     int
	firstRowSwipeDone  bool // true after first row swipe is used
	finalLargeScanUsed bool // true if final large scan has been used

	// Current item's three skills cache
	currentSkills [3]string

	// Row processing: collected boxes and index
	rowBoxes       [][4]int
	rowIndex       int
	weaponDataPath string

	// Matcher config - loaded from JSON config file, used for skill name matching
	matcherConfig MatcherConfig
)
