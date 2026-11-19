package essencefilter

// InitMode is the init node's attach.mode, selecting how EssenceFilterInitAction initializes a run.
type InitMode string

const (
	InitModeFilter          InitMode = "filter"
	InitModeScreenAndReport InitMode = "screen_and_report"
	InitModeInventory       InitMode = "inventory"
	InitModeAfterBattle     InitMode = "after_battle"
)

// AfterBattlePass is attach.after_battle_pass, selecting one of the two after-battle loot passes.
type AfterBattlePass string

const (
	// AfterBattlePassReadOnly records what the loot round holds without locking or discarding.
	AfterBattlePassReadOnly AfterBattlePass = "readonly"
	// AfterBattlePassExecute locks and discards under the per-combination quota.
	AfterBattlePassExecute AfterBattlePass = "execute"
)

// EssenceFilterOptions is unmarshaled from Pipeline node attach JSON (full UI / filter options).
// Matching uses the subset type matchapi.EssenceFilterOptions; see actions.go for the mapping.
type EssenceFilterOptions struct {
	Mode InitMode `json:"mode"`

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
	// 目标模式开启战后锁定名额时传入本次勾选的武器。
	LockLimitWeaponIDs []string `json:"lock_limit_weapon_ids"`
	// 战后筛选遍次：只读遍只记录本轮持有份数，执行遍按名额落锁；留空等同执行遍
	AfterBattlePass AfterBattlePass `json:"after_battle_pass"`
	// 筛选结束后推荐预刻写方案（枚举最优方案并输出到日志）；开启时会同时写入用户数据目录 debug/record/EssencePlan.html（每次覆盖）
	ExportCalculatorScript bool `json:"export_calculator_script"`
	// InputLanguage is game/OCR language for skill matching: CN|TC|EN|JP|KR.
	// Empty / omitted / AUTO: detect via __EssenceFilterDetectLang* OCR nodes at Init.
	// Explicit CN|TC|EN|JP|KR: force that locale (debug / override).
	InputLanguage string `json:"input_language"`
}

const (
	flawlessEssenceName = "无瑕基质"
	pureEssenceName     = "高纯基质"
)
