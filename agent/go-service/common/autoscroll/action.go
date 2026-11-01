package autoscroll

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	componentName = "AutoScrollAction"
	// wheelDelta 是一次滚轮事件的标准增量（WHEEL_DELTA）。
	// 引擎通常每帧只把一次滚轮事件消费成一格，dy 给 -2400 也只滚一格，
	// 因此大位移必须拆成多次事件逐格发送。
	wheelDelta = 120
	// wheelEventInterval 是相邻滚轮事件的间隔，避免多格被合并进同一帧。
	wheelEventInterval = 30 * time.Millisecond
	// 临时管道节点仅通过 override 注入，无需在资源文件中预定义。
	wheelScrollEntry   = "__AutoScrollWheelScrollAction"
	swipeFallbackEntry = "__AutoScrollSwipeFallbackAction"
	// 非 PC 端回退时复用的原有滑动动作（本包不修改其实现）。
	legacySwipeAction = "AutoAltSwipeAction"
)

// autoScrollParam 描述一次列表滚动。
//
// PC（Win32）端使用 dx/dy 滚轮滚动；ADB / PlayCover / Linux / MacOS 等端
// 回退到 legacySwipeAction（原 Alt+滑动 / 触屏滑动），保证多端兼容。
// 本动作只负责滚动列表，不识别、不点击任何按钮。
type autoScrollParam struct {
	// Dx / Dy：滚轮滚动量，仅 PC 端生效。Dy 为负表示向下滚动，建议 120 的整数倍。
	Dx int `json:"dx,omitempty"`
	Dy int `json:"dy,omitempty"`
	// TargetOffset：滚动点相对识别框的偏移 [dx, dy, dw, dh]，可选。
	TargetOffset []int `json:"target_offset,omitempty"`
	// BeginOffset / EndOffset：非 PC 端回退到 legacySwipeAction 时的滑动偏移，
	// 语义与 AutoAltSwipeAction 的 custom_action_param 一致，相对识别框叠加。
	BeginOffset []int `json:"begin_offset,omitempty"`
	EndOffset   []int `json:"end_offset,omitempty"`
}

// AutoScrollAction 在 PC 端用滚轮滚动列表，其它端回退为原有滑动。
type AutoScrollAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &AutoScrollAction{}

func (a *AutoScrollAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", componentName).Msg("got nil custom action arg")
		return false
	}

	var p autoScrollParam
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
			log.Error().
				Err(err).
				Str("component", componentName).
				Str("custom_action_param", arg.CustomActionParam).
				Msg("failed to parse custom action param")
			return false
		}
	}

	if useWheelScroll() {
		return runWheelScroll(ctx, arg, &p)
	}
	return runSwipeFallback(ctx, arg, &p)
}

// useWheelScroll 判断当前控制器是否走 PC 滚轮。
// 仅 Win32 原生 Scroll 可用；其它端（ADB/PlayCover/Linux/MacOS 等）一律回退滑动。
// 按编码规范：判断控制器类型只用 pienv，不为取类型去 GetController() / GetInfo()。
// PI_CONTROLLER 缺失（kind=Unknown）时走滑动回退，保持历史多端行为。
func useWheelScroll() bool {
	return pienv.ControllerKind() == pienv.ControllerKindWin32
}

func runWheelScroll(ctx *maa.Context, arg *maa.CustomActionArg, p *autoScrollParam) bool {
	steps := wheelSteps(p.Dx, p.Dy)
	if steps == 0 {
		log.Debug().
			Str("component", componentName).
			Interface("scroll_param", map[string]any{"dx": p.Dx, "dy": p.Dy}).
			Msg("wheel scroll skipped: nothing to scroll")
		return true
	}

	for i := 0; i < steps; i++ {
		param := map[string]any{
			"dx": wheelChunk(p.Dx, i),
			"dy": wheelChunk(p.Dy, i),
		}
		if len(p.TargetOffset) == 4 {
			param["target_offset"] = p.TargetOffset
		}

		_, err := ctx.RunAction(wheelScrollEntry, arg.Box, "", map[string]any{
			wheelScrollEntry: map[string]any{
				"pre_delay":  0,
				"post_delay": 0,
				"rate_limit": 0,
				"action": map[string]any{
					"type":  "Scroll",
					"param": param,
				},
			},
		})
		if err != nil {
			log.Error().
				Err(err).
				Str("component", componentName).
				Interface("box", arg.Box).
				Interface("scroll_param", param).
				Int("step", i).
				Int("steps", steps).
				Msg("failed to run wheel scroll")
			return false
		}

		if i < steps-1 {
			time.Sleep(wheelEventInterval)
		}
	}

	log.Debug().
		Str("component", componentName).
		Interface("box", arg.Box).
		Interface("scroll_param", map[string]any{"dx": p.Dx, "dy": p.Dy}).
		Int("steps", steps).
		Msg("wheel scroll done")
	return true
}

// wheelSteps 计算需要发送多少次滚轮事件：每格一个事件，dx/dy 取较大者。
func wheelSteps(dx, dy int) int {
	steps := (absInt(dy) + wheelDelta - 1) / wheelDelta
	if s := (absInt(dx) + wheelDelta - 1) / wheelDelta; s > steps {
		steps = s
	}
	return steps
}

// wheelChunk 返回第 index 次滚轮事件的增量，绝对值不超过一格。
func wheelChunk(v, index int) int {
	if v == 0 {
		return 0
	}
	sign := 1
	if v < 0 {
		sign = -1
	}
	remaining := absInt(v) - index*wheelDelta
	if remaining <= 0 {
		return 0
	}
	if remaining > wheelDelta {
		remaining = wheelDelta
	}
	return sign * remaining
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func runSwipeFallback(ctx *maa.Context, arg *maa.CustomActionArg, p *autoScrollParam) bool {
	// 与历史行为一致：AutoAltSwipeAction 默认以识别框为起终点，再叠加 begin/end 偏移。
	swipeParam := map[string]any{}
	if len(p.BeginOffset) == 4 {
		swipeParam["begin_offset"] = p.BeginOffset
	}
	if len(p.EndOffset) == 4 {
		swipeParam["end_offset"] = p.EndOffset
	}

	actionParam := map[string]any{
		"custom_action": legacySwipeAction,
	}
	if len(swipeParam) > 0 {
		actionParam["custom_action_param"] = swipeParam
	}

	_, err := ctx.RunAction(swipeFallbackEntry, arg.Box, "", map[string]any{
		swipeFallbackEntry: map[string]any{
			"pre_delay":  0,
			"post_delay": 0,
			"rate_limit": 0,
			"action": map[string]any{
				"type":  "Custom",
				"param": actionParam,
			},
		},
	})
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("legacy_action", legacySwipeAction).
			Interface("box", arg.Box).
			Interface("swipe_param", swipeParam).
			Msg("failed to run swipe fallback")
		return false
	}

	log.Debug().
		Str("component", componentName).
		Str("legacy_action", legacySwipeAction).
		Interface("box", arg.Box).
		Interface("swipe_param", swipeParam).
		Msg("swipe fallback done")
	return true
}
