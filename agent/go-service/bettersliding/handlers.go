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
	case nodeBetterSlidingGetSliderMaxQuantity:
		return a.handleGetSliderMaxQuantity(ctx, arg)
	case nodeBetterSlidingGetAvailableQuantity:
		return a.handleGetAvailableQuantity(ctx, arg)
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

	if !a.SwipeOnlyMode && len(a.SliderQuantityBox) != 4 {
		a.logger.Error().
			Ints("slider_quantity_box", a.SliderQuantityBox).
			Msg("invalid slider quantity box, expected [x,y,w,h]")
		return false
	}
	if a.AvailableQuantityExplicit && len(a.AvailableQuantityBox) != 4 {
		a.logger.Error().
			Ints("available_quantity_box", a.AvailableQuantityBox).
			Msg("invalid available quantity box, expected [x,y,w,h]")
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
		a.SliderQuantityBox,
		a.AvailableQuantityBox,
		a.AvailableQuantityExplicit,
		a.SliderQuantityFilter,
		a.AvailableQuantityFilter,
		a.SliderQuantityOnlyRec,
		a.AvailableQuantityOnlyRec,
		a.SwipeButton,
		a.GreenMask,
	)

	resetOverride, err := buildResetSwipeOverride(a.Direction, a.ResetBeforeFindStart)
	if err != nil {
		a.logger.Error().
			Str("direction", a.Direction).
			Err(err).
			Msg("failed to build reset swipe override")
		return false
	}
	for nodeName, nodeOverride := range resetOverride {
		override[nodeName] = nodeOverride
	}

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
		Ints("slider_quantity_roi", a.SliderQuantityBox).
		Ints("available_quantity_roi", a.AvailableQuantityBox).
		Bool("available_quantity_explicit", a.AvailableQuantityExplicit).
		Bool("green_mask", a.GreenMask).
		Bool("slider_quantity_filter_enabled", a.SliderQuantityFilter != nil).
		Bool("available_quantity_filter_enabled", a.AvailableQuantityFilter != nil).
		Bool("slider_quantity_only_rec", a.SliderQuantityOnlyRec).
		Bool("available_quantity_only_rec", a.AvailableQuantityOnlyRec).
		Bool("reset_before_find_start", a.ResetBeforeFindStart).
		Bool("swipe_only_mode", a.SwipeOnlyMode)

	if a.SliderQuantityFilter != nil {
		initializationLog = initializationLog.
			Int("slider_quantity_filter_method", a.SliderQuantityFilter.Method).
			Ints("slider_quantity_filter_lower", a.SliderQuantityFilter.Lower).
			Ints("slider_quantity_filter_upper", a.SliderQuantityFilter.Upper)
	}

	if a.AvailableQuantityFilter != nil {
		initializationLog = initializationLog.
			Int("available_quantity_filter_method", a.AvailableQuantityFilter.Method).
			Ints("available_quantity_filter_lower", a.AvailableQuantityFilter.Lower).
			Ints("available_quantity_filter_upper", a.AvailableQuantityFilter.Upper)
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

func (a *BetterSlidingAction) handleGetSliderMaxQuantity(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}
	if arg == nil {
		a.logger.Error().Msg("custom action arg is nil")
		return false
	}

	sliderMaxQuantity, err := readQuantityValue(arg.RecognitionDetail)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to parse slider max quantity from ocr")
		return false
	}

	a.sliderMaxQuantity = sliderMaxQuantity

	if !a.availableQuantityResolved {
		resolved, resolveErr := resolveTargetQuantity(
			a.OriginalTargetQuantity,
			a.TargetQuantityType,
			a.ReverseTarget,
			a.sliderMaxQuantity,
		)
		if resolveErr != nil {
			a.logger.Error().
				Err(resolveErr).
				Int("target_quantity", a.OriginalTargetQuantity).
				Str("target_quantity_type", a.TargetQuantityType).
				Bool("reverse_target", a.ReverseTarget).
				Msg("failed to resolve target quantity")
			return false
		}

		if resolved != a.OriginalTargetQuantity {
			a.logger.Info().
				Int("original_target_quantity", a.OriginalTargetQuantity).
				Int("resolved_target_quantity", resolved).
				Str("target_quantity_type", a.TargetQuantityType).
				Bool("reverse_target", a.ReverseTarget).
				Int("slider_max_quantity", a.sliderMaxQuantity).
				Msg("target quantity resolved")
		}
		a.TargetQuantity = resolved
		a.runtimeTargetResolved = true
	}

	originalResolvedTargetQuantity := a.TargetQuantity
	resolvedTargetQuantity, outcome := resolveSliderQuantityOutcome(
		a.TargetQuantity,
		a.sliderMaxQuantity,
		a.ClampTargetToSliderMax,
	)
	a.TargetQuantity = resolvedTargetQuantity
	a.outOfRange = outcome == sliderQuantityOutcomeOutOfRange
	a.targetReachable = outcome == sliderQuantityOutcomeTargetReachable

	if outcome == sliderQuantityOutcomeClamped {
		a.logger.Warn().
			Int("original_target_quantity", originalResolvedTargetQuantity).
			Int("clamped_target_quantity", a.TargetQuantity).
			Int("slider_max_quantity", a.sliderMaxQuantity).
			Msg("target quantity clamped to slider max quantity")
	}

	if a.outOfRange {
		if a.OutOfRangeOverrideEnable == "" {
			a.logger.Error().
				Str("outcome", "out-of-range").
				Int("resolved_target_quantity", a.TargetQuantity).
				Int("slider_max_quantity", a.sliderMaxQuantity).
				Msg("quantity outcome has no override configured")
			return false
		}

		if err := overrideCheckQuantityBranch(
			ctx,
			arg.CurrentTaskName,
			nodeBetterSlidingDone,
			buttonTarget{},
			0,
			a.GreenMask,
		); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Str("outcome", "out-of-range").
				Int("slider_max_quantity", a.sliderMaxQuantity).
				Int("target_quantity", a.TargetQuantity).
				Str("next", nodeBetterSlidingDone)
			if errors.Is(err, errCheckQuantityBranchNextOverride) {
				logEvent.Msg("failed to override next for quantity outcome branch")
			} else {
				logEvent.Msg("failed to override pipeline for quantity outcome branch")
			}
			return false
		}

		a.logger.Warn().
			Str("outcome", "out-of-range").
			Int("original_target_quantity", a.OriginalTargetQuantity).
			Int("resolved_target_quantity", a.TargetQuantity).
			Int("slider_max_quantity", a.sliderMaxQuantity).
			Str("override_node", a.OutOfRangeOverrideEnable).
			Msg("quantity adjustment skipped; caller outcome scheduled")
		return true
	}

	if a.OutOfRangeOverrideEnable != "" {
		if err := ctx.OverridePipeline(buildNodeEnableOverride(a.OutOfRangeOverrideEnable, false)); err != nil {
			a.logger.Error().Err(err).
				Str("override_node", a.OutOfRangeOverrideEnable).
				Msg("failed to disable quantity outcome override")
			return false
		}
	}

	nextNode, err := resolveSliderMaxQuantityNext(a.sliderMaxQuantity, a.TargetQuantity)
	if err != nil {
		a.logger.Error().
			Int("slider_max_quantity", a.sliderMaxQuantity).
			Int("target_quantity", a.TargetQuantity).
			Msg("slider max quantity lower than target quantity")
		return false
	}
	if nextNode != "" {
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nextNode, buttonTarget{}, 0, a.GreenMask); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Int("slider_max_quantity", a.sliderMaxQuantity).
				Int("target_quantity", a.TargetQuantity).
				Str("next", nextNode)
			if errors.Is(err, errCheckQuantityBranchNextOverride) {
				logEvent.Msg("failed to override next for direct-done branch")
			} else {
				logEvent.Msg("failed to override direct-done branch")
			}
			return false
		}

		a.logger.Info().
			Int("slider_max_quantity", a.sliderMaxQuantity).
			Int("target_quantity", a.TargetQuantity).
			Str("next", nextNode).
			Msg("slider max quantity already matches target quantity, branch to done")
		return true
	}

	a.logger.Info().
		Int("slider_max_quantity", a.sliderMaxQuantity).
		Int("target_quantity", a.TargetQuantity).
		Msg("slider max quantity parsed")
	return true
}

func (a *BetterSlidingAction) handleGetAvailableQuantity(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}
	if arg == nil {
		a.logger.Error().Msg("custom action arg is nil")
		return false
	}

	availableQuantity, err := readQuantityValue(arg.RecognitionDetail)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to parse available quantity from ocr")
		return false
	}

	a.availableQuantity = availableQuantity

	resolved, resolveErr := resolveTargetQuantity(
		a.OriginalTargetQuantity,
		a.TargetQuantityType,
		a.ReverseTarget,
		a.availableQuantity,
	)
	if resolveErr != nil {
		a.logger.Error().
			Err(resolveErr).
			Int("target_quantity", a.OriginalTargetQuantity).
			Str("target_quantity_type", a.TargetQuantityType).
			Bool("reverse_target", a.ReverseTarget).
			Msg("failed to resolve target quantity from available quantity")
		return false
	}

	if resolved != a.OriginalTargetQuantity {
		a.logger.Info().
			Int("original_target_quantity", a.OriginalTargetQuantity).
			Int("resolved_target_quantity", resolved).
			Str("target_quantity_type", a.TargetQuantityType).
			Bool("reverse_target", a.ReverseTarget).
			Int("available_quantity", a.availableQuantity).
			Msg("target quantity resolved from available quantity")
	}
	a.TargetQuantity = resolved
	a.runtimeTargetResolved = true
	a.availableQuantityResolved = true

	a.logger.Info().
		Int("available_quantity", a.availableQuantity).
		Int("resolved_target_quantity", a.TargetQuantity).
		Msg("available quantity parsed")
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
	if a.sliderMaxQuantity < 1 {
		a.logger.Error().
			Int("slider_max_quantity", a.sliderMaxQuantity).
			Msg("invalid slider max quantity for precise click calculation")
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

	numerator := a.TargetQuantity - 1
	denominator := a.sliderMaxQuantity - 1
	if denominator == 0 {
		a.logger.Error().
			Int("slider_max_quantity", a.sliderMaxQuantity).
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
		Int("target_quantity", a.TargetQuantity).
		Int("slider_max_quantity", a.sliderMaxQuantity).
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
	case currentQuantity == a.TargetQuantity:
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeBetterSlidingDone, buttonTarget{}, 0, a.GreenMask); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target_quantity", a.TargetQuantity)
			if errors.Is(err, errCheckQuantityBranchNextOverride) {
				logEvent.Msg("failed to override next to done")
			} else {
				logEvent.Msg("failed to override done node")
			}
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target_quantity", a.TargetQuantity).
			Str("next", nodeBetterSlidingDone).
			Msg("quantity matched target")
		return true
	case currentQuantity < a.TargetQuantity:
		diff := a.TargetQuantity - currentQuantity
		repeat := clampClickRepeat(diff)
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeBetterSlidingIncreaseQuantity, a.IncreaseButton, repeat, a.GreenMask); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target_quantity", a.TargetQuantity).
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
			Int("target_quantity", a.TargetQuantity).
			Int("diff", diff).
			Int("repeat", repeat).
			Interface("button", a.IncreaseButton.logValue()).
			Str("next", nodeBetterSlidingIncreaseQuantity).
			Msg("quantity below target, branch to increase")
		return true
	default:
		diff := currentQuantity - a.TargetQuantity
		repeat := clampClickRepeat(diff)
		if err := overrideCheckQuantityBranch(ctx, arg.CurrentTaskName, nodeBetterSlidingDecreaseQuantity, a.DecreaseButton, repeat, a.GreenMask); err != nil {
			logEvent := a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target_quantity", a.TargetQuantity).
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
			Int("target_quantity", a.TargetQuantity).
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
		Int("target_quantity", a.TargetQuantity).
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

	if !a.applyOutcomeOverrides(ctx, arg.CurrentTaskName) {
		return false
	}

	if a.SwipeOnlyMode {
		a.logger.Info().
			Str("caller", arg.CurrentTaskName).
			Int64("subtask_id", detail.ID).
			Str("subtask_status", detail.Status.String()).
			Bool("swipe_only_mode", true).
			Msg("internal BetterSliding pipeline finished (swipe-only)")
		return true
	}

	a.logger.Info().
		Str("caller", arg.CurrentTaskName).
		Int64("subtask_id", detail.ID).
		Str("subtask_status", detail.Status.String()).
		Msg("internal BetterSliding pipeline completed")
	return true
}

// applyOutcomeOverrides 在内部流水线结束后，将调用方结果节点的开关统一同步为本次判定结果，
// 保证命中的结果节点被启用、未命中的被禁用。新增结果时只需在此追加一项。
func (a *BetterSlidingAction) applyOutcomeOverrides(ctx *maa.Context, caller string) bool {
	outcomes := []struct {
		name    string
		node    string
		enabled bool
	}{
		{"out-of-range", a.OutOfRangeOverrideEnable, a.outOfRange},
		{"target-reachable", a.TargetReachableOverrideEnable, a.targetReachable},
	}

	for _, outcome := range outcomes {
		if outcome.node == "" {
			continue
		}
		if err := ctx.OverridePipeline(buildNodeEnableOverride(outcome.node, outcome.enabled)); err != nil {
			a.logger.Error().
				Err(err).
				Str("caller", caller).
				Str("outcome", outcome.name).
				Str("override_node", outcome.node).
				Bool("enabled", outcome.enabled).
				Msg("failed to apply outcome override after internal pipeline")
			return false
		}
		if outcome.enabled {
			a.logger.Info().
				Str("caller", caller).
				Str("outcome", outcome.name).
				Str("override_node", outcome.node).
				Msg("applied outcome override after internal pipeline")
		}
	}

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
	a.sliderMaxQuantity = 0
	a.availableQuantity = 0
	a.availableQuantityResolved = false
	a.outOfRange = false
	a.targetReachable = false
	a.runtimeTargetResolved = false
}

// sliderQuantityOutcome 描述目标数量相对滑条最大数量的判定结果。
type sliderQuantityOutcome uint8

const (
	// sliderQuantityOutcomeOutOfRange 表示目标小于 1、滑条最大数量为 0，
	// 或在未启用钳制时超过滑条最大数量。
	sliderQuantityOutcomeOutOfRange sliderQuantityOutcome = iota
	// sliderQuantityOutcomeTargetReachable 表示目标位于滑条可选范围内，无需钳制即可达到。
	sliderQuantityOutcomeTargetReachable
	// sliderQuantityOutcomeClamped 表示目标超过滑条最大数量，已钳制到该最大数量。
	sliderQuantityOutcomeClamped
)

// resolveSliderQuantityOutcome 根据解析后的目标数量与滑条最大可选数量，
// 返回本次实际使用的目标数量及其判定结果。
//
// 判定按以下优先级依次短路，三种结果互斥：
//
//  1. targetQuantity < 1 或 sliderMaxQuantity == 0 → OutOfRange：没有可选的有效目标。
//  2. targetQuantity > sliderMaxQuantity：启用 clampTargetToSliderMax 时将目标下调到
//     滑条上限并返回 Clamped（目标数量被替换为 sliderMaxQuantity，属部分达成，
//     因此不算 TargetReachable）；未启用钳制时返回 OutOfRange，由调用方决定如何处理。
//  3. 其余情况 → TargetReachable：目标在 [1, sliderMaxQuantity] 内，无需钳制即可达成。
//
// 除 Clamped 外，返回的目标数量均为入参原值。判定结果最终通过
// applyOutcomeOverrides 映射到调用方的结果节点开关（Clamped 不启用任何结果节点）。
func resolveSliderQuantityOutcome(
	targetQuantity int,
	sliderMaxQuantity int,
	clampTargetToSliderMax bool,
) (int, sliderQuantityOutcome) {
	if targetQuantity < 1 || sliderMaxQuantity == 0 {
		return targetQuantity, sliderQuantityOutcomeOutOfRange
	}
	if targetQuantity > sliderMaxQuantity {
		if clampTargetToSliderMax {
			return sliderMaxQuantity, sliderQuantityOutcomeClamped
		}
		return targetQuantity, sliderQuantityOutcomeOutOfRange
	}

	return targetQuantity, sliderQuantityOutcomeTargetReachable
}

func resolveSliderMaxQuantityNext(sliderMaxQuantity int, targetQuantity int) (string, error) {
	if sliderMaxQuantity == targetQuantity {
		return nodeBetterSlidingDone, nil
	}
	if sliderMaxQuantity < targetQuantity {
		return "", fmt.Errorf(
			"slider max quantity %d lower than target quantity %d",
			sliderMaxQuantity,
			targetQuantity,
		)
	}
	if sliderMaxQuantity == 1 && targetQuantity == 1 {
		return nodeBetterSlidingDone, nil
	}

	return "", nil
}
