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
	// Use the confirmed detail-panel anchor for both buttons on one frame.
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return false
	}
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		return false
	}
	for _, candidate := range []struct{ node, mark string }{
		{"EssenceFilterAfterBattleCheckLocked", lockedMark},
		{"EssenceFilterAfterBattleCheckDiscarded", discarded},
		{"EssenceFilterAfterBattleClickDiscardItem", unmarked},
	} {
		res, err := ctx.RunRecognition(candidate.node, img, nil)
		if err != nil {
			return inventoryFailed(err)
		}
		if res != nil && res.Hit {
			st.Loot.mark = candidate.mark
			st.CurrentLocked = candidate.mark == lockedMark
			return true
		}
	}
	return false
}

func (a *EssenceFilterAfterBattleSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 获取当前运行状态，如果状态为空则无法继续，直接返回
	st := currentRun
	if st == nil || st.TaskID != arg.TaskID {
		reportFocusByKey(ctx, "focus.error.no_run_state")
		return false
	}

	// 将识别到的三个技能名称和等级存入ocr结构中给api调用
	ocr := matchapi.OCRInput{
		Skills: [3]string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]},             // 这三条不要求严格按 slot1/slot2/slot3 顺序；引擎会基于 pool 自动重排（若能唯一推断）
		Levels: [3]int{st.CurrentSkillLevels[0], st.CurrentSkillLevels[1], st.CurrentSkillLevels[2]}, // 对应等级（1..6）
	}

	if st.MatchEngine == nil {
		reportFocusByKey(ctx, "focus.error.no_match_engine")
		return false
	}
	if st.Loot == nil {
		return false
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
	return commitAfterBattleInventory(st, st.Account.observedUID(ctx, taskController(ctx)))
}
