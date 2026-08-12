package itemtransfer

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const ctrlVirtualKey = 17

type directActionRunner interface {
	RunActionDirect(
		actionType maa.ActionType,
		actionParam maa.ActionParam,
		box maa.Rect,
		recoDetail *maa.RecognitionDetail,
	) (*maa.ActionDetail, error)
}

// CtrlClickAction 在一次 Custom Action 内完成 Ctrl+Click，并保证离开动作前尝试释放 Ctrl。
type CtrlClickAction struct{}

var _ maa.CustomActionRunner = &CtrlClickAction{}

// Run 对 Pipeline 已解析的目标框执行 Ctrl+Click。
func (a *CtrlClickAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", componentName).Msg("ctrl click action received nil context or arg")
		return false
	}
	return runCtrlClickActions(ctx, arg.Box)
}

func runCtrlClickActions(runner directActionRunner, box maa.Rect) (success bool) {
	// 即使按下动作报告失败，也防御性地执行 KeyUp，避免控制器已生效但状态回报失败时留下按键。
	defer func() {
		if !runDirectAction(runner, maa.ActionTypeKeyUp, &maa.KeyUpParam{Key: ctrlVirtualKey}, maa.Rect{}) {
			success = false
		}
	}()

	if !runDirectAction(runner, maa.ActionTypeKeyDown, &maa.KeyDownParam{Key: ctrlVirtualKey}, maa.Rect{}) {
		return false
	}
	return runDirectAction(runner, maa.ActionTypeClick, &maa.ClickParam{}, box)
}

func runDirectAction(
	runner directActionRunner,
	actionType maa.ActionType,
	param maa.ActionParam,
	box maa.Rect,
) bool {
	detail, err := runner.RunActionDirect(actionType, param, box, nil)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Str("action", string(actionType)).Msg("ctrl click sub-action failed")
		return false
	}
	if detail == nil || !detail.Success {
		log.Error().
			Str("component", componentName).
			Str("action", string(actionType)).
			Bool("detail_present", detail != nil).
			Msg("ctrl click sub-action was not successful")
		return false
	}
	return true
}
