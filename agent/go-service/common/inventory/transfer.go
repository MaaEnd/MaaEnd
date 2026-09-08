package inventory

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const transferClickNode = "__InventoryTransferClickAction"

type pipelineActionRunner interface {
	RunAction(entry string, box maa.Rect, recognitionDetail string, override ...any) (*maa.ActionDetail, error)
}

func runTransfer(ctx *maa.Context, arg *maa.CustomActionArg, component, prefix, mode string) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", component).Msg("inventory transfer received nil context or arg")
		return false
	}
	return runTransferActions(ctx, component, prefix, mode, arg.Box)
}

func runTransferActions(runner pipelineActionRunner, component, prefix, mode string, box maa.Rect) (success bool) {
	// 开始阶段即使报错，也可能已经产生输入；所有正常返回路径都必须尝试收尾。
	defer func() {
		if !runTransferStage(runner, component, prefix+"EndAction", box) {
			success = false
		}
	}()

	if !runTransferStage(runner, component, prefix+"BeginAction", box) {
		return false
	}
	// 桌面端共用 Click；ADB 资源将其覆盖为完整手势，只有 Custom 动作会使用此模式参数。
	override := map[string]any{
		transferClickNode: map[string]any{
			"custom_action_param": touchTransferParam{Mode: mode},
		},
	}
	return runTransferStage(runner, component, transferClickNode, box, override)
}

func runTransferStage(runner pipelineActionRunner, component, node string, box maa.Rect, override ...any) bool {
	detail, err := runner.RunAction(node, box, "", override...)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("node", node).
			Msg("inventory transfer pipeline action failed")
		return false
	}
	if detail == nil || !detail.Success {
		log.Error().Str("component", component).Str("node", node).
			Bool("detail_present", detail != nil).
			Msg("inventory transfer pipeline action was not successful")
		return false
	}
	return true
}
