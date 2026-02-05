package essence

import (
	"encoding/json"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type applyLockParam struct {
	Target         []int `json:"target"`          // 单一目标坐标 [x, y]（优先生效）
	OnlyDecision   string `json:"only_decision"`  // 仅当 decision 匹配才执行
	TreasureTarget []int `json:"treasure_target"` // [x, y]
	MaterialTarget []int `json:"material_target"` // [x, y]
}

// EssenceApplyLockAction 根据判定结果点击锁定/解锁按钮（固定坐标）。
type EssenceApplyLockAction struct{}

// Run 实现 CustomActionRunner 接口。
func (a *EssenceApplyLockAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param := applyLockParam{
		TreasureTarget: []int{1185, 360},
		MaterialTarget: []int{1185, 360},
	}
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
			log.Warn().Err(err).Msg("essence: failed to parse apply lock param, using defaults")
		}
	}

	var decision struct {
		Decision string `json:"decision"`
	}
	if arg.RecognitionDetail.DetailJson != "" {
		if err := json.Unmarshal([]byte(arg.RecognitionDetail.DetailJson), &decision); err != nil {
			log.Error().Err(err).Msg("essence: failed to parse recognition detail")
			return false
		}
	} else {
		log.Warn().Msg("essence: missing recognition detail for apply lock action")
		return false
	}

	if param.OnlyDecision != "" && decision.Decision != param.OnlyDecision {
		log.Info().
			Str("decision", decision.Decision).
			Str("onlyDecision", param.OnlyDecision).
			Msg("essence: decision mismatch, skip clicking")
		return true
	}

	target := param.Target
	if len(target) < 2 {
		target = param.MaterialTarget
		if decision.Decision == "Treasure" {
			target = param.TreasureTarget
		}
	}
	if len(target) < 2 {
		log.Warn().Msg("essence: invalid lock target coordinate")
		return false
	}

	log.Info().
		Str("decision", decision.Decision).
		Int("x", target[0]).
		Int("y", target[1]).
		Msg("essence: clicking lock toggle")

	ctx.GetTasker().GetController().PostClick(int32(target[0]), int32(target[1]))
	return true
}

