package quantizedsliding

import (
	"errors"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func (a *QuantizedSlidingAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", quantizedSlidingActionName).
			Msg("got nil custom action arg")
		return false
	}

	a.initLogger(arg.CurrentTaskName)

	if !isQuantizedSlidingActionNode(arg.CurrentTaskName) {
		return a.runInternalPipeline(ctx, arg)
	}

	if !a.loadActionParams(arg.CustomActionParam) {
		return false
	}

	return a.dispatchActionNode(ctx, arg)
}

func (a *QuantizedSlidingAction) dispatchActionNode(ctx *maa.Context, arg *maa.CustomActionArg) bool {

	switch arg.CurrentTaskName {
	case nodeQuantizedSlidingMain:
		return a.handleMain(ctx, arg)
	case nodeQuantizedSlidingFindStart:
		return a.handleFindStart(ctx, arg)
	case nodeQuantizedSlidingGetMaxQuantity:
		return a.handleGetMaxQuantity(ctx, arg)
	case nodeQuantizedSlidingFindEnd:
		return a.handleFindEnd(ctx, arg)
	case nodeQuantizedSlidingCheckQuantity:
		return a.handleCheckQuantity(ctx, arg)
	case nodeQuantizedSlidingDone:
		return a.handleDone(ctx, arg)
	default:
		a.logger.Warn().Msg("unknown current task name")
		return false
	}
}

func (a *QuantizedSlidingAction) handleMain(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	a.resetState()

	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}

	if len(a.QuantityBox) != 4 {
		a.logger.Error().
			Ints("quantity_box", a.QuantityBox).
			Msg("invalid quantity box, expected [x,y,w,h]")
		return false
	}

	end, err := buildSwipeEnd(a.Direction)
	if err != nil {
		a.logger.Error().
			Str("direction", a.Direction).
			Err(err).
			Msg("invalid direction")
		return false
	}

	override := buildMainInitializationOverride(end, a.QuantityBox, a.QuantityFilter, a.QuantityOnlyRec)

	if err := ctx.OverridePipeline(override); err != nil {
		a.logger.Error().Err(err).Msg("failed to override pipeline for main initialization")
		return false
	}

	initializationLog := a.logger.Info().
		Str("direction", a.Direction).
		Ints("end", end).
		Ints("quantity_roi", a.QuantityBox).
		Bool("green_mask", a.GreenMask).
		Bool("quantity_filter_enabled", a.QuantityFilter != nil)

	if a.QuantityFilter != nil {
		initializationLog = initializationLog.
			Int("quantity_filter_method", a.QuantityFilter.Method).
			Ints("quantity_filter_lower", a.QuantityFilter.Lower).
			Ints("quantity_filter_upper", a.QuantityFilter.Upper)
	}

	initializationLog.Msg("main initialization completed with pipeline overrides")
	return true
}

func (a *QuantizedSlidingAction) handleFindStart(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil || arg.RecognitionDetail == nil {
		a.logger.Error().Msg("recognition detail is nil")
		return false
	}

	box, ok := readHitBox(arg.RecognitionDetail)
	if !ok {
		a.logger.Error().Msg("failed to extract start box from recognition detail")
		return false
	}

	a.startBox = box
	a.logger.Info().Ints("start_box", a.startBox).Msg("start box recorded")
	return true
}

func (a *QuantizedSlidingAction) handleGetMaxQuantity(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}
	if arg == nil {
		a.logger.Error().Msg("custom action arg is nil")
		return false
	}

	maxQuantity, err := readQuantityValue(arg.RecognitionDetail)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to parse max quantity from ocr")
		return false
	}

	a.maxQuantity = maxQuantity

	// 先钳制 Target，再计算 nextNode，避免 maxQuantity==1 时的除零问题
	if a.ClampTargetToMax && a.maxQuantity < a.Target {
		originalTarget := a.Target
		a.Target = a.maxQuantity
		a.logger.Warn().
			Int("original_target", originalTarget).
			Int("clamped_target", a.Target).
			Int("max_quantity", a.maxQuantity).
			Msg("target clamped to max quantity")
	}

	nextNode, err := resolveMaxQuantityNext(a.maxQuantity, a.Target)
	if err != nil {
		a.logger.Error().
			Int("max_quantity", a.maxQuantity).
			Int("target", a.Target).
			Msg("max quantity lower than target")
		return false
	}
	if nextNode != "" {
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nextNode, buttonTarget{}, 0, a.GreenMask); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Int("max_quantity", a.maxQuantity).
				Int("target", a.Target).
				Str("next", nextNode)
			if errors.Is(err, errCheckQuantityBranchNextOverride) {
				logEvent.Msg("failed to override next for direct-done branch")
			} else {
				logEvent.Msg("failed to override direct-done branch")
			}
			return false
		}

		a.logger.Info().
			Int("max_quantity", a.maxQuantity).
			Int("target", a.Target).
			Str("next", nextNode).
			Msg("max quantity already satisfies target, branch to done")
		return true
	}

	a.logger.Info().
		Int("max_quantity", a.maxQuantity).
		Int("target", a.Target).
		Msg("max quantity parsed")
	return true
}

func (a *QuantizedSlidingAction) handleFindEnd(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}
	if arg == nil || arg.RecognitionDetail == nil {
		a.logger.Error().Msg("recognition detail is nil")
		return false
	}
	if a.maxQuantity < 1 {
		a.logger.Error().
			Int("max_quantity", a.maxQuantity).
			Msg("invalid max quantity for precise click calculation")
		return false
	}

	endBox, ok := readHitBox(arg.RecognitionDetail)
	if !ok {
		a.logger.Error().Msg("failed to extract end box from recognition detail")
		return false
	}
	a.endBox = endBox

	if len(a.startBox) < 4 {
		a.logger.Error().
			Ints("start_box", a.startBox).
			Msg("start box is invalid")
		return false
	}
	if len(a.endBox) < 4 {
		a.logger.Error().
			Ints("end_box", a.endBox).
			Msg("end box is invalid")
		return false
	}

	startX, startY := centerPoint(a.startBox, a.CenterPointOffset)
	endX, endY := centerPoint(a.endBox, a.CenterPointOffset)

	numerator := a.Target - 1
	denominator := a.maxQuantity - 1
	if denominator == 0 {
		a.logger.Error().
			Int("max_quantity", a.maxQuantity).
			Msg("denominator is zero in precise click calculation")
		return false
	}

	clickX := startX + (endX-startX)*numerator/denominator
	clickY := startY + (endY-startY)*numerator/denominator

	if err := ctx.OverridePipeline(map[string]any{
		nodeQuantizedSlidingPreciseClick: map[string]any{
			"action": map[string]any{
				"param": map[string]any{
					"target": []int{clickX, clickY},
				},
			},
		},
	}); err != nil {
		a.logger.Error().Err(err).Msg("failed to override precise click target")
		return false
	}

	a.logger.Info().
		Ints("start_box", a.startBox).
		Ints("end_box", a.endBox).
		Int("target", a.Target).
		Int("max_quantity", a.maxQuantity).
		Int("click_x", clickX).
		Int("click_y", clickY).
		Msg("precise click calculated")
	return true
}

func (a *QuantizedSlidingAction) handleCheckQuantity(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}

	if arg == nil {
		a.logger.Error().Msg("custom action arg is nil")
		return false
	}

	currentQuantity, err := readQuantityValue(arg.RecognitionDetail)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to parse current quantity from ocr")
		return false
	}

	switch {
	case currentQuantity == a.Target:
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeQuantizedSlidingDone, buttonTarget{}, 0, a.GreenMask); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target)
			if errors.Is(err, errCheckQuantityBranchNextOverride) {
				logEvent.Msg("failed to override next to done")
			} else {
				logEvent.Msg("failed to override done node")
			}
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Str("next", nodeQuantizedSlidingDone).
			Msg("quantity matched target")
		return true
	case currentQuantity < a.Target:
		diff := a.Target - currentQuantity
		repeat := clampClickRepeat(diff)
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeQuantizedSlidingIncreaseQuantity, a.IncreaseButton, repeat, a.GreenMask); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target).
				Int("diff", diff).
				Int("repeat", repeat).
				Interface("increase_button", a.IncreaseButton.logValue())
			if errors.Is(err, errCheckQuantityBranchNextOverride) {
				logEvent.Msg("failed to override next to increase quantity")
			} else {
				logEvent.Msg("failed to override increase quantity node")
			}
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Int("diff", diff).
			Int("repeat", repeat).
			Interface("button", a.IncreaseButton.logValue()).
			Str("next", nodeQuantizedSlidingIncreaseQuantity).
			Msg("quantity below target, branch to increase")
		return true
	default:
		diff := currentQuantity - a.Target
		repeat := clampClickRepeat(diff)
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeQuantizedSlidingDecreaseQuantity, a.DecreaseButton, repeat, a.GreenMask); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target).
				Int("diff", diff).
				Int("repeat", repeat).
				Interface("decrease_button", a.DecreaseButton.logValue())
			if errors.Is(err, errCheckQuantityBranchNextOverride) {
				logEvent.Msg("failed to override next to decrease quantity")
			} else {
				logEvent.Msg("failed to override decrease quantity node")
			}
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Int("diff", diff).
			Int("repeat", repeat).
			Interface("button", a.DecreaseButton.logValue()).
			Str("next", nodeQuantizedSlidingDecreaseQuantity).
			Msg("quantity above target, branch to decrease")
		return true
	}
}

func (a *QuantizedSlidingAction) handleDone(_ *maa.Context, _ *maa.CustomActionArg) bool {
	a.logger.Info().
		Int("target", a.Target).
		Msg("quantity adjustment completed")
	return true
}

func (a *QuantizedSlidingAction) runInternalPipeline(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}

	override, err := buildInternalPipelineOverride(arg.CustomActionParam)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("caller", arg.CurrentTaskName).
			Msg("failed to build internal quantized sliding pipeline override")
		return false
	}

	detail, err := ctx.RunTask(nodeQuantizedSlidingMain, override)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("caller", arg.CurrentTaskName).
			Msg("failed to run internal quantized sliding pipeline")
		return false
	}
	if detail == nil {
		a.logger.Error().
			Str("caller", arg.CurrentTaskName).
			Msg("internal quantized sliding pipeline returned nil detail")
		return false
	}
	if !detail.Status.Success() {
		a.logger.Error().
			Str("caller", arg.CurrentTaskName).
			Int64("subtask_id", detail.ID).
			Str("subtask_status", detail.Status.String()).
			Msg("internal quantized sliding pipeline failed")
		return false
	}

	a.logger.Info().
		Str("caller", arg.CurrentTaskName).
		Int64("subtask_id", detail.ID).
		Str("subtask_status", detail.Status.String()).
		Msg("internal quantized sliding pipeline completed")
	return true
}

func isQuantizedSlidingActionNode(taskName string) bool {
	for _, nodeName := range quantizedSlidingActionNodes {
		if taskName == nodeName {
			return true
		}
	}

	return false
}

func (a *QuantizedSlidingAction) resetState() {
	a.startBox = nil
	a.endBox = nil
	a.maxQuantity = 0
}

func resolveMaxQuantityNext(maxQuantity int, target int) (string, error) {
	if maxQuantity < target {
		return "", fmt.Errorf("max quantity %d lower than target %d", maxQuantity, target)
	}
	if maxQuantity == 1 && target == 1 {
		return nodeQuantizedSlidingDone, nil
	}

	return "", nil
}
