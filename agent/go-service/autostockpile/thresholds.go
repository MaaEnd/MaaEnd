package autostockpile

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func minPositiveThreshold(priceLimits PriceLimitConfig) int {
	min := 0
	for _, threshold := range priceLimits {
		if threshold <= 0 {
			continue
		}
		if min == 0 || threshold < min {
			min = threshold
		}
	}
	if min > 0 {
		return min
	}
	return defaultFallbackBuyThreshold
}

func resolveTierThreshold(tierID string, cfg SelectionConfig) int {
	if threshold, ok := cfg.PriceLimits[tierID]; ok && threshold > 0 {
		return threshold
	}
	return resolveFallbackThreshold(cfg.FallbackThreshold)
}

func resolveFallbackThreshold(raw int) int {
	if raw > 0 {
		return raw
	}
	return defaultFallbackBuyThreshold
}

type thresholdConfigError struct {
	field string
	err   error
}

func (e *thresholdConfigError) Error() string {
	return fmt.Sprintf("%s: %v", e.field, e.err)
}

func (e *thresholdConfigError) Unwrap() error {
	return e.err
}

func newThresholdConfigError(field string, err error) error {
	if err == nil {
		return nil
	}

	var target *thresholdConfigError
	if errors.As(err, &target) {
		return err
	}

	return &thresholdConfigError{field: field, err: err}
}

func parsePositiveThresholdValue(field string, data json.RawMessage) (int, error) {
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		if strings.TrimSpace(stringValue) == "" {
			return 0, newThresholdConfigError(field, fmt.Errorf("must not be empty"))
		}

		parsed, parseErr := strconv.Atoi(stringValue)
		if parseErr != nil {
			return 0, newThresholdConfigError(field, fmt.Errorf("invalid integer string %q", stringValue))
		}
		if parsed <= 0 {
			return 0, newThresholdConfigError(field, fmt.Errorf("must be greater than 0"))
		}
		return parsed, nil
	}

	parsed, err := parsePriceLimitValue(data)
	if err != nil {
		return 0, newThresholdConfigError(field, err)
	}
	if parsed <= 0 {
		return 0, newThresholdConfigError(field, fmt.Errorf("must be greater than 0"))
	}

	return parsed, nil
}

func parsePriceLimitValue(data json.RawMessage) (int, error) {
	var intValue int
	if err := json.Unmarshal(data, &intValue); err == nil {
		return intValue, nil
	}

	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		parsed, parseErr := strconv.Atoi(stringValue)
		if parseErr != nil {
			return 0, fmt.Errorf("invalid integer string %q", stringValue)
		}
		return parsed, nil
	}

	return 0, fmt.Errorf("must be an integer or integer string")
}
