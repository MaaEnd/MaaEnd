package autoalt

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	autoCtrlClickKeyDownNode = "__AutoCtrlClickCtrlKeyDownAction"
	autoCtrlClickMouseNode   = "__AutoCtrlClickMouseClickAction"
	autoCtrlClickKeyUpNode   = "__AutoCtrlClickCtrlKeyUpAction"
)

// AutoCtrlClickAction 通过可被平台资源覆盖的 Pipeline 子节点执行 Ctrl+Click。
type AutoCtrlClickAction struct{}

var _ maa.CustomActionRunner = &AutoCtrlClickAction{}

// Run 对 Pipeline 已解析的目标框执行 Ctrl+Click，并保证离开动作前尝试释放 Ctrl。
func (a *AutoCtrlClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", "AutoCtrlClickAction").Msg("modifier click action received nil context or arg")
		return false
	}

	return runModifierClickActions(
		ctx,
		"AutoCtrlClickAction",
		autoCtrlClickKeyDownNode,
		autoCtrlClickMouseNode,
		autoCtrlClickKeyUpNode,
		arg.Box,
	)
}
