package quantizedsliding

import (
	"encoding/json"
	"strings"

	"github.com/rs/zerolog/log"
)

type parsedQuantizedSlidingParams struct {
	target            int
	quantityBox       []int
	quantityFilter    *quantityFilterParam
	direction         string
	increaseButton    buttonTarget
	decreaseButton    buttonTarget
	centerPointOffset [2]int
}

func parseQuantizedSlidingParam(customActionParam string) (quantizedSlidingParam, error) {
	var params quantizedSlidingParam
	if err := json.Unmarshal([]byte(customActionParam), &params); err != nil {
		return quantizedSlidingParam{}, err
	}

	return params, nil
}

func (a *QuantizedSlidingAction) loadActionParams(customActionParam string) bool {
	params, err := parseQuantizedSlidingParam(customActionParam)
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

func (a *QuantizedSlidingAction) normalizeActionParams(params quantizedSlidingParam) (parsedQuantizedSlidingParams, bool) {
	if params.Target <= 0 {
		a.logger.Error().
			Int("target", params.Target).
			Msg("invalid target, must be greater than 0")
		return parsedQuantizedSlidingParams{}, false
	}

	increaseButton, err := normalizeButtonParam(params.IncreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize increase button")
		return parsedQuantizedSlidingParams{}, false
	}

	decreaseButton, err := normalizeButtonParam(params.DecreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize decrease button")
		return parsedQuantizedSlidingParams{}, false
	}

	centerPointOffset, err := normalizeCenterPointOffset(params.CenterPointOffset)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize center point offset")
		return parsedQuantizedSlidingParams{}, false
	}

	quantityFilter, err := normalizeQuantityFilter(params.QuantityFilter)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize quantity filter")
		return parsedQuantizedSlidingParams{}, false
	}

	return parsedQuantizedSlidingParams{
		target:            params.Target,
		quantityBox:       append([]int(nil), params.QuantityBox...),
		quantityFilter:    quantityFilter,
		direction:         strings.ToLower(strings.TrimSpace(params.Direction)),
		increaseButton:    increaseButton,
		decreaseButton:    decreaseButton,
		centerPointOffset: centerPointOffset,
	}, true
}

func (a *QuantizedSlidingAction) applyActionParams(params parsedQuantizedSlidingParams) {
	a.Target = params.target
	a.QuantityBox = params.quantityBox
	a.QuantityFilter = params.quantityFilter
	a.Direction = params.direction
	a.IncreaseButton = params.increaseButton
	a.DecreaseButton = params.decreaseButton
	a.CenterPointOffset = params.centerPointOffset
}

func (a *QuantizedSlidingAction) logParsedActionParams() {
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
}

func (a *QuantizedSlidingAction) initLogger(taskName string) {
	a.logger = log.With().
		Str("component", "QuantizedSliding").
		Str("task", taskName).
		Logger()
}
