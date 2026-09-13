package inventory

import (
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	moveRepoToBagComponent = "InventoryMoveRepoItemToBagAction"
	moveRepoToBagSource    = "StashBackpackFindCurrentItemInRepo"
	moveRepoToBagTarget    = "StashBackpackFindCurrentItemInBag"
	// Win32 直接拖动使用与原 Pipeline Swipe 相同的持续时间，避免拖动被识别为点击。
	moveRepoToBagDuration = 400 * time.Millisecond
	// 拖到目标格后保持短暂按住，确保 ADB 输入端收到完整的拖动手势。
	dragTargetHold = 200 * time.Millisecond
)

// MoveRepoItemToBagAction 将仓库物品移动到背包中对应的目标格。
// 起点和终点由调用方的 And 识别在同一帧提供；ADB 仅在长按后等待操作菜单出现。
type MoveRepoItemToBagAction struct{}

var _ maa.CustomActionRunner = &MoveRepoItemToBagAction{}

// Run 根据控制器类型执行桌面端直接拖动或 ADB 长按后拖动。
func (a *MoveRepoItemToBagAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	source, ok := findCombinedRecognitionBox(arg.RecognitionDetail, moveRepoToBagSource)
	if !ok {
		log.Error().Str("component", moveRepoToBagComponent).Str("node", moveRepoToBagSource).
			Msg("repository source was not provided by combined recognition")
		return false
	}
	destination, ok := findCombinedRecognitionBox(arg.RecognitionDetail, moveRepoToBagTarget)
	if !ok {
		log.Error().Str("component", moveRepoToBagComponent).Str("node", moveRepoToBagTarget).
			Msg("backpack destination was not provided by combined recognition")
		return false
	}
	tasker := ctx.GetTasker()
	if tasker == nil || tasker.GetController() == nil {
		log.Error().Str("component", moveRepoToBagComponent).Msg("inventory move controller is unavailable")
		return false
	}
	runtime := &touchTransferRuntime{Context: ctx, controller: tasker.GetController()}
	controlType, err := control.GetControlType(runtime.controller)
	if err != nil {
		log.Error().Err(err).Str("component", moveRepoToBagComponent).Msg("failed to determine controller type")
		return false
	}
	switch controlType {
	case control.CONTROL_TYPE_WIN32:
		return runRepoToBagDesktop(runtime, source, destination)
	case control.CONTROL_TYPE_ADB:
		return runRepoToBagADB(runtime, source, destination, menuWaitTimeout)
	default:
		log.Error().Str("component", moveRepoToBagComponent).Str("control_type", controlType).
			Msg("inventory move action does not support this controller")
		return false
	}
}

type repoToBagADBRunner interface {
	touchTransferRunner
	move(contact int32, x, y int32) bool
}

func (r *touchTransferRuntime) move(contact int32, x, y int32) bool {
	return r.controller.PostTouchMove(contact, x, y, 1).Wait().Success()
}

type repoToBagRunner interface {
	repoToBagADBRunner
	swipe(source, destination maa.Rect, duration time.Duration) bool
}

func (r *touchTransferRuntime) swipe(source, destination maa.Rect, duration time.Duration) bool {
	sourceX, sourceY := rectCenter(source)
	destinationX, destinationY := rectCenter(destination)
	return r.controller.PostSwipeV2(sourceX, sourceY, destinationX, destinationY, duration, sourceContact, 1).Wait().Success()
}

func runRepoToBagDesktop(runner repoToBagRunner, source, destination maa.Rect) bool {
	if runner.stopping() || !validRect(source) || !validRect(destination) {
		return false
	}
	if !runner.swipe(source, destination, moveRepoToBagDuration) {
		log.Error().Str("component", moveRepoToBagComponent).Msg("failed to drag repository item on desktop controller")
		return false
	}
	return true
}

func runRepoToBagADB(runner repoToBagADBRunner, source, destination maa.Rect, timeout time.Duration) (success bool) {
	if runner.stopping() || source[2] <= 0 || source[3] <= 0 {
		return false
	}
	if !validRect(destination) {
		return false
	}

	// 从尝试按下起负责清理；取消或菜单识别失败也必须释放，不能依赖后续节点。
	defer func() {
		if !runner.release(sourceContact) {
			log.Error().Str("component", moveRepoToBagComponent).Msg("failed to release inventory move contact")
			success = false
		}
		if runner.stopping() {
			success = false
		}
	}()
	if !runTransferStage(runner, moveRepoToBagComponent, sourceTouchDownNode, source) {
		return false
	}
	// 菜单只用于确认长按已生效，不能点击“转移一组”，否则可能占用新的背包格。
	if _, ok := waitTransferButton(runner, "__InventoryTransferStackButton", timeout); !ok || runner.stopping() {
		return false
	}
	x, y := rectCenter(destination)
	if !runner.move(sourceContact, x, y) {
		log.Error().Str("component", moveRepoToBagComponent).Int32("x", x).Int32("y", y).
			Msg("failed to move held repository item to backpack destination")
		return false
	}
	return runner.wait(dragTargetHold)
}

func findCombinedRecognitionBox(detail *maa.RecognitionDetail, node string) (maa.Rect, bool) {
	if detail == nil {
		return maa.Rect{}, false
	}
	if detail.Name == node && detail.Hit && validRect(detail.Box) {
		return detail.Box, true
	}
	for _, child := range detail.CombinedResult {
		if box, ok := findCombinedRecognitionBox(child, node); ok {
			return box, true
		}
	}
	return maa.Rect{}, false
}

func validRect(rect maa.Rect) bool {
	return rect[2] > 0 && rect[3] > 0
}

func rectCenter(rect maa.Rect) (int32, int32) {
	return int32(rect[0] + rect[2]/2), int32(rect[1] + rect[3]/2)
}
}
