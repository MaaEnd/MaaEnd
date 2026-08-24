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
	// 筛选结束后推荐预刻写方案（枚举最优方案并输出到日志）；开启时会同时写入工作目录 ./EssencePlan.html（每次覆盖）
	ExportCalculatorScript bool `json:"export_calculator_script"`
	// InputLanguage is game/OCR language for skill matching: CN|TC|EN|JP|KR (default CN).
	InputLanguage string `json:"input_language"`
}

// EssenceMode describes which essence tiers are selected for this run.
type EssenceMode int

const (
	EssenceModeBoth         EssenceMode = iota // both flawless + pure selected
	EssenceModeFlawlessOnly                    // only flawless selected; stop when pure encountered
	EssenceModePureOnly                        // only pure selected; skip flawless until pure appears
)

const (
	flawlessEssenceName = "无暇基质"
	pureEssenceName     = "高纯基质"
)
