package autostockpile

import (
	"errors"
	"fmt"
	"strings"
)

func resolveTierThreshold(tierID string, cfg SelectionConfig) (int, error) {
	tierID = strings.TrimSpace(tierID)
	if tierID == "" {
		return 0, fmt.Errorf("tier is empty")
	}

	threshold, ok := cfg.PriceLimits[tierID]
	if !ok {
		return 0, newThresholdConfigError("price_limits."+tierID, fmt.Errorf("tier %q is not configured in price_limits", tierID))
	}

	return threshold, nil
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
