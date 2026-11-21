package essencefilter

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var levelParseRe = regexp.MustCompile(`\+?(\d+)`)

// --- Init ---

// EssenceFilterInitAction initializes warehouse screening, inventory, or battle rewards.
type EssenceFilterInitAction struct{}

// afterBattleInitResetPerLoot clears state that must be fresh for each战后战利品界面；引擎与锁定汇总保留在 RunState 上由首次完整 Init 建立。
func afterBattleInitResetPerLoot(st *RunState) {
	st.RowBoxes = nil
	st.RowIndex = 0
	if st.Loot != nil {
		st.Loot.gridVerified = false
	}
	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
	st.CurrentLocked = false
}

// prepareLockLimit reloads the inventory snapshot and rebuilds the per-combination quota plan.
func prepareLockLimit(ctx *maa.Context, st *RunState, opts *EssenceFilterOptions) bool {
	if len(opts.LockLimitWeaponIDs) == 0 {
		st.LockLimit = nil
		return true
	}
	snapshot := st.Account.base
	var err error
	if snapshot == nil {
		snapshot, err = st.Account.load(st.MatchEngine)
	}
	if err == nil && !snapshot.targetReady() {
		err = fmt.Errorf("missing verified gold inventory")
	}
	if err != nil {
		err = fmt.Errorf("load essence inventory: %w", err)
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadInventory").Msg("load essence inventory failed")
		reportFocusByKey(ctx, "lock_limit.inventory_unavailable", err.Error())
		return false
	}
	weapons := st.MatchEngine.Weapons()
	groups := snapshot.groups(weapons)
	reportSimpleByKey(ctx, "lock_limit.loaded", len(groups))
	st.LockLimit = newLockLimitPlanner(groups, weapons, opts.LockLimitWeaponIDs)
	return true
}

func (a *EssenceFilterInitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Msg("init start")

	opts, err := getOptionsFromAttach(ctx, arg.CurrentTaskName)
	if err != nil {
		err = fmt.Errorf("load options from %s: %w", arg.CurrentTaskName, err)
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadOptions").Msg("load options failed")
		reportFocusByKey(ctx, "focus.error.load_engine_failed", err.Error())
		return false
	}
	ctrl := taskController(ctx)
	var account *inventoryAccount
	if opts.Mode != InitModeFilter {
		account, err = accountFor(ctx, ctrl, arg.TaskID)
		if err != nil {
			return inventoryFailed(err)
		}
	}
	switch opts.Mode {
	case InitModeFilter, InitModeInventory, InitModeScreenAndReport:
	case InitModeAfterBattle:
		// 同一任务内再次进入战利品流程时只清本轮 UI 状态；首次进入继续做完整初始化。
		if st := currentRun; st != nil && st.TaskID == arg.TaskID && st.Account == account {
			afterBattleInitResetPerLoot(st)
			st.PipelineOpts = *opts
			if err := prepareReward(st); err != nil {
				return inventoryFailed(err)
			}
			if !prepareLockLimit(ctx, st, opts) {
				return false
			}
			log.Info().Str("component", "EssenceFilter").
				Int("lock_limit_weapons", len(opts.LockLimitWeaponIDs)).Msg("reward batch prepared")
			return true
		}
	default:
		err := fmt.Errorf("unknown attach.mode %q on %s", opts.Mode, arg.CurrentTaskName)
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "ValidateMode").Msg("invalid init mode")
		reportFocusByKey(ctx, "focus.error.load_engine_failed", err.Error())
		return false
	}
	currentRun = nil

	engine, err := loadMatchEngine(ctx, ctrl, opts)
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadMatchEngine").Msg("load match data failed")
		reportFocusByKey(ctx, "focus.error.load_engine_failed", err.Error())
		return false
	}
	inputLocale := engine.Locale()

	log.Info().Str("component", "EssenceFilter").Str("input_language", inputLocale).Msg("match engine ready")
	reportSimpleByKey(ctx, "focus.init.data_loaded")
	var weaponRarity []int
	if opts.Rarity6Weapon {
		weaponRarity = append(weaponRarity, 6)
	}
	if opts.Rarity5Weapon {
		weaponRarity = append(weaponRarity, 5)
	}
	if opts.Rarity4Weapon {
		weaponRarity = append(weaponRarity, 4)
	}
	var essenceTypes []string
	if opts.FlawlessEssence {
		essenceTypes = append(essenceTypes, flawlessEssenceName)
	}
	if opts.PureEssence {
		essenceTypes = append(essenceTypes, pureEssenceName)
	}
	if len(essenceTypes) == 0 {
		log.Error().Str("component", "EssenceFilter").Str("step", "ValidatePresets").Msg("no essence type selected")
		reportColoredByKey(ctx, "#ff0000", "focus.init.no_essence_type")
		return false
	}

	st := &RunState{TaskID: arg.TaskID, Account: account}
	st.PipelineOpts = *opts
	st.MatchEngine = engine
	st.TargetSkillCombinations = engine.BuildTargets(matchOptsFromPipeline(opts))

	if opts.Mode == InitModeAfterBattle {
		if err := prepareReward(st); err != nil {
			return inventoryFailed(err)
		}
		if !prepareLockLimit(ctx, st, opts) {
			return false
		}
	} else {
		if err := ctx.OverridePipeline(map[string]any{"EssenceGridAdvance": map[string]any{"attach": map[string]any{"collect_inventory": account != nil}}}); err != nil {
			return inventoryFailed(err)
		}
		if account != nil {
			if err := account.begin(engine); err != nil {
				return inventoryFailed(err)
			}
			st.Scan = newWarehouseScan()
		}
	}

	st.MatchedCombinationSummary = make(map[string]*matchapi.SkillCombinationSummary)
	currentRun = st
	if st.Scan != nil {
		reportSimpleByKey(ctx, "inventory.started")
	} else {
		reportInitSelection(ctx, weaponRarity, essenceTypes)
	}

	names := make([]string, 0, len(st.TargetSkillCombinations))
	for _, combo := range st.TargetSkillCombinations {
		names = append(names, combo.Weapon.ChineseName)
	}
	vm := buildInitViewModel(st)
	filteredWeapons := vm.FilteredWeapons
	log.Info().Str("component", "EssenceFilter").Str("step", "FilterWeapons").Int("filtered_count", len(filteredWeapons)).Strs("weapons", names).Msg("weapons filtered")
	reportInitWeapons(ctx, filteredWeapons)

	log.Info().Str("component", "EssenceFilter").Str("step", "BuildSkillCombinations").Int("combinations", len(st.TargetSkillCombinations)).Msg("skill combinations built")
	log.Info().Str("component", "EssenceFilter").Int("lock_limit_weapons", len(opts.LockLimitWeaponIDs)).Msg("init done")

	reportInitSkillList(ctx, vm.SlotSkills)
	reportDataVersionNotice(ctx, st)
	return true
}

// --- CheckItem / CheckItemLevel / SkillDecision（同一 case：单格技能识别与决策）---

// EssenceFilterCheckItemAction - OCR skills and match
type EssenceFilterCheckItemAction struct{}

func (a *EssenceFilterCheckItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Slot   int  `json:"slot"`
		IsLast bool `json:"is_last"`
	}
	if arg.CustomActionParam != "" {
		_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	}
	log.Info().Str("component", "EssenceFilter").Str("action", "CheckItem").Msg("start")
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckItem").Msg("no run state")
		return false
	}
	if params.Slot < 1 || params.Slot > 3 {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("invalid slot param")
		return false
	}
	if params.Slot == 1 {
		st.CurrentSkills = [3]string{}
		st.CurrentSkillLevels = [3]int{}
	}
	rawText, ok := firstOCRText(arg.RecognitionDetail)
	if !ok {
		log.Error().Str("component", "EssenceFilter").Msg("OCR detail missing from pipeline")
		return false
	}
	text := matchapi.NormalizeInputForMatch(rawText, st.MatchEngine.Locale())
	if text == "" {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("raw", rawText).Msg("OCR empty")
		return false
	}
	st.CurrentSkills[params.Slot-1] = text
	log.Info().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("skill", rawText).Bool("is_last", params.IsLast).Msg("OCR ok")
	if !params.IsLast {
		return true
	}
	for i, s := range st.CurrentSkills {
		if s == "" {
			log.Error().Str("component", "EssenceFilter").Int("slot", i+1).Msg("missing skill for slot")
			return false
		}
	}
	return true
}

// EssenceFilterCheckItemLevelAction - 识别技能等级（独立 level ROI）
type EssenceFilterCheckItemLevelAction struct{}

func (a *EssenceFilterCheckItemLevelAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Slot int `json:"slot"`
	}
	if arg.CustomActionParam != "" {
		_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	}
	if params.Slot < 1 || params.Slot > 3 {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("invalid level slot param")
		return false
	}
	rawText, ok := firstOCRText(arg.RecognitionDetail)
	if !ok {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("level OCR detail missing or empty")
		return false
	}
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		return false
	}
	if m := levelParseRe.FindStringSubmatch(rawText); len(m) >= 2 {
		if lv, err := strconv.Atoi(m[1]); err == nil && lv >= 1 && lv <= 6 {
			st.CurrentSkillLevels[params.Slot-1] = lv
			log.Info().Str("component", "EssenceFilter").Int("slot", params.Slot).Int("level", lv).Str("raw", rawText).Msg("OCR level ok")
			return true
		}
	}
	log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("raw", rawText).Msg("level parse fail")
	return false
}

// EssenceFilterSkillDecisionAction - match skills then decide lock or skip
type EssenceFilterSkillDecisionAction struct{}

func (a *EssenceFilterSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		reportFocusByKey(ctx, "focus.error.no_run_state")
		return false
	}
	ocr := matchapi.OCRInput{
		Skills: [3]string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]},
		Levels: [3]int{st.CurrentSkillLevels[0], st.CurrentSkillLevels[1], st.CurrentSkillLevels[2]},
	}

	if st.MatchEngine == nil {
		reportFocusByKey(ctx, "focus.error.no_match_engine")
		return false
	}
	r := &st.warehouseDetail
	var detail *warehouseDetail
	if skills, err := st.MatchEngine.InventorySkills(ocr); err == nil {
		detail = &warehouseDetail{Quality: r.Quality, Skills: skills}
	}
	if r.needsSecondRead(detail) {
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceGridClickPending"}}); err != nil {
			return inventoryFailed(err)
		}
		return true
	}
	if detail == nil && st.Scan != nil {
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterUnreadPurple"}}); err != nil {
			return inventoryFailed(err)
		}
		return true
	}
	mark, ok := decideWarehouseMark(ctx, st, ocr)
	if !ok {
		return false
	}
	r.previous = detail
	next := decisionNextNodes{
		Lock:    "EssenceFilterLockItem",
		Discard: "EssenceFilterDiscardItem",
		Skip:    "EssenceGridAdvance",
	}
	return next.route(ctx, arg.CurrentTaskName, mark)
}

func decideWarehouseMark(ctx *maa.Context, st *RunState, ocr matchapi.OCRInput) (string, bool) {
	if st.PipelineOpts.Mode == InitModeFilter {
		return decideSkillMark(ctx, st, ocr)
	}
	s := st.Scan
	if s == nil {
		return "", false
	}
	_, err := s.observe(st.MatchEngine, ocr)
	if err != nil {
		return "", inventoryFailed(err)
	}
	return unmarked, true
}

// --- Finish ---

// EssenceFilterFinishAction - finish and reset
type EssenceFilterFinishAction struct{}

func (a *EssenceFilterFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Msg("finish")
	st := currentRun
	if st == nil {
		return true
	}
	if st.TaskID != arg.TaskID {
		return false
	}
	if st.Scan != nil && !finishWarehouse(ctx, st) {
		return false
	}
	if st.PipelineOpts.Mode == InitModeFilter {
		log.Info().Str("component", "EssenceFilter").Int("matched_total", st.MatchedCount).Msg("locked items")
		reportColoredByKey(ctx, "#11cf00", "focus.finish.summary", st.MatchedCount)
		reportFinishExtRuleStats(ctx, st)
		reportFinishArtifacts(ctx, st)
		reportFinishLockLimit(ctx, st)
	}
	currentRun = nil
	return true
}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner = &EssenceFilterInitAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemLevelAction{}
	_ maa.CustomActionRunner = &EssenceFilterReadLockStateAction{}
	_ maa.CustomActionRunner = &EssenceFilterSkillDecisionAction{}
	_ maa.CustomActionRunner = &EssenceFilterFinishAction{}
)
