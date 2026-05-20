package exprcoord

import (
	"fmt"
	"math"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// ResolveRect resolves a 4-element mixed array (each element: float64 or string expression) into maa.Rect.
func ResolveRect(raw []any, w, h int) (maa.Rect, error) {
	if len(raw) != 4 {
		return maa.Rect{}, fmt.Errorf("rect requires 4 elements, got %d", len(raw))
	}
	vals := make([]int, 4)
	for i, v := range raw {
		n, err := resolveValue(v, w, h)
		if err != nil {
			return maa.Rect{}, err
		}
		vals[i] = int(math.Round(n))
	}
	return maa.Rect{vals[0], vals[1], vals[2], vals[3]}, nil
}

// ResolvePoint resolves a 2-element mixed array into (x, y, error).
func ResolvePoint(raw []any, w, h int) (int, int, error) {
	if len(raw) != 2 {
		return 0, 0, fmt.Errorf("point requires 2 elements, got %d", len(raw))
	}
	x, err := resolveValue(raw[0], w, h)
	if err != nil {
		return 0, 0, err
	}
	y, err := resolveValue(raw[1], w, h)
	if err != nil {
		return 0, 0, err
	}
	return int(math.Round(x)), int(math.Round(y)), nil
}

func resolveValue(v any, w, h int) (float64, error) {
	switch val := v.(type) {
	case int:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case float64:
		return val, nil
	case string:
		return Eval(val, w, h)
	default:
		return 0, fmt.Errorf("unsupported coordinate value type %T", v)
	}
}
