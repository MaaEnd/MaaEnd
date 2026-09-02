package aerosalvage

import (
	"fmt"
	"math"
	"slices"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	aeroSalvageConfigureSwipeActionName = "AeroSalvageConfigureSwipeAction"
	aeroSalvageSwipeBalloonNode         = "AeroSalvageSwipeBalloon"
	// aeroSalvageStateRepeatLimit 熔断阈值：同一 balloon state 累计出现超过该次数判定卡死。
	aeroSalvageStateRepeatLimit = 5
)

var _ maa.CustomActionRunner = &ConfigureSwipeAction{}

// ConfigureSwipeAction validates the observed balloon state against the placement plan
// and applies the entry the state maps to the next Swipe node.
type ConfigureSwipeAction struct{}

// Run advances the plan index by the bijection between balloon state and plan progress,
// then overrides AeroSalvageSwipeBalloon with the entry to swipe. Any validation failure
// aborts the task immediately by returning false.
func (a *ConfigureSwipeAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", aeroSalvageConfigureSwipeActionName).Msg("context or custom action arg is nil")
		return false
	}

	override, placement, end, err := buildSwipeOverride()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", aeroSalvageConfigureSwipeActionName).
			Int("balloon_state_index", balloonStateIndex).
			Int("placement_count", len(balloonPlacements)).
			Ints("balloon_state", balloonObservedState).
			Msg("failed to configure balloon swipe")
		return false
	}
	if err := ctx.OverridePipeline(override); err != nil {
		log.Error().
			Err(err).
			Str("component", aeroSalvageConfigureSwipeActionName).
			Int("balloon_state_index", balloonStateIndex).
			Msg("failed to override balloon swipe")
		return false
	}

	log.Debug().
		Str("component", aeroSalvageConfigureSwipeActionName).
		Int("balloon_state_index", balloonStateIndex).
		Str("balloon_count_node", placement.Config.CountNode).
		Ints("target_position", []int{placement.TargetPos.X, placement.TargetPos.Y}).
		Ints("swipe_end", end).
		Msg("balloon swipe configured")
	return true
}

// buildSwipeOverride 校验观测到的 balloon state 并推进方案进度，返回状态所对应条目的
// Swipe 覆写。balloon state 与 index 一一映射：观测状态必须恰好对应当前条目（重试）
// 或下一个条目（推进），任何偏离或熔断触发都视为致命错误。
func buildSwipeOverride() (map[string]any, balloonPlacement, []int, error) {
	if balloonStateIndex < 0 || balloonStateIndex >= len(balloonPlacements) {
		return nil, balloonPlacement{}, nil, fmt.Errorf("balloon state index %d is out of plan range %d", balloonStateIndex, len(balloonPlacements))
	}
	if len(balloonObservedState) != len(balloonPlanSlots) {
		return nil, balloonPlacement{}, nil, fmt.Errorf("observed balloon state size %d mismatches plan slot count %d", len(balloonObservedState), len(balloonPlanSlots))
	}

	// 熔断：同一 balloon state 累计出现超过阈值说明卡死（合法推进必然改变状态）。
	signature := fmt.Sprint(balloonObservedState)
	if signature == balloonStateSignature {
		balloonStateRepeatCount++
	} else {
		balloonStateSignature = signature
		balloonStateRepeatCount = 1
	}
	if balloonStateRepeatCount > aeroSalvageStateRepeatLimit {
		return nil, balloonPlacement{}, nil, fmt.Errorf("balloon state %v repeated %d times", balloonObservedState, balloonStateRepeatCount)
	}

	switch {
	case slices.Equal(balloonObservedState, balloonPathStates[balloonStateIndex]):
		// 重试：swipe 未生效，状态对应的 index 不变。
	case balloonStateIndex+1 < len(balloonPlacements) && slices.Equal(balloonObservedState, balloonPathStates[balloonStateIndex+1]):
		balloonStateIndex++
	default:
		// 偏离路径（含全零对应 index == len 的不可达状态）一律致命。
		return nil, balloonPlacement{}, nil, fmt.Errorf("observed balloon state %v deviates from the placement path at index %d", balloonObservedState, balloonStateIndex)
	}

	placement := balloonPlacements[balloonStateIndex]
	if placement.Config.CountNode == "" {
		return nil, balloonPlacement{}, nil, fmt.Errorf("placement %d has an empty balloon count node", balloonStateIndex)
	}
	point, ok := gridPointsCache[placement.TargetPos]
	if !ok {
		return nil, balloonPlacement{}, nil, fmt.Errorf("target grid position %+v is absent from the grid cache", placement.TargetPos)
	}
	if math.IsNaN(point.X) || math.IsInf(point.X, 0) || math.IsNaN(point.Y) || math.IsInf(point.Y, 0) {
		return nil, balloonPlacement{}, nil, fmt.Errorf("target grid position %+v has invalid screen coordinates", placement.TargetPos)
	}

	end := []int{int(math.Round(point.X)), int(math.Round(point.Y)), 1, 1}
	override := map[string]any{
		aeroSalvageSwipeBalloonNode: map[string]any{
			"begin": placement.Config.CountNode,
			"end":   end,
		},
	}
	return override, placement, end, nil
}
