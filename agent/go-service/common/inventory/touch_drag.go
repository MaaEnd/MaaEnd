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
	// 拖到目标格后保持短暂按住，确保 ADB 输入端收到完整的拖动手势。
	dragTargetHold = 200 * time.Millisecond
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

type touchDragRunner interface {
	touchTransferRunner
	move(contact int32, x, y int32) bool
}

func (r *touchTransferRuntime) move(contact int32, x, y int32) bool {
	return r.controller.PostTouchMove(contact, x, y, 1).Wait().Success()
}

func runTouchDrag(runner touchDragRunner, source maa.Rect, param touchDragParam, timeout time.Duration) (success bool) {
	if runner.stopping() || source[2] <= 0 || source[3] <= 0 {
		return false
	}
	// 必须在长按弹出操作菜单前缓存目标格；菜单会遮挡物品，弹出后不能再次定位目标。
	img, err := runner.screenshot()
	if err != nil || img == nil {
		log.Error().Err(err).Str("component", touchDragComponent).
			Msg("failed to capture drag destination")
		return false
	}
	destination, err := runner.RunRecognition(param.End, img)
	if err != nil || destination == nil || !destination.Hit {
		log.Error().Err(err).Str("component", touchDragComponent).Str("node", param.End).
			Msg("failed to recognize drag destination")
		return false
	}
	x, y := dragTargetPoint(destination.Box, param.EndOffset)

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
	if !runner.move(sourceContact, x, y) {
		log.Error().Str("component", touchDragComponent).Int32("x", x).Int32("y", y).
			Msg("failed to move held inventory item to destination")
		return false
	}
	return runner.wait(dragTargetHold)
}

func dragTargetPoint(box, offset maa.Rect) (int32, int32) {
	x := box[0] + offset[0]
	y := box[1] + offset[1]
	w := box[2] + offset[2]
	h := box[3] + offset[3]
	return int32(x + w/2), int32(y + h/2)
}
