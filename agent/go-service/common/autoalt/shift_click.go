package autoalt

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	autoShiftClickKeyDownNode = "__AutoShiftClickShiftKeyDownAction"
	autoShiftClickMouseNode   = "__AutoShiftClickMouseClickAction"
	autoShiftClickKeyUpNode   = "__AutoShiftClickShiftKeyUpAction"
)

// AutoShiftClickAction 通过可被平台资源覆盖的 Pipeline 子节点执行 Shift+Click。
type AutoShiftClickAction struct{}

var _ maa.CustomActionRunner = &AutoShiftClickAction{}

// Run 对 Pipeline 已解析的目标框执行 Shift+Click，并保证离开动作前尝试释放 Shift。
func (a *AutoShiftClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "AutoShiftClickAction").Msg("modifier click action received nil context or arg")
		return false
	}

	return runModifierClickActions(
		ctx,
		"AutoShiftClickAction",
		autoShiftClickKeyDownNode,
		autoShiftClickMouseNode,
		autoShiftClickKeyUpNode,
		arg.Box,
	)
}
