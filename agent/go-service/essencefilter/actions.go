package essencefilter

import (
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var levelParseRe = regexp.MustCompile(`\+?(\d+)`)

// --- Init ---

// EssenceFilterInitAction - initialize filter
type EssenceFilterInitAction struct{}

// afterBattleInitResetPerLoot clears state that must be fresh for each战后战利品界面；引擎与锁定汇总保留在 RunState 上由首次完整 Init 建立。
func afterBattleInitResetPerLoot(st *RunState) {
	st.RowBoxes = nil
	st.RowIndex = 0
	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
}

func (a *EssenceFilterInitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Msg("init start")

	// EssenceFilterAfterBattleInit：每次战利品流程都会进入；仅首次做下方完整初始化，之后每次只做 afterBattleInitResetPerLoot。
	if arg != nil && arg.CurrentTaskName == "EssenceFilterAfterBattleInit" {
		if st := getRunState(); st != nil && st.MatchEngine != nil {
			afterBattleInitResetPerLoot(st)
			return true
		}
	}

	engine, opts, err := EnsureMatchEngine(ctx, nil, arg.CurrentTaskName)
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadMatchEngine").Msg("load match data failed")
		reportFocusByKey(ctx, nil, "focus.error.load_engine_failed", err.Error())
		return false
	}
	inputLocale := matchapi.NormalizeInputLocale(opts.InputLanguage)

	log.Info().Str("component", "EssenceFilter").Str("input_language", inputLocale).Msg("match engine ready")
	reportSimpleByKey(ctx, nil, "focus.init.data_loaded")
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
	var essenceTypes []EssenceMeta
	if opts.FlawlessEssence {
		essenceTypes = append(essenceTypes, FlawlessEssenceMeta)
	}
	if opts.PureEssence {
		essenceTypes = append(essenceTypes, PureEssenceMeta)
	}
	if len(essenceTypes) == 0 {
		log.Error().Str("component", "EssenceFilter").Str("step", "ValidatePresets").Msg("no essence type selected")
		reportColoredByKey(ctx, nil, "#ff0000", "focus.init.no_essence_type")
		return false
	}

	var essenceMode EssenceMode
	switch {
	case opts.FlawlessEssence && opts.PureEssence:
		essenceMode = EssenceModeBoth
	case opts.FlawlessEssence:
		essenceMode = EssenceModeFlawlessOnly
	default:
		essenceMode = EssenceModePureOnly
	}

	st := &RunState{EssenceTypes: essenceTypes}
	st.Reset()
	st.PipelineOpts = *opts
	st.InputLanguage = inputLocale
	st.MatchEngine = engine
	st.EssenceMode = essenceMode

	matchOpts := matchOptsFromPipeline(opts)
	st.TargetSkillCombinations = engine.BuildTargets(matchOpts)
	st.MatchedCombinationSummary = make(map[string]*matchapi.SkillCombinationSummary)
	st.EssenceTypes = essenceTypes
	setRunState(st)
	reportInitSelection(ctx, st, weaponRarity, essenceTypes)

	names := make([]string, 0, len(st.TargetSkillCombinations))
	for _, combo := range st.TargetSkillCombinations {
		names = append(names, combo.Weapon.ChineseName)
	}
	vm := buildInitViewModel(st)
	filteredWeapons := vm.FilteredWeapons
	log.Info().Str("component", "EssenceFilter").Str("step", "FilterWeapons").Int("filtered_count", len(filteredWeapons)).Strs("weapons", names).Msg("weapons filtered")
	reportInitWeapons(ctx, st, filteredWeapons)

	log.Info().Str("component", "EssenceFilter").Str("step", "BuildSkillCombinations").Int("combinations", len(st.TargetSkillCombinations)).Msg("skill combinations built")
	log.Info().Str("component", "EssenceFilter").Msg("init done")

	reportInitSkillList(ctx, st, vm.SlotSkills)
	reportDataVersionNotice(ctx, st)
	return true
}

// --- Trace ---

// EssenceFilterTraceAction - log node/step
type EssenceFilterTraceAction struct{}

func (a *EssenceFilterTraceAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Step string `json:"step"`
	}
	_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	if params.Step == "" {
		params.Step = arg.CurrentTaskName
	}
	log.Info().Str("component", "EssenceFilter").Str("step", params.Step).Str("node", arg.CurrentTaskName).Msg("trace")
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
	st := getRunState()
	if st == nil {
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
	text := matchapi.NormalizeInputForMatch(rawText, st.InputLanguage)
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
	st := getRunState()
	if st == nil {
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
	st := getRunState()
	if st == nil {
		reportFocusByKey(ctx, nil, "focus.error.no_run_state")
		return false
	}
	ocr := matchapi.OCRInput{
		Skills: [3]string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]},
		Levels: [3]int{st.CurrentSkillLevels[0], st.CurrentSkillLevels[1], st.CurrentSkillLevels[2]},
	}

	if st.MatchEngine == nil {
		reportFocusByKey(ctx, st, "focus.error.no_match_engine")
		return false
	}
	return runUnifiedSkillDecision(ctx, arg, st, st.MatchEngine, ocr, decisionNextNodes{
		Lock:    "EssenceFilterLockItemLog",
		Discard: "EssenceFilterDiscardItemLog",
		Skip:    "EssenceGridAdvance",
	})
}

// --- Finish ---

// EssenceFilterFinishAction - finish and reset
type EssenceFilterFinishAction struct{}

func (a *EssenceFilterFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Msg("finish")
	st := getRunState()
	if st != nil {
		log.Info().Str("component", "EssenceFilter").Int("matched_total", st.MatchedCount).Msg("locked items")
		reportColoredByKey(ctx, st, "#11cf00", "focus.finish.summary", st.VisitedCount, st.MatchedCount)
		reportFinishExtRuleStats(ctx, st)
		reportFinishArtifacts(ctx, st)
	}
	setRunState(nil)
	return true
}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner = &EssenceFilterInitAction{}
	_ maa.CustomActionRunner = &EssenceFilterTraceAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemLevelAction{}
	_ maa.CustomActionRunner = &EssenceFilterSkillDecisionAction{}
	_ maa.CustomActionRunner = &EssenceFilterFinishAction{}
)
