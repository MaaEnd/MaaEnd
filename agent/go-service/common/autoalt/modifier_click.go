package autoalt

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type pipelineActionRunner interface {
	RunAction(
		entry string,
		box maa.Rect,
		recognitionDetail string,
		override ...any,
	) (*maa.ActionDetail, error)
}

func runModifierClickActions(
	runner pipelineActionRunner,
	component string,
	keyDownNode string,
	clickNode string,
	keyUpNode string,
	box maa.Rect,
) (success bool) {
	// 即使按下或点击动作失败，也尝试释放修饰键，避免控制器已生效但状态回报失败时留下按键。
	defer func() {
		if !runPipelineAction(runner, component, keyUpNode, maa.Rect{}) {
			success = false
		}
	}()

	if !runPipelineAction(runner, component, keyDownNode, maa.Rect{}) {
		return false
	}
	return runPipelineAction(runner, component, clickNode, box)
}

func runPipelineAction(
	runner pipelineActionRunner,
	component string,
	node string,
	box maa.Rect,
) bool {
	detail, err := runner.RunAction(node, box, "", nil)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", component).
			Str("node", node).
			Msg("modifier click pipeline action failed")
		return false
	}
	if detail == nil || !detail.Success {
		log.Error().
			Str("component", component).
			Str("node", node).
			Bool("detail_present", detail != nil).
			Msg("modifier click pipeline action was not successful")
		return false
	}
	return true
}
