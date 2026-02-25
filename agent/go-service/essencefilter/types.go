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

// SkillCombination - target skill combination（静态配置，一把武器一条）
type SkillCombination struct {
	Weapon        WeaponData
	SkillsChinese []string // [slot1_cn, slot2_cn, slot3_cn]
	SkillIDs      []int    // [slot1_id, slot2_id, slot3_id]
}

// SkillCombinationMatch - 运行时匹配结果：同一套技能可能对应多把武器
type SkillCombinationMatch struct {
	SkillIDs      []int
	SkillsChinese []string
	Weapons       []WeaponData
}

// SkillCombinationSummary - 本次运行中某一套技能组合的锁定统计
type SkillCombinationSummary struct {
	SkillIDs      []int
	SkillsChinese []string // 静态配置中的技能中文名（用于调试）
	OCRSkills     []string // 实际本次匹配时 OCR 到的技能文本（用于展示）
	Weapons       []WeaponData
	Count         int
}

// MatcherConfig - 匹配器配置结构
type MatcherConfig struct {
	SimilarWordMap  map[string]string `json:"similarWordMap"`
	SuffixStopwords []string          `json:"suffixStopwords"`
}

type EssenceFilterOptions struct {
	Rarity6Weapon   bool `json:"rarity6_weapon"`
	Rarity5Weapon   bool `json:"rarity5_weapon"`
	Rarity4Weapon   bool `json:"rarity4_weapon"`
	FlawlessEssence bool `json:"flawless_essence"`
	PureEssence     bool `json:"pure_essence"`

	// 保留未来可期基质：三种词条且总等级 >= n
	KeepFuturePromising     bool `json:"keep_future_promising"`
	FuturePromisingMinTotal int  `json:"future_promising_min_total"`
	// 保留实用基质：词条3等级 >= n 且为辅助即插即用技能
	KeepSlot3Level3Practical bool `json:"keep_slot3_level3_practical"`
	Slot3MinLevel            int  `json:"slot3_min_level"`
}

type ColorRange struct {
	Lower [3]int
	Upper [3]int
}

type EssenceMeta struct {
	Name  string
	Range ColorRange
}

// Global variables
var (
	weaponDB                WeaponDatabase
	targetSkillCombinations []SkillCombination
	visitedCount                int
	matchedCount                int
	extFuturePromisingCount     int
	extSlot3PracticalCount      int
	filteredSkillStats      [3]map[int]int
	statsLogged             bool

	// 本次运行中命中的技能组合摘要，按技能 ID 组合聚合
	matchedCombinationSummary map[string]*SkillCombinationSummary

	// Grid traversal state
	currentCol         int // 1~9
	currentRow         int // row index
	maxItemsPerRow     int
	firstRowSwipeDone  bool // true after first row swipe is used
	finalLargeScanUsed bool // true if final large scan has been used

	// Current item's three skills cache
	currentSkills      [3]string
	currentSkillLevels [3]int // 从 OCR 解析出的等级 (+1/+2/+3)，0 表示未识别

	// Row processing: collected boxes and index
	rowBoxes       [][4]int
	rowIndex       int
	weaponDataPath string

	// Matcher config - loaded from JSON config file, used for skill name matching
	matcherConfig MatcherConfig

	// Essence color matching parameters
	FlawlessEssenceMeta = EssenceMeta{
		// Name: "Flawless Essence",
		Name: "无暇基质",
		Range: ColorRange{
			Lower: [3]int{18, 70, 220},
			Upper: [3]int{26, 255, 255},
		},
	}
	PureEssenceMeta = EssenceMeta{
		Name: "高纯基质",
		Range: ColorRange{
			Lower: [3]int{130, 55, 80},
			Upper: [3]int{136, 255, 255},
		},
	}

	EssenceTypes []EssenceMeta
)
