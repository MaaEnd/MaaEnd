package quantizedsliding

import (
	"fmt"
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

func normalizeQuantityFilter(raw *quantityFilterParam) (*quantityFilterParam, error) {
	if raw == nil {
		return nil, nil
	}

	if len(raw.Lower) == 0 || len(raw.Upper) == 0 {
		return nil, fmt.Errorf("QuantityFilter lower and upper must both be provided")
	}

	if len(raw.Lower) != len(raw.Upper) {
		return nil, fmt.Errorf("QuantityFilter lower and upper must have the same length, got lower=%d upper=%d", len(raw.Lower), len(raw.Upper))
	}

	channelCount, err := quantityFilterChannelCount(raw.Method)
	if err != nil {
		return nil, err
	}

	if len(raw.Lower) != channelCount {
		return nil, fmt.Errorf("QuantityFilter lower and upper must each contain %d values for method %d, got %d", channelCount, raw.Method, len(raw.Lower))
	}

	return &quantityFilterParam{
		Lower:  append([]int(nil), raw.Lower...),
		Upper:  append([]int(nil), raw.Upper...),
		Method: raw.Method,
	}, nil
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
