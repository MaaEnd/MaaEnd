package bettersliding

import (
	"errors"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func (a *BetterSlidingAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", betterSlidingActionName).
			Msg("got nil custom action arg")
		return false
	}

	a.initLogger(arg.CurrentTaskName)

	if !isBetterSlidingActionNode(arg.CurrentTaskName) {
		return a.runInternalPipeline(ctx, arg)
	}

	if !a.loadActionParams(arg.CustomActionParam) {
		return false
	}

	return a.dispatchActionNode(ctx, arg)
}

func (a *BetterSlidingAction) dispatchActionNode(ctx *maa.Context, arg *maa.CustomActionArg) bool {

	switch arg.CurrentTaskName {
	case nodeBetterSlidingMain:
		return a.handleMain(ctx, arg)
	case nodeBetterSlidingFindStart:
		return a.handleFindStart(ctx, arg)
	case nodeBetterSlidingGetMaxQuantity:
		return a.handleGetMaxQuantity(ctx, arg)
	case nodeBetterSlidingFindEnd:
		return a.handleFindEnd(ctx, arg)
	case nodeBetterSlidingCheckQuantity:
		return a.handleCheckQuantity(ctx, arg)
	case nodeBetterSlidingDone:
		return a.handleDone(ctx, arg)
	default:
		a.logger.Warn().Msg("unknown current task name")
		return false
	}
}

func (a *BetterSlidingAction) handleMain(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	a.resetState()

	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}

	if !a.SwipeOnlyMode && len(a.QuantityBox) != 4 {
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

	override := buildMainInitializationOverride(
		end,
		a.QuantityBox,
		a.QuantityFilter,
		a.QuantityOnlyRec,
		a.SwipeButton,
		a.GreenMask,
	)

	if err := ctx.OverridePipeline(override); err != nil {
		a.logger.Error().Err(err).Msg("failed to override pipeline for main initialization")
		return false
	}

	// Swipe-only mode: clear next items for SwipeToMax so it runs one-shot.
	if a.SwipeOnlyMode {
		if err := ctx.OverrideNext(nodeBetterSlidingSwipeToMax, []maa.NextItem{}); err != nil {
			a.logger.Error().Err(err).Msg("failed to clear swipe-to-max next items for swipe-only mode")
			return false
		}
	}

	initializationLog := a.logger.Info().
		Str("direction", a.Direction).
		Ints("end", end).
		Ints("quantity_roi", a.QuantityBox).
		Bool("green_mask", a.GreenMask).
		Bool("quantity_filter_enabled", a.QuantityFilter != nil).
		Bool("swipe_only_mode", a.SwipeOnlyMode)

	if a.QuantityFilter != nil {
		initializationLog = initializationLog.
			Int("quantity_filter_method", a.QuantityFilter.Method).
			Ints("quantity_filter_lower", a.QuantityFilter.Lower).
			Ints("quantity_filter_upper", a.QuantityFilter.Upper)
	}

	initializationLog.Msg("main initialization completed with pipeline overrides")
	return true
}

func (a *BetterSlidingAction) handleFindStart(_ *maa.Context, arg *maa.CustomActionArg) bool {
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

func (a *BetterSlidingAction) handleGetMaxQuantity(ctx *maa.Context, arg *maa.CustomActionArg) bool {
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

	resolved, resolveErr := resolveTarget(a.OriginalTarget, a.TargetType, a.TargetReverse, a.maxQuantity)
	if resolveErr != nil {
		a.logger.Error().
			Err(resolveErr).
			Int("target", a.OriginalTarget).
			Str("target_type", a.TargetType).
			Bool("target_reverse", a.TargetReverse).
			Msg("failed to resolve target")
		return false
	}

	if resolved != a.OriginalTarget {
		a.logger.Info().
			Int("original_target", a.OriginalTarget).
			Int("resolved_target", resolved).
			Str("target_type", a.TargetType).
			Bool("target_reverse", a.TargetReverse).
			Int("max_quantity", a.maxQuantity).
			Msg("target resolved")
	}
	a.Target = resolved
	a.runtimeTargetResolved = true

	a.exceeded = false
	outOfRange := a.Target > a.maxQuantity
	if a.TargetType == TargetTypeValue && a.TargetReverse && a.Target < 1 {
		outOfRange = true
	}

	if a.ExceedingOverrideEnable != "" {
		if outOfRange {
			a.exceeded = true
			if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeBetterSlidingDone, buttonTarget{}, 0, a.GreenMask); err != nil {
				logEvent := a.logger.Error().
					Err(err).
					Int("max_quantity", a.maxQuantity).
					Int("target", a.Target).
					Str("next", nodeBetterSlidingDone)
				if errors.Is(err, errCheckQuantityBranchNextOverride) {
					logEvent.Msg("failed to override next for exceeding branch")
				} else {
					logEvent.Msg("failed to override pipeline for exceeding branch")
				}

				return false
			}

			a.logger.Warn().
				Int("original_target", a.OriginalTarget).
				Int("resolved_target", a.Target).
				Int("max_quantity", a.maxQuantity).
				Str("override_node", a.ExceedingOverrideEnable).
				Msg("target out of range: exceeding override scheduled, branching to done")
			return true
		}

		if err := ctx.OverridePipeline(buildExceedingOverrideEnable(a.ExceedingOverrideEnable, false)); err != nil {
			a.logger.Error().Err(err).
				Str("override_node", a.ExceedingOverrideEnable).
				Msg("failed to override exceeding disable state")
			return false
		}
	} else if a.Target < 1 || (a.Target > a.maxQuantity && !a.ClampTargetToMax) {
		a.logger.Error().
			Int("resolved_target", a.Target).
			Int("max_quantity", a.maxQuantity).
			Msg("target out of range and no exceeding override configured")
		return false
	}

	// Clamp Target before calculating nextNode to avoid division-by-zero when maxQuantity==1.
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

func (a *BetterSlidingAction) handleFindEnd(ctx *maa.Context, arg *maa.CustomActionArg) bool {
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
		nodeBetterSlidingPreciseClick: map[string]any{
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

	if a.FinishAfterPreciseClick {
		if err := ctx.OverrideNext(nodeBetterSlidingPreciseClick, []maa.NextItem{}); err != nil {
			a.logger.Error().Err(err).Msg("failed to clear precise click next for finish-after-precise-click")
			return false
		}

		a.logger.Info().Msg("finish-after-precise-click enabled, skipping quantity check")
	} else {
		if err := ctx.OverrideNext(nodeBetterSlidingPreciseClick, []maa.NextItem{{Name: nodeBetterSlidingJumpBackNode}}); err != nil {
			a.logger.Error().Err(err).Msg("failed to restore precise click next")
			return false
		}
	}

	return true
}

func (a *BetterSlidingAction) handleCheckQuantity(ctx *maa.Context, arg *maa.CustomActionArg) bool {
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
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeBetterSlidingDone, buttonTarget{}, 0, a.GreenMask); err != nil {
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
			Str("next", nodeBetterSlidingDone).
			Msg("quantity matched target")
		return true
	case currentQuantity < a.Target:
		diff := a.Target - currentQuantity
		repeat := clampClickRepeat(diff)
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeBetterSlidingIncreaseQuantity, a.IncreaseButton, repeat, a.GreenMask); err != nil {
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
			Str("next", nodeBetterSlidingIncreaseQuantity).
			Msg("quantity below target, branch to increase")
		return true
	default:
		diff := currentQuantity - a.Target
		repeat := clampClickRepeat(diff)
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeBetterSlidingDecreaseQuantity, a.DecreaseButton, repeat, a.GreenMask); err != nil {
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
			Str("next", nodeBetterSlidingDecreaseQuantity).
			Msg("quantity above target, branch to decrease")
		return true
	}
}

func (a *BetterSlidingAction) handleDone(_ *maa.Context, _ *maa.CustomActionArg) bool {
	a.logger.Info().
		Int("target", a.Target).
		Msg("quantity adjustment completed")
	return true
}

func (a *BetterSlidingAction) runInternalPipeline(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}

	merged := mergeAttachParams(ctx, arg.CurrentTaskName, arg.CustomActionParam)

	raw, err := parseBetterSlidingParam(merged)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("caller", arg.CurrentTaskName).
			Msg("failed to parse merged custom_action_param")
		return false
	}

	parsed, ok := a.normalizeActionParams(raw)
	if !ok {
		return false
	}

	a.applyActionParams(parsed)

	override, err := buildInternalPipelineOverride(merged)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("caller", arg.CurrentTaskName).
			Msg("failed to build internal BetterSliding pipeline override")
		return false
	}

	detail, err := ctx.RunTask(nodeBetterSlidingMain, override)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("caller", arg.CurrentTaskName).
			Msg("failed to run internal BetterSliding pipeline")
		return false
	}
	if detail == nil {
		a.logger.Error().
			Str("caller", arg.CurrentTaskName).
			Msg("internal BetterSliding pipeline returned nil detail")
		return false
	}

	if !detail.Status.Success() {
		a.logger.Error().
			Str("caller", arg.CurrentTaskName).
			Int64("subtask_id", detail.ID).
			Str("subtask_status", detail.Status.String()).
			Msg("internal BetterSliding pipeline failed")
		return false
	}

	if a.exceeded && a.ExceedingOverrideEnable != "" {
		if err := ctx.OverridePipeline(buildExceedingOverrideEnable(a.ExceedingOverrideEnable, true)); err != nil {
			a.logger.Error().
				Err(err).
				Str("caller", arg.CurrentTaskName).
				Str("override_node", a.ExceedingOverrideEnable).
				Msg("failed to apply exceeding override after internal pipeline")
			return false
		}

		a.logger.Info().
			Str("caller", arg.CurrentTaskName).
			Str("override_node", a.ExceedingOverrideEnable).
			Msg("applied exceeding override after internal pipeline")
	}

	if a.SwipeOnlyMode {
		a.logger.Info().
			Str("caller", arg.CurrentTaskName).
			Int64("subtask_id", detail.ID).
			Str("subtask_status", detail.Status.String()).
			Bool("swipe_only_mode", true).
			Msg("internal BetterSliding pipeline finished (swipe-only)")

		if !a.exceeded && a.ExceedingOverrideEnable != "" {
			if err := ctx.OverridePipeline(buildExceedingOverrideEnable(a.ExceedingOverrideEnable, false)); err != nil {
				a.logger.Error().Err(err).Msg("failed to apply exceeding override after swipe-only")
				return false
			}
		}

		return true
	}

	if !a.exceeded && a.ExceedingOverrideEnable != "" {
		if err := ctx.OverridePipeline(buildExceedingOverrideEnable(a.ExceedingOverrideEnable, false)); err != nil {
			a.logger.Error().Err(err).Msg("failed to apply exceeding override after internal pipeline")
			return false
		}
	}

	a.logger.Info().
		Str("caller", arg.CurrentTaskName).
		Int64("subtask_id", detail.ID).
		Str("subtask_status", detail.Status.String()).
		Msg("internal BetterSliding pipeline completed")
	return true
}

func isBetterSlidingActionNode(taskName string) bool {
	for _, nodeName := range betterSlidingActionNodes {
		if taskName == nodeName {
			return true
		}
	}

	return false
}

func (a *BetterSlidingAction) resetState() {
	a.startBox = nil
	a.endBox = nil
	a.maxQuantity = 0
	a.exceeded = false
	a.runtimeTargetResolved = false
}

func resolveMaxQuantityNext(maxQuantity int, target int) (string, error) {
	if maxQuantity < target {
		return "", fmt.Errorf("max quantity %d lower than target %d", maxQuantity, target)
	}
	if maxQuantity == 1 && target == 1 {
		return nodeBetterSlidingDone, nil
	}

	return "", nil
}
