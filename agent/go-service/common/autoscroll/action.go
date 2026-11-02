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
	wheelScrollEntry = "__AutoScrollWheelScrollAction"
)

// autoScrollParam 描述一次列表滚动。
//
// 仅对 Win PC（Win32）实现滚轮滚动。ADB / PlayCover / Linux / MacOS 等端
// 由于无法进行实机测试，此处留空：动作直接返回成功且不做任何滚动，
// 由 Pipeline 的 next 继续原有流程（例如直接传送重走）。
// 本动作只负责滚动列表，不识别、不点击任何按钮。
type autoScrollParam struct {
	// Dx / Dy：滚轮滚动量，仅 PC 端生效。Dy 为负表示向下滚动，建议 120 的整数倍。
	Dx int `json:"dx,omitempty"`
	Dy int `json:"dy,omitempty"`
	// TargetOffset：滚动点相对识别框的偏移 [dx, dy, dw, dh]，可选。
	TargetOffset []int `json:"target_offset,omitempty"`
}

// AutoScrollAction 在 PC 端用滚轮滚动列表；其它端不做任何操作。
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
	// 非 Win PC 端（ADB / PlayCover / Linux / MacOS 等）无法实机验证滚动参数，
	// 此处留空：不滚动、直接返回成功，由 Pipeline 的 next 继续原有流程。
	log.Debug().
		Str("component", componentName).
		Str("controller_kind", pienv.ControllerKind()).
		Msg("skip scroll: only Win PC is supported")
	return true
}

// useWheelScroll 判断当前控制器是否走 PC 滚轮。
// 仅 Win32 原生 Scroll 可用；其它端不做任何滚动操作。
// 按编码规范：判断控制器类型只用 pienv，不为取类型去 GetController() / GetInfo()。
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

		detail, err := ctx.RunAction(wheelScrollEntry, arg.Box, "", map[string]any{
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
		if detail == nil || !detail.Success {
			log.Error().
				Str("component", componentName).
				Interface("box", arg.Box).
				Interface("scroll_param", param).
				Int("step", i).
				Int("steps", steps).
				Bool("detail_present", detail != nil).
				Msg("wheel scroll action was not successful")
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
