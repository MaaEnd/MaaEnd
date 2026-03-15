package essencefilter

import "encoding/json"

// WeaponData - weapon data
type WeaponData struct {
	InternalID    string   `json:"internal_id"`
	ChineseName   string   `json:"chinese_name"`
	TypeID        int      `json:"type_id"`
	Rarity        int      `json:"rarity"`
	SkillIDs      []int    `json:"skill_ids"`      // [slot1_id, slot2_id, slot3_id]
	SkillsChinese []string `json:"skills_chinese"` // for logging/matching
}

// SkillPool - skill pool entry (supports both "chinese"/"english" and "cn"/"en" from new skill_pools.json)
type SkillPool struct {
	ID      int    `json:"id"`
	English string `json:"english"`
	Chinese string `json:"chinese"`
}

// skillPoolJSON - for unmarshaling; cn/tc/en map to Chinese when chinese is empty
type skillPoolJSON struct {
	ID      int    `json:"id"`
	English string `json:"english"`
	Chinese string `json:"chinese"`
	CN      string `json:"cn"`
	TC      string `json:"tc"`
	EN      string `json:"en"`
}

// UnmarshalJSON supports both legacy (chinese/english) and new (cn/tc/en) skill_pools.json
func (s *SkillPool) UnmarshalJSON(data []byte) error {
	var raw skillPoolJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	s.ID = raw.ID
	s.English = raw.EN
	if s.English == "" {
		s.English = raw.English
	}
	s.Chinese = raw.Chinese
	if s.Chinese == "" {
		s.Chinese = raw.CN
	}
	if s.Chinese == "" {
		s.Chinese = raw.TC
	}
	return nil
}

// WeaponOutputEntry - single weapon from weapons_output.json
type WeaponOutputEntry struct {
	InternalID  string              `json:"internal_id"`
	WeaponType  string              `json:"weapon_type"`
	Rarity      int                 `json:"rarity"`
	IconPath    string              `json:"icon_path"`
	Names       map[string]string   `json:"names"`
	Skills      map[string][]string `json:"skills"`
	SkillIDs    []string            `json:"skill_ids"` // internal ids, not used for matching
}

// WeaponsOutputRaw - root structure of weapons_output.json: map internal_id -> weapon
type WeaponsOutputRaw map[string]WeaponOutputEntry

// Location 刷取地点数据：记录该地点可选的附加属性（slot2）和技能属性（slot3）池
type Location struct {
	Name     string `json:"name"`
	Slot2IDs []int  `json:"slot2_ids"`
	Slot3IDs []int  `json:"slot3_ids"`
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
	Weapons   []WeaponData `json:"weapons"`
	Locations []Location   `json:"locations"`
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

// MatcherConfig - 匹配器配置结构（suffixStopwords 支持旧版数组或新版按语言 map）
type MatcherConfig struct {
	DataVersion        string              `json:"data_version"`
	SimilarWordMap     map[string]string   `json:"similarWordMap"`
	SuffixStopwords    []string            `json:"-"` // filled from SuffixStopwordsMap[locale] or legacy array
	SuffixStopwordsMap map[string][]string `json:"suffixStopwords"`
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
	// 未匹配时废弃而非跳过
	DiscardUnmatched bool `json:"discard_unmatched"`
	// 筛选结束后推荐预刻写方案（枚举最优方案并输出到日志）
	ExportCalculatorScript bool `json:"export_calculator_script"`
	// 跳过已识别（已锁）行：每行先试最后一个格子，已锁则整行跳过；关闭后不从最后一个试起
	SkipLockedRow bool `json:"skip_locked_row"`
}

type ColorRange struct {
	Lower [3]int
	Upper [3]int
}

type EssenceMeta struct {
	Name  string
	Range ColorRange
}

// Global variables (data in db.go; runtime state in RunState; matcher config in config.go)
var (
	// Essence color matching parameters (defaults; per-run selection in RunState.EssenceTypes)
	FlawlessEssenceMeta = EssenceMeta{
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
)
