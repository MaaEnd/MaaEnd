package essencefilter

// EssenceFilterOptions is unmarshaled from Pipeline node attach JSON (full UI / filter options).
// Matching uses the subset type matchapi.EssenceFilterOptions; see actions.go for the mapping.
type EssenceFilterOptions struct {
	Rarity6Weapon   bool `json:"rarity6_weapon"`
	Rarity5Weapon   bool `json:"rarity5_weapon"`
	Rarity4Weapon   bool `json:"rarity4_weapon"`
	FlawlessEssence bool `json:"flawless_essence"`
	PureEssence     bool `json:"pure_essence"`

	// 保留未来可期基质：三种词条且总等级 >= n
	KeepFuturePromising     bool `json:"keep_future_promising"`
	FuturePromisingMinTotal int  `json:"future_promising_min_total"`
	// 未来可期命中后是否执行锁定；关闭时仅分类命中并跳过（不锁定、不废弃）
	LockFuturePromising bool `json:"lock_future_promising"`
	// 保留实用基质：词条3等级 >= n 且为辅助即插即用技能
	KeepSlot3Level3Practical bool `json:"keep_slot3_level3_practical"`
	Slot3MinLevel            int  `json:"slot3_min_level"`
	// 实用基质命中后是否执行锁定；关闭时仅分类命中并跳过（不锁定、不废弃）
	LockSlot3Practical bool `json:"lock_slot3_practical"`
	// 未匹配时废弃而非跳过
	DiscardUnmatched bool `json:"discard_unmatched"`
	// 筛选结束后推荐预刻写方案（枚举最优方案并输出到日志）
	ExportCalculatorScript bool `json:"export_calculator_script"`
	// 收集每行时对缩略图做已锁定/已废弃标记识别，命中则从本行待处理列表排除（见 RowCollect + EssenceThumbMarked）
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
