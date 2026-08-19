package bettersliding

import (
	"fmt"
	"math"
	"strings"
)

func clampClickRepeat(repeat int) int {
	if repeat < 0 {
		return 0
	}
	if repeat > maxClickRepeat {
		return maxClickRepeat
	}

	return repeat
}

func normalizeButton(btn any) ([]int, error) {
	numbers, err := normalizeIntSlice(btn)
	if err != nil {
		return nil, err
	}

	switch len(numbers) {
	case 2:
		return []int{numbers[0], numbers[1], 1, 1}, nil
	case 4:
		return []int{numbers[0], numbers[1], numbers[2], numbers[3]}, nil
	default:
		return nil, fmt.Errorf("button must be [x,y] or [x,y,w,h], got len=%d", len(numbers))
	}
}

func normalizeButtonParam(btn any) (buttonTarget, error) {
	if template, ok := btn.(string); ok {
		template = strings.TrimSpace(template)
		if template == "" {
			return buttonTarget{}, fmt.Errorf("button template must not be empty")
		}

		return buttonTarget{template: template}, nil
	}

	coordinates, err := normalizeButton(btn)
	if err != nil {
		return buttonTarget{}, err
	}

	return buttonTarget{coordinates: coordinates}, nil
}

func normalizeCenterPointOffset(raw any) ([2]int, error) {
	if raw == nil {
		return defaultCenterPointOffset, nil
	}

	numbers, err := normalizeIntSlice(raw)
	if err != nil {
		return [2]int{}, err
	}

	if len(numbers) != 2 {
		return [2]int{}, fmt.Errorf("centerPointOffset must be [x,y], got len=%d", len(numbers))
	}

	return [2]int{numbers[0], numbers[1]}, nil
}

func normalizeQuantityFilter(fieldName string, raw *quantityFilterParam) (*quantityFilterParam, error) {
	if raw == nil {
		return nil, nil
	}

	if len(raw.Lower) == 0 || len(raw.Upper) == 0 {
		return nil, fmt.Errorf("%s lower and upper must both be provided", fieldName)
	}

	if len(raw.Lower) != len(raw.Upper) {
		return nil, fmt.Errorf("%s lower and upper must have the same length, got lower=%d upper=%d", fieldName, len(raw.Lower), len(raw.Upper))
	}

	channelCount, err := quantityFilterChannelCount(raw.Method)
	if err != nil {
		return nil, err
	}

	if len(raw.Lower) != channelCount {
		return nil, fmt.Errorf("%s lower and upper must each contain %d values for method %d, got %d", fieldName, channelCount, raw.Method, len(raw.Lower))
	}

	return &quantityFilterParam{
		Lower:  append([]int(nil), raw.Lower...),
		Upper:  append([]int(nil), raw.Upper...),
		Method: raw.Method,
	}, nil
}

func normalizeQuantityParam(raw quantityParam) ([]int, bool) {
	onlyRec := false
	if raw.OnlyRec != nil {
		onlyRec = *raw.OnlyRec
	}

	return append([]int(nil), raw.Box...), onlyRec
}

func quantityFilterChannelCount(method int) (int, error) {
	switch method {
	case 4, 40:
		return 3, nil
	case 6:
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported QuantityFilter method %d, expected 4 (RGB), 40 (HSV), or 6 (GRAY)", method)
	}
}

func normalizeIntSlice(raw any) ([]int, error) {
	switch v := raw.(type) {
	case []int:
		return append([]int(nil), v...), nil
	case []float64:
		result := make([]int, 0, len(v))
		for _, item := range v {
			result = append(result, int(item))
		}
		return result, nil
	case []any:
		result := make([]int, 0, len(v))
		for _, item := range v {
			num, ok := item.(float64)
			if !ok {
				return nil, fmt.Errorf("unsupported number type %T", item)
			}
			result = append(result, int(num))
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported button type %T", raw)
	}
}

func centerPoint(rect []int, offset [2]int) (int, int) {
	if len(rect) < 4 {
		return 0, 0
	}
	return rect[0] + rect[2]/2 + offset[0], rect[1] + rect[3]/2 + offset[1]
}

// normalizeTargetQuantityType normalizes a TargetQuantityType string, returning the canonical
// form. An empty string defaults to TargetQuantityTypeValue.
func normalizeTargetQuantityType(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return TargetQuantityTypeValue, nil
	}

	switch strings.ToLower(s) {
	case "value":
		return TargetQuantityTypeValue, nil
	case "percentage":
		return TargetQuantityTypePercentage, nil
	default:
		return "", fmt.Errorf(
			"invalid TargetQuantityType %q, expected %q or %q",
			raw,
			TargetQuantityTypeValue,
			TargetQuantityTypePercentage,
		)
	}
}

// resolveTargetQuantity computes the effective slider quantity using the available quantity as
// the reference for percentage and reverse calculations.
//
//	Value + !Reverse → targetQuantity unchanged.
//	Value + Reverse  → availableQuantity - targetQuantity (may be < 1).
//	Percentage + !Reverse → round(availableQuantity * targetQuantity / 100), clamped.
//	Percentage + Reverse  → round(availableQuantity * (100-targetQuantity) / 100), clamped.
func resolveTargetQuantity(
	targetQuantity int,
	targetQuantityType string,
	reverseTarget bool,
	availableQuantity int,
) (int, error) {
	switch targetQuantityType {
	case TargetQuantityTypeValue:
		if !reverseTarget {
			return targetQuantity, nil
		}

		return availableQuantity - targetQuantity, nil

	case TargetQuantityTypePercentage:
		if targetQuantity == 0 {
			return 0, fmt.Errorf("percentage target must be greater than 0")
		}

		if targetQuantity > 100 {
			return 0, fmt.Errorf("percentage target must be at most 100, got %d", targetQuantity)
		}

		var factor float64
		if !reverseTarget {
			factor = float64(targetQuantity) / 100.0
		} else {
			factor = float64(100-targetQuantity) / 100.0
		}

		resolved := int(math.Round(float64(availableQuantity) * factor))
		if resolved < 1 {
			resolved = 1
		}

		if resolved > availableQuantity {
			resolved = availableQuantity
		}

		return resolved, nil

	default:
		return 0, fmt.Errorf("invalid target quantity type %q", targetQuantityType)
	}
}

func isSwipeOnlyMode(params betterSlidingParam) bool {
	return !params.presence.TargetQuantity &&
		!params.presence.SliderQuantity &&
		!params.presence.AvailableQuantity &&
		!params.presence.IncreaseButton &&
		!params.presence.DecreaseButton &&
		!params.presence.OutOfRangeOverrideEnable &&
		!params.presence.TargetReachableOverrideEnable &&
		!params.presence.TargetQuantityType &&
		!params.presence.ReverseTarget &&
		!params.presence.CenterPointOffset &&
		!params.presence.ClampTargetToSliderMax
}
