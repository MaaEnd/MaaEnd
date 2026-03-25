package essencefilter

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type EssenceFilterAfterBattleSkillDecisionAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &EssenceFilterAfterBattleSkillDecisionAction{}

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
