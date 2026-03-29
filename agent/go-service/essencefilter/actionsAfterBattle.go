package essencefilter

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type EssenceFilterAfterBattleSkillDecisionAction struct{}

// EssenceFilterAfterBattleTierGateAction gates after-battle items by essence tier.
// Pipeline param "tier" must be "flawless" or "pure".
type EssenceFilterAfterBattleTierGateAction struct{}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner = &EssenceFilterAfterBattleSkillDecisionAction{}
	_ maa.CustomActionRunner = &EssenceFilterAfterBattleTierGateAction{}
)

func (a *EssenceFilterAfterBattleTierGateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := getRunState()
	if st == nil {
		return false
	}
	var params struct {
		Tier string `json:"tier"` // "flawless" or "pure"
	}
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().Str("component", "EssenceFilter").Str("action", "AfterBattleTierGate").
				Err(err).Str("raw", arg.CustomActionParam).Msg("failed to parse custom_action_param")
			return false
		}
	}
	if params.Tier != "flawless" && params.Tier != "pure" {
		log.Error().Str("component", "EssenceFilter").Str("action", "AfterBattleTierGate").
			Str("tier", params.Tier).Msg("invalid or missing tier param, expected \"flawless\" or \"pure\"")
		return false
	}

	switch st.EssenceMode {
	case EssenceModeFlawlessOnly:
		if params.Tier == "pure" {
			log.Info().Str("component", "EssenceFilter").Str("action", "AfterBattleTierGate").
				Msg("flawless-only: pure item detected, exiting after-battle")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleTierBoundaryFlawlessNotice"}})
			return true
		}
	case EssenceModePureOnly:
		if params.Tier == "flawless" {
			log.Info().Str("component", "EssenceFilter").Str("action", "AfterBattleTierGate").
				Msg("pure-only: flawless item detected, skipping")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleTierSkipPureOnlyNotice"}})
			return true
		}
	}

	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceAfterBattleYellowSlot1"}})
	return true
}

func (a *EssenceFilterAfterBattleSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 获取当前运行状态，如果状态为空则无法继续，直接返回
	st := getRunState()
	if st == nil {
		reportFocusByKey(ctx, nil, "focus.error.no_run_state")
		return false
	}

	// 将识别到的三个技能名称和等级存入ocr结构中给api调用
	ocr := matchapi.OCRInput{
		Skills: [3]string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]},             // 这三条不要求严格按 slot1/slot2/slot3 顺序；引擎会基于 pool 自动重排（若能唯一推断）
		Levels: [3]int{st.CurrentSkillLevels[0], st.CurrentSkillLevels[1], st.CurrentSkillLevels[2]}, // 对应等级（1..6）
	}

	if st.MatchEngine == nil {
		reportFocusByKey(ctx, st, "focus.error.no_match_engine")
		return false
	}
	return runUnifiedSkillDecision(ctx, arg, st, st.MatchEngine, ocr, decisionNextNodes{
		Lock:    "EssenceFilterAfterBattleLockItemLog",
		Discard: "EssenceFilterAfterBattleDiscardItemLog",
		Skip:    "EssenceFilterAfterBattleCloseDetail",
	})
}
