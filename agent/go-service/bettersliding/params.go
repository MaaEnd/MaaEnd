package bettersliding

import (
	"encoding/json"
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type parsedBetterSlidingParams struct {
	targetQuantity                int
	availableQuantityExplicit     bool
	direction                     string
	increaseButton                buttonTarget
	decreaseButton                buttonTarget
	centerPointOffset             [2]int
	clampTargetToSliderMax        bool
	outOfRangeOverrideEnable      string
	targetReachableOverrideEnable string
	targetQuantityType            string
	reverseTarget                 bool
	swipeOnlyMode                 bool
	fineTuneQuantity              fineTuneQuantity
	fineTuneFallback              string
	resetBeforeFindStart          bool

	swipeButtonPatch             map[string]any
	sliderQuantityPatch          map[string]any
	sliderQuantityFilterNode     string
	sliderQuantityFilterPatch    map[string]any
	availableQuantityPatch       map[string]any
	availableQuantityFilterNode  string
	availableQuantityFilterPatch map[string]any
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
		SliderQuantityFilter:          hasNonNullRawKey(rawKeys, "SliderQuantityFilter"),
		AvailableQuantity:             hasNonNullRawKey(rawKeys, "AvailableQuantity"),
		AvailableQuantityFilter:       hasNonNullRawKey(rawKeys, "AvailableQuantityFilter"),
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
		FineTuneQuantity:              hasNonNullRawKey(rawKeys, "FineTuneQuantity"),
		FineTuneFallback:              hasNonNullRawKey(rawKeys, "FineTuneFallback"),
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

func (a *BetterSlidingAction) loadActionParams(ctx *maa.Context, customActionParam string) bool {
	params, err := parseBetterSlidingParam(customActionParam)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("param", customActionParam).
			Msg("failed to parse custom_action_param")
		return false
	}

	parsed, ok := a.normalizeActionParams(ctx, params)
	if !ok {
		return false
	}

	a.applyActionParams(parsed)
	a.logParsedActionParams()
	return true
}

func (a *BetterSlidingAction) normalizeActionParams(
	ctx *maa.Context,
	params betterSlidingParam,
) (parsedBetterSlidingParams, bool) {
	parsed := parsedBetterSlidingParams{
		targetQuantity:                params.TargetQuantity,
		availableQuantityExplicit:     params.presence.AvailableQuantity,
		direction:                     strings.ToLower(strings.TrimSpace(params.Direction)),
		centerPointOffset:             defaultCenterPointOffset,
		clampTargetToSliderMax:        params.ClampTargetToSliderMax,
		outOfRangeOverrideEnable:      strings.TrimSpace(params.OutOfRangeOverrideEnable),
		targetReachableOverrideEnable: strings.TrimSpace(params.TargetReachableOverrideEnable),
		fineTuneQuantity:              defaultFineTuneQuantity,
		fineTuneFallback:              FineTuneFallbackNone,
		resetBeforeFindStart:          params.ResetBeforeFindStart,
	}

	if !a.validateOutcomeOverrideNodes(
		parsed.outOfRangeOverrideEnable,
		parsed.targetReachableOverrideEnable,
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
	parsed.targetQuantityType = targetQuantityType
	parsed.reverseTarget = params.ReverseTarget

	fineTuneQuantity, err := normalizeFineTuneQuantity(
		params.FineTuneQuantity,
		params.presence.FineTuneQuantity,
	)
	if err != nil {
		a.logger.Error().
			Err(err).
			Interface("fine_tune_quantity", params.FineTuneQuantity).
			Msg("invalid FineTuneQuantity")
		return parsedBetterSlidingParams{}, false
	}
	parsed.fineTuneQuantity = fineTuneQuantity

	fineTuneFallback, err := normalizeFineTuneFallback(params.FineTuneFallback)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("fine_tune_fallback", params.FineTuneFallback).
			Msg("invalid FineTuneFallback")
		return parsedBetterSlidingParams{}, false
	}
	parsed.fineTuneFallback = fineTuneFallback

	swipeButtonPatch, err := resolveRecognitionParam(ctx, params.SwipeButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Interface("swipe_button", params.SwipeButton).
			Msg("invalid SwipeButton")
		return parsedBetterSlidingParams{}, false
	}
	parsed.swipeButtonPatch = swipeButtonPatch

	if isSwipeOnlyMode(params) {
		switch parsed.direction {
		case "left", "right", "up", "down":
		default:
			a.logger.Error().
				Str("direction", params.Direction).
				Msg("invalid direction for swipe-only mode")
			return parsedBetterSlidingParams{}, false
		}

		parsed.targetQuantity = 0
		parsed.swipeOnlyMode = true
		return parsed, true
	}

	if params.TargetQuantity <= 0 {
		a.logger.Error().
			Int("target_quantity", params.TargetQuantity).
			Msg("invalid target quantity, must be greater than 0")
		return parsedBetterSlidingParams{}, false
	}

	increaseButton, err := resolveButtonTarget(ctx, params.IncreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Interface("increase_button", params.IncreaseButton).
			Msg("failed to normalize increase button")
		return parsedBetterSlidingParams{}, false
	}
	parsed.increaseButton = increaseButton

	decreaseButton, err := resolveButtonTarget(ctx, params.DecreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Interface("decrease_button", params.DecreaseButton).
			Msg("failed to normalize decrease button")
		return parsedBetterSlidingParams{}, false
	}
	parsed.decreaseButton = decreaseButton

	centerPointOffset, err := normalizeCenterPointOffset(params.CenterPointOffset)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize center point offset")
		return parsedBetterSlidingParams{}, false
	}
	parsed.centerPointOffset = centerPointOffset

	sliderQuantityFilterNode, sliderQuantityFilterPatch, err := resolveFilterPatch(
		ctx,
		params.SliderQuantityFilter,
		nodeBetterSlidingSliderQuantityFilter,
	)
	if err != nil {
		a.logger.Error().
			Err(err).
			Interface("slider_quantity_filter", params.SliderQuantityFilter).
			Msg("invalid SliderQuantityFilter")
		return parsedBetterSlidingParams{}, false
	}
	parsed.sliderQuantityFilterNode = sliderQuantityFilterNode
	parsed.sliderQuantityFilterPatch = sliderQuantityFilterPatch

	sliderQuantityPatch, err := resolveRecognitionParam(ctx, params.SliderQuantity)
	if err != nil {
		a.logger.Error().
			Err(err).
			Interface("slider_quantity", params.SliderQuantity).
			Msg("invalid SliderQuantity")
		return parsedBetterSlidingParams{}, false
	}
	applyColorFilter(sliderQuantityPatch, sliderQuantityFilterNode)
	parsed.sliderQuantityPatch = sliderQuantityPatch

	// AvailableQuantityFilter 独立于 AvailableQuantity：单独配置时仍写入内建 Filter 节点，
	// 而 Quantity 节点保持 enabled:false（不告警）。
	availableQuantityFilterNode, availableQuantityFilterPatch, err := resolveFilterPatch(
		ctx,
		params.AvailableQuantityFilter,
		nodeBetterSlidingAvailableQuantityFilter,
	)
	if err != nil {
		a.logger.Error().
			Err(err).
			Interface("available_quantity_filter", params.AvailableQuantityFilter).
			Msg("invalid AvailableQuantityFilter")
		return parsedBetterSlidingParams{}, false
	}
	parsed.availableQuantityFilterNode = availableQuantityFilterNode
	parsed.availableQuantityFilterPatch = availableQuantityFilterPatch

	if params.presence.AvailableQuantity {
		availableQuantityPatch, err := resolveRecognitionParam(ctx, params.AvailableQuantity)
		if err != nil {
			a.logger.Error().
				Err(err).
				Interface("available_quantity", params.AvailableQuantity).
				Msg("invalid AvailableQuantity")
			return parsedBetterSlidingParams{}, false
		}
		applyColorFilter(availableQuantityPatch, availableQuantityFilterNode)
		parsed.availableQuantityPatch = availableQuantityPatch
	}

	return parsed, true
}

func (a *BetterSlidingAction) applyActionParams(params parsedBetterSlidingParams) {
	a.OriginalTargetQuantity = params.targetQuantity
	if !a.runtimeTargetResolved {
		a.TargetQuantity = params.targetQuantity
	}
	a.AvailableQuantityExplicit = params.availableQuantityExplicit
	a.Direction = params.direction
	a.IncreaseButton = params.increaseButton
	a.DecreaseButton = params.decreaseButton
	a.CenterPointOffset = params.centerPointOffset
	a.ClampTargetToSliderMax = params.clampTargetToSliderMax
	a.OutOfRangeOverrideEnable = params.outOfRangeOverrideEnable
	a.TargetReachableOverrideEnable = params.targetReachableOverrideEnable
	a.TargetQuantityType = params.targetQuantityType
	a.ReverseTarget = params.reverseTarget
	a.SwipeOnlyMode = params.swipeOnlyMode
	a.FineTuneQuantity = params.fineTuneQuantity
	a.FineTuneFallback = params.fineTuneFallback
	a.ResetBeforeFindStart = params.resetBeforeFindStart

	a.swipeButtonPatch = params.swipeButtonPatch
	a.sliderQuantityPatch = params.sliderQuantityPatch
	a.sliderQuantityFilterNode = params.sliderQuantityFilterNode
	a.sliderQuantityFilterPatch = params.sliderQuantityFilterPatch
	a.availableQuantityPatch = params.availableQuantityPatch
	a.availableQuantityFilterNode = params.availableQuantityFilterNode
	a.availableQuantityFilterPatch = params.availableQuantityFilterPatch
}

// logParsedActionParams 只输出补丁的键集合与字节数级摘要，不再展开 Box / OnlyRec / Filter 明细。
func (a *BetterSlidingAction) logParsedActionParams() {
	parseLog := a.logger.Info().
		Int("target_quantity", a.OriginalTargetQuantity).
		Bool("available_quantity_explicit", a.AvailableQuantityExplicit).
		Str("direction", a.Direction).
		Interface("increase_button", a.IncreaseButton.logValue()).
		Interface("decrease_button", a.DecreaseButton.logValue()).
		Ints("center_point_offset", []int{a.CenterPointOffset[0], a.CenterPointOffset[1]}).
		Bool("clamp_target_to_slider_max", a.ClampTargetToSliderMax).
		Str("fine_tune_quantity_mode", a.FineTuneQuantity.modeLabel()).
		Bool("fine_tune_quantity_enabled", a.FineTuneQuantity.enabled).
		Int("fine_tune_quantity_threshold", a.FineTuneQuantity.threshold).
		Str("fine_tune_fallback", a.FineTuneFallback).
		Bool("reset_before_find_start", a.ResetBeforeFindStart).
		Str("out_of_range_override_enable", a.OutOfRangeOverrideEnable).
		Str("target_reachable_override_enable", a.TargetReachableOverrideEnable).
		Str("target_quantity_type", a.TargetQuantityType).
		Bool("reverse_target", a.ReverseTarget).
		Bool("swipe_only_mode", a.SwipeOnlyMode)

	if a.runtimeTargetResolved {
		parseLog = parseLog.Int("runtime_target_quantity", a.TargetQuantity)
	}

	patches := []struct {
		name  string
		patch map[string]any
	}{
		{"swipe_button", a.swipeButtonPatch},
		{"slider_quantity", a.sliderQuantityPatch},
		{"slider_quantity_filter", a.sliderQuantityFilterPatch},
		{"available_quantity", a.availableQuantityPatch},
		{"available_quantity_filter", a.availableQuantityFilterPatch},
	}
	for _, entry := range patches {
		if len(entry.patch) == 0 {
			continue
		}
		parseLog = parseLog.
			Str(entry.name+"_patch_keys", strings.Join(sortedKeys(entry.patch), ",")).
			Int(entry.name+"_patch_size", len(entry.patch))
	}

	parseLog.Msg("parsed custom action parameters")
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (a *BetterSlidingAction) initLogger(taskName string) {
	a.logger = log.With().
		Str("component", betterSlidingActionName).
		Str("task", taskName).
		Logger()
}

// mergeAttachParams reads the attach block from the caller pipeline node and merges the
// recognized fields into the customActionParam JSON.
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

	if ftqRaw, has := attachKeys["FineTuneQuantity"]; has {
		var ftq any
		if err := json.Unmarshal(ftqRaw, &ftq); err == nil {
			paramMap["FineTuneQuantity"] = ftq
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.FineTuneQuantity").
				Str("value", string(ftqRaw)).
				Msg("failed to parse attach field")
		}
	}

	if ftfRaw, has := attachKeys["FineTuneFallback"]; has {
		var ftf string
		if err := json.Unmarshal(ftfRaw, &ftf); err == nil {
			paramMap["FineTuneFallback"] = ftf
		} else {
			logger.Warn().
				Err(err).
				Str("node", callerNodeName).
				Str("field", "attach.FineTuneFallback").
				Str("value", string(ftfRaw)).
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
