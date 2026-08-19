package bettersliding

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type parsedBetterSlidingParams struct {
	targetQuantity                int
	sliderQuantityBox             []int
	availableQuantityBox          []int
	availableQuantityExplicit     bool
	sliderQuantityFilter          *quantityFilterParam
	availableQuantityFilter       *quantityFilterParam
	sliderQuantityOnlyRec         bool
	availableQuantityOnlyRec      bool
	direction                     string
	increaseButton                buttonTarget
	decreaseButton                buttonTarget
	centerPointOffset             [2]int
	clampTargetToSliderMax        bool
	swipeButton                   string
	outOfRangeOverrideEnable      string
	targetReachableOverrideEnable string
	targetQuantityType            string
	reverseTarget                 bool
	swipeOnlyMode                 bool
	finishAfterPreciseClick       bool
	resetBeforeFindStart          bool
}

func detectBetterSlidingParamPresence(rawParam string) (betterSlidingParamPresence, error) {
	var rawKeys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawParam), &rawKeys); err != nil {
		return betterSlidingParamPresence{}, err
	}

	_, sliderQuantityPresent := rawKeys["SliderQuantity"]

	return betterSlidingParamPresence{
		TargetQuantity:                hasNonNullRawKey(rawKeys, "TargetQuantity"),
		SliderQuantity:                sliderQuantityPresent,
		AvailableQuantity:             hasNonNullRawKey(rawKeys, "AvailableQuantity"),
		Direction:                     hasNonNullRawKey(rawKeys, "Direction"),
		IncreaseButton:                hasNonNullRawKey(rawKeys, "IncreaseButton"),
		DecreaseButton:                hasNonNullRawKey(rawKeys, "DecreaseButton"),
		SwipeButton:                   hasNonNullRawKey(rawKeys, "SwipeButton"),
		OutOfRangeOverrideEnable:      hasNonNullRawKey(rawKeys, "OutOfRangeOverrideEnable"),
		TargetReachableOverrideEnable: hasNonNullRawKey(rawKeys, "TargetReachableOverrideEnable"),
		TargetQuantityType:            hasNonNullRawKey(rawKeys, "TargetQuantityType"),
		ReverseTarget:                 hasNonNullRawKey(rawKeys, "ReverseTarget"),
		CenterPointOffset:             hasNonNullRawKey(rawKeys, "CenterPointOffset"),
		ClampTargetToSliderMax:        hasNonNullRawKey(rawKeys, "ClampTargetToSliderMax"),
		FinishAfterPreciseClick:       hasNonNullRawKey(rawKeys, "FinishAfterPreciseClick"),
		ResetBeforeFindStart:          hasNonNullRawKey(rawKeys, "ResetBeforeFindStart"),
	}, nil
}

func hasNonNullRawKey(rawKeys map[string]json.RawMessage, key string) bool {
	raw, ok := rawKeys[key]
	return ok && len(raw) > 0 && string(raw) != "null"
}

func (a *BetterSlidingAction) validateOutcomeOverrideNodes(nodes ...string) bool {
	seen := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node == "" {
			continue
		}
		if _, exists := seen[node]; exists {
			a.logger.Error().
				Str("node", node).
				Msg("BetterSliding outcome overrides must use different nodes")
			return false
		}
		seen[node] = struct{}{}
	}
	return true
}

func parseBetterSlidingParam(customActionParam string) (betterSlidingParam, error) {
	presence, err := detectBetterSlidingParamPresence(customActionParam)
	if err != nil {
		return betterSlidingParam{}, err
	}

	var params betterSlidingParam
	if err := json.Unmarshal([]byte(customActionParam), &params); err != nil {
		return betterSlidingParam{}, err
	}
	params.presence = presence

	return params, nil
}

func (a *BetterSlidingAction) loadActionParams(customActionParam string) bool {
	params, err := parseBetterSlidingParam(customActionParam)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("param", customActionParam).
			Msg("failed to parse custom_action_param")
		return false
	}

	parsed, ok := a.normalizeActionParams(params)
	if !ok {
		return false
	}

	a.applyActionParams(parsed)
	a.logParsedActionParams()
	return true
}

func (a *BetterSlidingAction) normalizeActionParams(params betterSlidingParam) (parsedBetterSlidingParams, bool) {
	swipeButton := strings.TrimSpace(params.SwipeButton)
	outOfRangeOverrideEnable := strings.TrimSpace(params.OutOfRangeOverrideEnable)
	targetReachableOverrideEnable := strings.TrimSpace(params.TargetReachableOverrideEnable)
	if !a.validateOutcomeOverrideNodes(
		outOfRangeOverrideEnable,
		targetReachableOverrideEnable,
	) {
		return parsedBetterSlidingParams{}, false
	}

	targetQuantityType, err := normalizeTargetQuantityType(params.TargetQuantityType)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("target_quantity_type", params.TargetQuantityType).
			Msg("invalid TargetQuantityType")
		return parsedBetterSlidingParams{}, false
	}

	if isSwipeOnlyMode(params) {
		direction := strings.ToLower(strings.TrimSpace(params.Direction))
		switch direction {
		case "left", "right", "up", "down":
		default:
			a.logger.Error().
				Str("direction", params.Direction).
				Msg("invalid direction for swipe-only mode")
			return parsedBetterSlidingParams{}, false
		}

		return parsedBetterSlidingParams{
			targetQuantity:                0,
			sliderQuantityBox:             nil,
			availableQuantityBox:          nil,
			availableQuantityExplicit:     false,
			sliderQuantityFilter:          nil,
			availableQuantityFilter:       nil,
			sliderQuantityOnlyRec:         false,
			availableQuantityOnlyRec:      false,
			direction:                     direction,
			increaseButton:                buttonTarget{},
			decreaseButton:                buttonTarget{},
			centerPointOffset:             defaultCenterPointOffset,
			clampTargetToSliderMax:        params.ClampTargetToSliderMax,
			swipeButton:                   swipeButton,
			outOfRangeOverrideEnable:      outOfRangeOverrideEnable,
			targetReachableOverrideEnable: targetReachableOverrideEnable,
			targetQuantityType:            targetQuantityType,
			reverseTarget:                 params.ReverseTarget,
			swipeOnlyMode:                 true,
			finishAfterPreciseClick:       false,
			resetBeforeFindStart:          params.ResetBeforeFindStart,
		}, true
	}

	if params.TargetQuantity <= 0 {
		a.logger.Error().
			Int("target_quantity", params.TargetQuantity).
			Msg("invalid target quantity, must be greater than 0")
		return parsedBetterSlidingParams{}, false
	}

	increaseButton, err := normalizeButtonParam(params.IncreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize increase button")
		return parsedBetterSlidingParams{}, false
	}

	decreaseButton, err := normalizeButtonParam(params.DecreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize decrease button")
		return parsedBetterSlidingParams{}, false
	}

	centerPointOffset, err := normalizeCenterPointOffset(params.CenterPointOffset)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize center point offset")
		return parsedBetterSlidingParams{}, false
	}

	sliderQuantityFilter, err := normalizeQuantityFilter("SliderQuantity.Filter", params.SliderQuantity.Filter)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize slider quantity filter")
		return parsedBetterSlidingParams{}, false
	}

	sliderQuantityBox, sliderQuantityOnlyRec := normalizeQuantityParam(params.SliderQuantity)

	var availableQuantityFilter *quantityFilterParam
	availableQuantityBox := []int(nil)
	availableQuantityOnlyRec := false
	if params.presence.AvailableQuantity {
		availableQuantityFilter, err = normalizeQuantityFilter(
			"AvailableQuantity.Filter",
			params.AvailableQuantity.Filter,
		)
		if err != nil {
			a.logger.Error().
				Err(err).
				Msg("failed to normalize available quantity filter")
			return parsedBetterSlidingParams{}, false
		}
		availableQuantityBox, availableQuantityOnlyRec = normalizeQuantityParam(params.AvailableQuantity)
	}

	return parsedBetterSlidingParams{
		targetQuantity:                params.TargetQuantity,
		sliderQuantityBox:             sliderQuantityBox,
		availableQuantityBox:          availableQuantityBox,
		availableQuantityExplicit:     params.presence.AvailableQuantity,
		sliderQuantityFilter:          sliderQuantityFilter,
		availableQuantityFilter:       availableQuantityFilter,
		sliderQuantityOnlyRec:         sliderQuantityOnlyRec,
		availableQuantityOnlyRec:      availableQuantityOnlyRec,
		direction:                     strings.ToLower(strings.TrimSpace(params.Direction)),
		increaseButton:                increaseButton,
		decreaseButton:                decreaseButton,
		centerPointOffset:             centerPointOffset,
		clampTargetToSliderMax:        params.ClampTargetToSliderMax,
		swipeButton:                   swipeButton,
		outOfRangeOverrideEnable:      outOfRangeOverrideEnable,
		targetReachableOverrideEnable: targetReachableOverrideEnable,
		targetQuantityType:            targetQuantityType,
		reverseTarget:                 params.ReverseTarget,
		swipeOnlyMode:                 false,
		finishAfterPreciseClick:       params.FinishAfterPreciseClick,
		resetBeforeFindStart:          params.ResetBeforeFindStart,
	}, true
}

func (a *BetterSlidingAction) applyActionParams(params parsedBetterSlidingParams) {
	a.OriginalTargetQuantity = params.targetQuantity
	if !a.runtimeTargetResolved {
		a.TargetQuantity = params.targetQuantity
	}
	a.SliderQuantityBox = params.sliderQuantityBox
	a.AvailableQuantityBox = params.availableQuantityBox
	a.AvailableQuantityExplicit = params.availableQuantityExplicit
	a.SliderQuantityFilter = params.sliderQuantityFilter
	a.AvailableQuantityFilter = params.availableQuantityFilter
	a.SliderQuantityOnlyRec = params.sliderQuantityOnlyRec
	a.AvailableQuantityOnlyRec = params.availableQuantityOnlyRec
	a.Direction = params.direction
	a.IncreaseButton = params.increaseButton
	a.DecreaseButton = params.decreaseButton
	a.CenterPointOffset = params.centerPointOffset
	a.ClampTargetToSliderMax = params.clampTargetToSliderMax
	a.SwipeButton = params.swipeButton
	a.OutOfRangeOverrideEnable = params.outOfRangeOverrideEnable
	a.TargetReachableOverrideEnable = params.targetReachableOverrideEnable
	a.TargetQuantityType = params.targetQuantityType
	a.ReverseTarget = params.reverseTarget
	a.SwipeOnlyMode = params.swipeOnlyMode
	a.FinishAfterPreciseClick = params.finishAfterPreciseClick
	a.ResetBeforeFindStart = params.resetBeforeFindStart
}

func (a *BetterSlidingAction) logParsedActionParams() {
	parseLog := a.logger.Info().
		Int("target_quantity", a.OriginalTargetQuantity).
		Ints("slider_quantity_box", a.SliderQuantityBox).
		Ints("available_quantity_box", a.AvailableQuantityBox).
		Bool("available_quantity_explicit", a.AvailableQuantityExplicit).
		Str("direction", a.Direction).
		Interface("increase_button", a.IncreaseButton.logValue()).
		Interface("decrease_button", a.DecreaseButton.logValue()).
		Bool("slider_quantity_filter_enabled", a.SliderQuantityFilter != nil).
		Bool("available_quantity_filter_enabled", a.AvailableQuantityFilter != nil).
		Bool("slider_quantity_only_rec", a.SliderQuantityOnlyRec).
		Bool("available_quantity_only_rec", a.AvailableQuantityOnlyRec).
		Ints("center_point_offset", []int{a.CenterPointOffset[0], a.CenterPointOffset[1]}).
		Bool("clamp_target_to_slider_max", a.ClampTargetToSliderMax).
		Bool("finish_after_precise_click", a.FinishAfterPreciseClick).
		Bool("reset_before_find_start", a.ResetBeforeFindStart).
		Str("swipe_button", a.SwipeButton).
		Str("out_of_range_override_enable", a.OutOfRangeOverrideEnable).
		Str("target_reachable_override_enable", a.TargetReachableOverrideEnable).
		Str("target_quantity_type", a.TargetQuantityType).
		Bool("reverse_target", a.ReverseTarget).
		Bool("swipe_only_mode", a.SwipeOnlyMode)

	if a.runtimeTargetResolved {
		parseLog = parseLog.Int("runtime_target_quantity", a.TargetQuantity)
	}

	if a.SliderQuantityFilter != nil {
		parseLog = parseLog.
			Int("slider_quantity_filter_method", a.SliderQuantityFilter.Method).
			Ints("slider_quantity_filter_lower", a.SliderQuantityFilter.Lower).
			Ints("slider_quantity_filter_upper", a.SliderQuantityFilter.Upper)
	}

	if a.AvailableQuantityFilter != nil {
		parseLog = parseLog.
			Int("available_quantity_filter_method", a.AvailableQuantityFilter.Method).
			Ints("available_quantity_filter_lower", a.AvailableQuantityFilter.Lower).
			Ints("available_quantity_filter_upper", a.AvailableQuantityFilter.Upper)
	}

	parseLog.Msg("parsed custom action parameters")
}

func (a *BetterSlidingAction) initLogger(taskName string) {
	a.logger = log.With().
		Str("component", betterSlidingActionName).
		Str("task", taskName).
		Logger()
}

// mergeAttachParams reads the attach block from the caller pipeline node and merges
// TargetQuantity, TargetQuantityType, ReverseTarget, FinishAfterPreciseClick, and
// ResetBeforeFindStart into the customActionParam JSON.
// On any error, the original customActionParam string is returned unchanged.
func mergeAttachParams(ctx *maa.Context, callerNodeName string, customActionParam string) string {
	if ctx == nil || callerNodeName == "" {
		return customActionParam
	}

	logger := log.With().
		Str("component", betterSlidingActionName).
		Str("step", "mergeAttachParams").
		Logger()

	raw, err := ctx.GetNodeJSON(callerNodeName)
	if err != nil || raw == "" {
		if err != nil {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Msg("failed to get node json")
		}

		return customActionParam
	}

	var nodeWrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &nodeWrapper); err != nil {
		logger.Warn().
			Err(err).
			Str("node", callerNodeName).
			Msg("failed to unmarshal node json")

		return customActionParam
	}

	attachRaw, ok := nodeWrapper["attach"]
	if !ok || len(attachRaw) == 0 || string(attachRaw) == "null" {
		return customActionParam
	}

	var attachKeys map[string]json.RawMessage
	if err := json.Unmarshal(attachRaw, &attachKeys); err != nil {
		logger.Warn().
			Err(err).
			Str("node", callerNodeName).
			Msg("failed to unmarshal attach block")

		return customActionParam
	}

	var paramMap map[string]any
	if err := json.Unmarshal([]byte(customActionParam), &paramMap); err != nil {
		return customActionParam
	}

	if targetRaw, has := attachKeys["TargetQuantity"]; has {
		var target int
		if err := json.Unmarshal(targetRaw, &target); err == nil {
			paramMap["TargetQuantity"] = float64(target)
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.TargetQuantity").
				Str("value", string(targetRaw)).
				Msg("failed to parse attach field")
		}
	}

	if ttRaw, has := attachKeys["TargetQuantityType"]; has {
		var tt string
		if err := json.Unmarshal(ttRaw, &tt); err == nil {
			paramMap["TargetQuantityType"] = tt
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.TargetQuantityType").
				Str("value", string(ttRaw)).
				Msg("failed to parse attach field")
		}
	}

	if trRaw, has := attachKeys["ReverseTarget"]; has {
		var tr bool
		if err := json.Unmarshal(trRaw, &tr); err == nil {
			paramMap["ReverseTarget"] = tr
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.ReverseTarget").
				Str("value", string(trRaw)).
				Msg("failed to parse attach field")
		}
	}

	if fapcRaw, has := attachKeys["FinishAfterPreciseClick"]; has {
		var fapc bool
		if err := json.Unmarshal(fapcRaw, &fapc); err == nil {
			paramMap["FinishAfterPreciseClick"] = fapc
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.FinishAfterPreciseClick").
				Str("value", string(fapcRaw)).
				Msg("failed to parse attach field")
		}
	}

	if rbfsRaw, has := attachKeys["ResetBeforeFindStart"]; has {
		var rbfs bool
		if err := json.Unmarshal(rbfsRaw, &rbfs); err == nil {
			paramMap["ResetBeforeFindStart"] = rbfs
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.ResetBeforeFindStart").
				Str("value", string(rbfsRaw)).
				Msg("failed to parse attach field")
		}
	}

	out, err := json.Marshal(paramMap)
	if err != nil {
		return customActionParam
	}

	return string(out)
}
