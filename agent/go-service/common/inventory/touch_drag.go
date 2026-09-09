package inventory

import (
	"encoding/json"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	touchDragComponent = "InventoryDragTouchAction"
	dragTouchMoveNode  = "__InventoryDragTouchMove"
)

type touchDragParam struct {
	End       string   `json:"end"`
	EndOffset maa.Rect `json:"end_offset"`
}

// DragTouchAction 长按源物品，确认操作菜单出现后拖到已识别的目标格，供触屏端合并堆叠。
type DragTouchAction struct{}

var _ maa.CustomActionRunner = &DragTouchAction{}

// Run 的外层 target 指定源格，end 引用目标格识别节点；动作不负责寻找物品或判断补充数量。
func (a *DragTouchAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	var param touchDragParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil || strings.TrimSpace(param.End) == "" {
		log.Error().Err(err).Str("component", touchDragComponent).Msg("drag requires a recognized destination node")
		return false
	}
	tasker := ctx.GetTasker()
	if tasker == nil || tasker.GetController() == nil {
		log.Error().Str("component", touchDragComponent).Msg("touch drag controller is unavailable")
		return false
	}
	runtime := &touchTransferRuntime{Context: ctx, controller: tasker.GetController()}
	return runTouchDrag(runtime, arg.Box, param, menuWaitTimeout)
}

func runTouchDrag(runner touchTransferRunner, source maa.Rect, param touchDragParam, timeout time.Duration) (success bool) {
	if runner.stopping() || source[2] <= 0 || source[3] <= 0 {
		return false
	}
	// 从尝试按下起负责清理；取消或菜单识别失败也必须释放，不能依赖后续节点。
	defer func() {
		if !runner.release(sourceContact) {
			log.Error().Str("component", touchDragComponent).Msg("failed to release inventory drag contact")
			success = false
		}
		if runner.stopping() {
			success = false
		}
	}()
	if !runTransferStage(runner, touchDragComponent, sourceTouchDownNode, source) {
		return false
	}
	// 菜单只用于确认长按已生效，不能点击“转移一组”，否则可能占用新的背包格。
	if _, ok := waitTransferButton(runner, "__InventoryTransferStackButton", timeout); !ok || runner.stopping() {
		return false
	}
	return runTransferStage(runner, touchDragComponent, dragTouchMoveNode, source, map[string]any{
		dragTouchMoveNode: map[string]any{
			"target":        param.End,
			"target_offset": param.EndOffset,
		},
	})
}
