package essencefilter

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type EssenceFilterAfterBattleSkillDecisionAction struct{}

// EssenceFilterAfterBattleInventoryCommitAction stages or commits the observed rewards.
type EssenceFilterAfterBattleInventoryCommitAction struct{}

// EssenceFilterAfterBattleTierGateAction gates after-battle items by essence tier.
// Pipeline param "tier" must be "flawless" or "pure".
type EssenceFilterAfterBattleTierGateAction struct{}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner = &EssenceFilterAfterBattleSkillDecisionAction{}
	_ maa.CustomActionRunner = &EssenceFilterAfterBattleInventoryCommitAction{}
	_ maa.CustomActionRunner = &EssenceFilterAfterBattleTierGateAction{}
)

func (a *EssenceFilterAfterBattleTierGateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
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

	if st.Loot == nil {
		return false
	}
	st.Loot.mark = ""
	st.Loot.quality = gold
	if params.Tier == "pure" {
		st.Loot.quality = purple
	}
	if st.RowIndex < 1 || st.RowIndex > len(st.Loot.qualities) || st.Loot.qualities[st.RowIndex-1] != st.Loot.quality {
		return false
	}
	mark, err := readRewardMark(ctx, taskController(ctx))
	if err != nil {
		return inventoryFailed(err)
	}
	st.Loot.mark = mark
	st.CurrentLocked = mark == lockedMark
	return true
}

func (a *EssenceFilterAfterBattleSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 获取当前运行状态，如果状态为空则无法继续，直接返回
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		reportFocusByKey(ctx, "focus.error.no_run_state")
		return false
	}

	if st.Loot == nil {
		return false
	}
	var ocr matchapi.OCRInput
	if st.Loot.quality == gold {
		if st.MatchEngine == nil {
			reportFocusByKey(ctx, "focus.error.no_match_engine")
			return false
		}
		ocr = matchapi.OCRInput{Skills: st.CurrentSkills, Levels: st.CurrentSkillLevels}
	}
	mark, ok := decideRewardMark(ctx, st, ocr)
	if !ok {
		return false
	}
	next := decisionNextNodes{
		Lock:    "EssenceFilterAfterBattleLockItem",
		Discard: "EssenceFilterAfterBattleDiscardItem",
		Skip:    "EssenceFilterAfterBattleCloseDetail",
	}
	if !next.route(ctx, arg.CurrentTaskName, mark) {
		delete(st.Loot.expectedMarks, st.RowIndex)
		return false
	}
	return true
}

func (a *EssenceFilterAfterBattleInventoryCommitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		log.Error().Str("component", "EssenceFilter").Str("action", "AfterBattleInventoryCommit").
			Msg("no run state")
		reportFocusByKey(ctx, "focus.error.no_run_state")
		return false
	}
	if st.Loot != nil && st.Loot.committed {
		return true
	}
	before := st.Account.base
	if !commitAfterBattleInventory(st, st.Account.observedUID(ctx, taskController(ctx))) {
		return false
	}
	LogMXUHTML(ctx, rewardReport(st, before))
	return true
}
