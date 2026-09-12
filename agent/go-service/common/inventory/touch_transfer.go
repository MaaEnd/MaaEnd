package inventory

import (
	"encoding/json"
	"errors"
	"image"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	touchTransferComponent = "InventoryTransferTouchAction"
	sourceTouchDownNode    = "__InventoryTransferSourceTouchDown"
	buttonTouchDownNode    = "__InventoryTransferButtonTouchDown"
	// 触点 0 在识别期间持续按住源物品，触点 1 仅用于点击操作菜单。
	sourceContact = 0
	buttonContact = 1
	// 限制菜单出现的等待时间，避免持续按压；不是重复执行转移动作的次数。
	menuWaitTimeout = 5 * time.Second
	// 未命中后的截图轮询节流，菜单一旦识别到就立即点击。
	menuPollInterval = 100 * time.Millisecond
	// 第二触点抬起后继续保持源物品，避免同一帧内关闭菜单并将点击穿透到下层物品。
	sourceReleaseDelay = 100 * time.Millisecond
)

type touchTransferParam struct {
	Mode string `json:"mode"`
}

// TouchTransferAction 在触屏端保持源物品长按，用第二触点点击已识别的转移按钮。
type TouchTransferAction struct{}

var _ maa.CustomActionRunner = &TouchTransferAction{}

// Run 供 ADB 内部资源调用；mode 选择操作图标，所有退出路径都尝试释放已按下的触点。
func (a *TouchTransferAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	prefix, err := parseTouchTransferMode(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Str("component", touchTransferComponent).Msg("invalid touch transfer mode")
		return false
	}
	tasker := ctx.GetTasker()
	if tasker == nil || tasker.GetController() == nil {
		log.Error().Str("component", touchTransferComponent).Msg("touch transfer controller is unavailable")
		return false
	}
	runtime := &touchTransferRuntime{Context: ctx, controller: tasker.GetController()}
	return runTouchTransfer(runtime, prefix, arg.Box, menuWaitTimeout)
}

func parseTouchTransferMode(raw string) (string, error) {
	var param touchTransferParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return "", err
	}
	switch param.Mode {
	case "all":
		return "__InventoryTransferAllButton", nil
	case "stack":
		return "__InventoryTransferStackButton", nil
	case "half":
		return "__InventoryTransferHalfButton", nil
	default:
		return "", errors.New("mode must be all, stack or half")
	}
}

type touchTransferRunner interface {
	pipelineActionRunner
	RunRecognition(entry string, img image.Image, override ...any) (*maa.RecognitionDetail, error)
	screenshot() (image.Image, error)
	stopping() bool
	release(contact int32) bool
	wait(duration time.Duration) bool
}

type touchTransferRuntime struct {
	*maa.Context
	controller *maa.Controller
}

func (r *touchTransferRuntime) screenshot() (image.Image, error) {
	if !r.controller.PostScreencap().Wait().Success() {
		return nil, errors.New("screencap failed")
	}
	return r.controller.CacheImage()
}

func (r *touchTransferRuntime) stopping() bool {
	return r.GetTasker().Stopping()
}

func (r *touchTransferRuntime) release(contact int32) bool {
	// 任务停止后 RunAction 可能不再执行，清理直接交给控制器，不依赖 Pipeline 继续调度。
	return r.controller.PostTouchUp(contact).Wait().Success()
}

func (r *touchTransferRuntime) wait(duration time.Duration) bool {
	if r.stopping() {
		return false
	}
	time.Sleep(duration)
	return !r.stopping()
}

func runTouchTransfer(runner touchTransferRunner, buttonPrefix string, source maa.Rect, timeout time.Duration) (success bool) {
	if runner.stopping() || source[2] <= 0 || source[3] <= 0 {
		return false
	}
	buttonAttempted := false
	// 动作失败不代表输入没有生效，因此在尝试按下之前就建立清理责任。
	defer func() {
		if buttonAttempted {
			if !releaseTransferContact(runner, buttonContact) {
				success = false
			}
			if !runner.wait(sourceReleaseDelay) {
				success = false
			}
		}
		if !releaseTransferContact(runner, sourceContact) {
			success = false
		}
		if runner.stopping() {
			success = false
		}
	}()
	if !runTransferStage(runner, touchTransferComponent, sourceTouchDownNode, source) {
		return false
	}
	button, ok := waitTransferButton(runner, buttonPrefix, timeout)
	if !ok || runner.stopping() {
		return false
	}
	buttonAttempted = true
	return runTransferStage(runner, touchTransferComponent, buttonTouchDownNode, button)
}

func releaseTransferContact(runner touchTransferRunner, contact int32) bool {
	if runner.release(contact) {
		return true
	}
	log.Error().Str("component", touchTransferComponent).Int32("contact", contact).
		Msg("failed to release inventory transfer contact")
	return false
}

func waitTransferButton(runner touchTransferRunner, prefix string, timeout time.Duration) (maa.Rect, bool) {
	deadline := time.Now().Add(timeout)
	for !runner.stopping() && time.Now().Before(deadline) {
		img, err := runner.screenshot()
		if err != nil || img == nil {
			log.Error().Err(err).Str("component", touchTransferComponent).Msg("inventory menu screenshot failed")
			return maa.Rect{}, false
		}
		// 两个 ROI 使用同一帧，左侧未命中才匹配右侧，避免扫描中间物品网格。
		for _, side := range []string{"Left", "Right"} {
			if runner.stopping() || !time.Now().Before(deadline) {
				return maa.Rect{}, false
			}
			node := prefix + side
			detail, err := runner.RunRecognition(node, img)
			if err != nil || detail == nil {
				log.Error().Err(err).Str("component", touchTransferComponent).Str("node", node).
					Msg("inventory menu recognition failed")
				return maa.Rect{}, false
			}
			if detail.Hit {
				if runner.stopping() || !time.Now().Before(deadline) || detail.Box[2] <= 0 || detail.Box[3] <= 0 {
					return maa.Rect{}, false
				}
				log.Debug().Str("component", touchTransferComponent).Str("node", node).
					Interface("box", detail.Box).Msg("inventory transfer button found")
				return detail.Box, true
			}
		}
		if remaining := time.Until(deadline); remaining > 0 && !runner.stopping() {
			time.Sleep(min(menuPollInterval, remaining))
		}
	}
	if !runner.stopping() {
		log.Warn().Str("component", touchTransferComponent).Str("button", prefix).
			Msg("inventory transfer menu did not appear before deadline")
	}
	return maa.Rect{}, false
}
