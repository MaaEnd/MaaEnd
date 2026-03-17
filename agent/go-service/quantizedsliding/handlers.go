package quantizedsliding

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func (a *QuantizedSlidingAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", "QuantizedSliding").
			Msg("got nil custom action arg")
		return false
	}

	a.logger = log.With().
		Str("component", "QuantizedSliding").
		Str("task", arg.CurrentTaskName).
		Logger()

	if !isQuantizedSlidingActionNode(arg.CurrentTaskName) {
		return a.runInternalPipeline(ctx, arg)
	}

	var params quantizedSlidingParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		a.logger.Error().
			Err(err).
			Str("param", arg.CustomActionParam).
			Msg("failed to parse custom_action_param")
		return false
	}

	if params.Target <= 0 {
		a.logger.Error().
			Int("target", params.Target).
			Msg("invalid target, must be greater than 0")
		return false
	}

	increaseButton, err := normalizeButtonParam(params.IncreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize increase button")
		return false
	}

	decreaseButton, err := normalizeButtonParam(params.DecreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize decrease button")
		return false
	}

	centerPointOffset, err := normalizeCenterPointOffset(params.CenterPointOffset)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize center point offset")
		return false
	}

	quantityFilter, err := normalizeQuantityFilter(params.QuantityFilter)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize quantity filter")
		return false
	}

	a.Target = params.Target
	a.QuantityBox = append([]int(nil), params.QuantityBox...)
	a.QuantityFilter = quantityFilter
	a.Direction = strings.ToLower(strings.TrimSpace(params.Direction))
	a.IncreaseButton = increaseButton
	a.DecreaseButton = decreaseButton
	a.CenterPointOffset = centerPointOffset

	parseLog := a.logger.Info().
		Int("target", a.Target).
		Ints("quantity_box", a.QuantityBox).
		Str("direction", a.Direction).
		Interface("increase_button", a.IncreaseButton.logValue()).
		Interface("decrease_button", a.DecreaseButton.logValue()).
		Bool("quantity_filter_enabled", a.QuantityFilter != nil).
		Ints("center_point_offset", []int{a.CenterPointOffset[0], a.CenterPointOffset[1]})

	if a.QuantityFilter != nil {
		parseLog = parseLog.
			Int("quantity_filter_method", a.QuantityFilter.Method).
			Ints("quantity_filter_lower", a.QuantityFilter.Lower).
			Ints("quantity_filter_upper", a.QuantityFilter.Upper)
	}

	parseLog.Msg("parsed custom action parameters")

	switch arg.CurrentTaskName {
	case "QuantizedSlidingMain":
		return a.handleMain(ctx, arg)
	case "QuantizedSlidingFindStart":
		return a.handleFindStart(ctx, arg)
	case "QuantizedSlidingGetMaxQuantity":
		return a.handleGetMaxQuantity(ctx, arg)
	case "QuantizedSlidingFindEnd":
		return a.handleFindEnd(ctx, arg)
	case "QuantizedSlidingCheckQuantity":
		return a.handleCheckQuantity(ctx, arg)
	case "QuantizedSlidingDone":
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

	override := buildMainInitializationOverride(end, a.QuantityBox, a.QuantityFilter)

	if err := ctx.OverridePipeline(override); err != nil {
		a.logger.Error().Err(err).Msg("failed to override pipeline for main initialization")
		return false
	}

	initializationLog := a.logger.Info().
		Str("direction", a.Direction).
		Ints("end", end).
		Ints("quantity_roi", a.QuantityBox).
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

	box, ok := extractHitBox(arg.RecognitionDetail)
	if !ok {
		a.logger.Error().Msg("failed to extract start box from recognition detail")
		return false
	}

	a.startBox = box
	a.logger.Info().Ints("start_box", a.startBox).Msg("start box recorded")
	return true
}

func (a *QuantizedSlidingAction) handleGetMaxQuantity(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		a.logger.Error().Msg("custom action arg is nil")
		return false
	}

	maxQuantity, err := parseOCRText(arg.RecognitionDetail)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to parse max quantity from ocr")
		return false
	}

	a.maxQuantity = maxQuantity
	if a.maxQuantity < a.Target {
		a.logger.Error().
			Int("max_quantity", a.maxQuantity).
			Int("target", a.Target).
			Msg("max quantity lower than target")
		return false
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
	if a.maxQuantity <= 1 {
		a.logger.Error().
			Int("max_quantity", a.maxQuantity).
			Msg("invalid max quantity for precise click calculation")
		return false
	}

	endBox, ok := extractHitBox(arg.RecognitionDetail)
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
		"QuantizedSlidingPreciseClick": map[string]any{
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

	currentQuantity, err := parseOCRText(arg.RecognitionDetail)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to parse current quantity from ocr")
		return false
	}

	switch {
	case currentQuantity == a.Target:
		if err := ctx.OverridePipeline(buildCheckQuantityBranchOverride("QuantizedSlidingDone", buttonTarget{}, 0)); err != nil {
			a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target).
				Msg("failed to override done node")
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Str("next", "QuantizedSlidingDone").
			Msg("quantity matched target")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "QuantizedSlidingDone"}}); err != nil {
			a.logger.Error().Err(err).Msg("failed to override next to done")
			return false
		}
		return true
	case currentQuantity < a.Target:
		diff := a.Target - currentQuantity
		repeat := clampClickRepeat(diff)
		if err := ctx.OverridePipeline(buildCheckQuantityBranchOverride("QuantizedSlidingIncreaseQuantity", a.IncreaseButton, repeat)); err != nil {
			a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target).
				Int("diff", diff).
				Int("repeat", repeat).
				Interface("increase_button", a.IncreaseButton.logValue()).
				Msg("failed to override increase quantity node")
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Int("diff", diff).
			Int("repeat", repeat).
			Interface("button", a.IncreaseButton.logValue()).
			Str("next", "QuantizedSlidingIncreaseQuantity").
			Msg("quantity below target, branch to increase")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "QuantizedSlidingIncreaseQuantity"}}); err != nil {
			a.logger.Error().Err(err).Msg("failed to override next to increase quantity")
			return false
		}
		return true
	default:
		diff := currentQuantity - a.Target
		repeat := clampClickRepeat(diff)
		if err := ctx.OverridePipeline(buildCheckQuantityBranchOverride("QuantizedSlidingDecreaseQuantity", a.DecreaseButton, repeat)); err != nil {
			a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target).
				Int("diff", diff).
				Int("repeat", repeat).
				Interface("decrease_button", a.DecreaseButton.logValue()).
				Msg("failed to override decrease quantity node")
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Int("diff", diff).
			Int("repeat", repeat).
			Interface("button", a.DecreaseButton.logValue()).
			Str("next", "QuantizedSlidingDecreaseQuantity").
			Msg("quantity above target, branch to decrease")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "QuantizedSlidingDecreaseQuantity"}}); err != nil {
			a.logger.Error().Err(err).Msg("failed to override next to decrease quantity")
			return false
		}
		return true
	}
}

func (a *QuantizedSlidingAction) handleDone(_ *maa.Context, _ *maa.CustomActionArg) bool {
	a.logger.Info().Msg("quantity adjustment completed")
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

	detail, err := ctx.RunTask("QuantizedSlidingMain", override)
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
