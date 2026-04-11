package bettersliding

import (
	"encoding/json"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type parsedBetterSlidingParams struct {
	target                  int
	quantityBox             []int
	quantityFilter          *quantityFilterParam
	quantityOnlyRec         bool
	greenMask               bool
	direction               string
	increaseButton          buttonTarget
	decreaseButton          buttonTarget
	centerPointOffset       [2]int
	clampTargetToMax        bool
	swipeButton             string
	exceedingOverrideEnable string
	targetType              string
	targetReverse           bool
	swipeOnlyMode           bool
	finishAfterPreciseClick bool
}

func detectBetterSlidingParamPresence(rawParam string) (betterSlidingParamPresence, error) {
	var rawKeys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(rawParam), &rawKeys); err != nil {
		return betterSlidingParamPresence{}, err
	}

	_, quantityPresent := rawKeys["Quantity"]

	return betterSlidingParamPresence{
		Target:                  hasNonNullRawKey(rawKeys, "Target"),
		Quantity:                quantityPresent,
		QuantityFilter:          hasNonNullRawKey(rawKeys, "QuantityFilter"),
		GreenMask:               hasNonNullRawKey(rawKeys, "GreenMask"),
		Direction:               hasNonNullRawKey(rawKeys, "Direction"),
		IncreaseButton:          hasNonNullRawKey(rawKeys, "IncreaseButton"),
		DecreaseButton:          hasNonNullRawKey(rawKeys, "DecreaseButton"),
		SwipeButton:             hasNonNullRawKey(rawKeys, "SwipeButton"),
		ExceedingOverrideEnable: hasNonNullRawKey(rawKeys, "ExceedingOverrideEnable"),
		TargetType:              hasNonNullRawKey(rawKeys, "TargetType"),
		TargetReverse:           hasNonNullRawKey(rawKeys, "TargetReverse"),
		CenterPointOffset:       hasNonNullRawKey(rawKeys, "CenterPointOffset"),
		ClampTargetToMax:        hasNonNullRawKey(rawKeys, "ClampTargetToMax"),
		FinishAfterPreciseClick: hasNonNullRawKey(rawKeys, "FinishAfterPreciseClick"),
	}, nil
}

func hasNonNullRawKey(rawKeys map[string]json.RawMessage, key string) bool {
	raw, ok := rawKeys[key]
	return ok && len(raw) > 0 && string(raw) != "null"
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
	exceedingOverrideEnable := strings.TrimSpace(params.ExceedingOverrideEnable)

	targetType, err := normalizeTargetType(params.TargetType)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("target_type", params.TargetType).
			Msg("invalid TargetType")
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
			target:                  0,
			quantityBox:             nil,
			quantityFilter:          nil,
			quantityOnlyRec:         false,
			greenMask:               params.GreenMask,
			direction:               direction,
			increaseButton:          buttonTarget{},
			decreaseButton:          buttonTarget{},
			centerPointOffset:       defaultCenterPointOffset,
			clampTargetToMax:        params.ClampTargetToMax,
			swipeButton:             swipeButton,
			exceedingOverrideEnable: exceedingOverrideEnable,
			targetType:              targetType,
			targetReverse:           params.TargetReverse,
			swipeOnlyMode:           true,
			finishAfterPreciseClick: false,
		}, true
	}

	if params.Target <= 0 {
		a.logger.Error().
			Int("target", params.Target).
			Msg("invalid target, must be greater than 0")
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

	quantityFilter, err := normalizeQuantityFilter(params.QuantityFilter)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize quantity filter")
		return parsedBetterSlidingParams{}, false
	}

	quantityOnlyRec := false
	if params.Quantity.OnlyRec != nil {
		quantityOnlyRec = *params.Quantity.OnlyRec
	}

	return parsedBetterSlidingParams{
		target:                  params.Target,
		quantityBox:             append([]int(nil), params.Quantity.Box...),
		quantityFilter:          quantityFilter,
		quantityOnlyRec:         quantityOnlyRec,
		greenMask:               params.GreenMask,
		direction:               strings.ToLower(strings.TrimSpace(params.Direction)),
		increaseButton:          increaseButton,
		decreaseButton:          decreaseButton,
		centerPointOffset:       centerPointOffset,
		clampTargetToMax:        params.ClampTargetToMax,
		swipeButton:             swipeButton,
		exceedingOverrideEnable: exceedingOverrideEnable,
		targetType:              targetType,
		targetReverse:           params.TargetReverse,
		swipeOnlyMode:           false,
		finishAfterPreciseClick: params.FinishAfterPreciseClick,
	}, true
}

func (a *BetterSlidingAction) applyActionParams(params parsedBetterSlidingParams) {
	a.OriginalTarget = params.target
	if !a.runtimeTargetResolved {
		a.Target = params.target
	}
	a.QuantityBox = params.quantityBox
	a.QuantityFilter = params.quantityFilter
	a.QuantityOnlyRec = params.quantityOnlyRec
	a.GreenMask = params.greenMask
	a.Direction = params.direction
	a.IncreaseButton = params.increaseButton
	a.DecreaseButton = params.decreaseButton
	a.CenterPointOffset = params.centerPointOffset
	a.ClampTargetToMax = params.clampTargetToMax
	a.SwipeButton = params.swipeButton
	a.ExceedingOverrideEnable = params.exceedingOverrideEnable
	a.TargetType = params.targetType
	a.TargetReverse = params.targetReverse
	a.SwipeOnlyMode = params.swipeOnlyMode
	a.FinishAfterPreciseClick = params.finishAfterPreciseClick
}

func (a *BetterSlidingAction) logParsedActionParams() {
	parseLog := a.logger.Info().
		Int("target", a.OriginalTarget).
		Ints("quantity_box", a.QuantityBox).
		Str("direction", a.Direction).
		Interface("increase_button", a.IncreaseButton.logValue()).
		Interface("decrease_button", a.DecreaseButton.logValue()).
		Bool("green_mask", a.GreenMask).
		Bool("quantity_filter_enabled", a.QuantityFilter != nil).
		Bool("quantity_only_rec", a.QuantityOnlyRec).
		Ints("center_point_offset", []int{a.CenterPointOffset[0], a.CenterPointOffset[1]}).
		Bool("clamp_target_to_max", a.ClampTargetToMax).
		Bool("finish_after_precise_click", a.FinishAfterPreciseClick).
		Str("swipe_button", a.SwipeButton).
		Str("exceeding_override_enable", a.ExceedingOverrideEnable).
		Str("target_type", a.TargetType).
		Bool("target_reverse", a.TargetReverse).
		Bool("swipe_only_mode", a.SwipeOnlyMode)

	if a.runtimeTargetResolved {
		parseLog = parseLog.Int("runtime_target", a.Target)
	}

	if a.QuantityFilter != nil {
		parseLog = parseLog.
			Int("quantity_filter_method", a.QuantityFilter.Method).
			Ints("quantity_filter_lower", a.QuantityFilter.Lower).
			Ints("quantity_filter_upper", a.QuantityFilter.Upper)
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
// Target, TargetType, TargetReverse, and FinishAfterPreciseClick into the customActionParam JSON.
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

	if targetRaw, has := attachKeys["Target"]; has {
		var target int
		if err := json.Unmarshal(targetRaw, &target); err == nil {
			paramMap["Target"] = float64(target)
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.Target").
				Str("value", string(targetRaw)).
				Msg("failed to parse attach field")
		}
	}

	if ttRaw, has := attachKeys["TargetType"]; has {
		var tt string
		if err := json.Unmarshal(ttRaw, &tt); err == nil {
			paramMap["TargetType"] = tt
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.TargetType").
				Str("value", string(ttRaw)).
				Msg("failed to parse attach field")
		}
	}

	if trRaw, has := attachKeys["TargetReverse"]; has {
		var tr bool
		if err := json.Unmarshal(trRaw, &tr); err == nil {
			paramMap["TargetReverse"] = tr
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.TargetReverse").
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

	out, err := json.Marshal(paramMap)
	if err != nil {
		return customActionParam
	}

	return string(out)
}
