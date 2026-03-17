package quantizedsliding

import (
	"encoding/json"
	"fmt"
)

func buildSwipeEnd(direction string) ([]int, error) {
	switch direction {
	case "right", "up":
		return []int{1260, 10, 10, 10}, nil
	case "left", "down":
		return []int{10, 700, 10, 10}, nil
	default:
		return nil, fmt.Errorf("unsupported direction %q", direction)
	}
}

func buildMainInitializationOverride(end []int, quantityBox []int, quantityFilter *quantityFilterParam) map[string]any {
	quantityParam := map[string]any{
		"roi": append([]int(nil), quantityBox...),
	}

	override := map[string]any{
		"QuantizedSlidingSwipeToMax": map[string]any{
			"action": map[string]any{
				"param": map[string]any{
					"end": append([]int(nil), end...),
				},
			},
		},
		"QuantizedSlidingGetQuantity": map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"roi": quantityParam["roi"],
				},
			},
		},
	}

	if quantityFilter == nil {
		return override
	}

	quantityParam["color_filter"] = "QuantizedSlidingQuantityFilter"
	override["QuantizedSlidingGetQuantity"] = map[string]any{
		"recognition": map[string]any{
			"param": quantityParam,
		},
	}
	override["QuantizedSlidingQuantityFilter"] = map[string]any{
		"recognition": map[string]any{
			"param": map[string]any{
				"method": quantityFilter.Method,
				"lower":  [][]int{append([]int(nil), quantityFilter.Lower...)},
				"upper":  [][]int{append([]int(nil), quantityFilter.Upper...)},
			},
		},
	}

	return override
}

func buildCheckQuantityBranchOverride(nextNode string, target buttonTarget, repeat int) map[string]any {
	override := map[string]any{
		"QuantizedSlidingDone": map[string]any{
			"enabled": nextNode == "QuantizedSlidingDone",
		},
		"QuantizedSlidingIncreaseQuantity": map[string]any{
			"enabled": nextNode == "QuantizedSlidingIncreaseQuantity",
		},
		"QuantizedSlidingDecreaseQuantity": map[string]any{
			"enabled": nextNode == "QuantizedSlidingDecreaseQuantity",
		},
	}

	if nextNode != "QuantizedSlidingIncreaseQuantity" && nextNode != "QuantizedSlidingDecreaseQuantity" {
		return override
	}

	repeat = clampClickRepeat(repeat)

	if target.template != "" {
		override[nextNode] = buildTemplateMatchButtonOverride(target.template, repeat)
		return override
	}

	override[nextNode] = map[string]any{
		"enabled": true,
		"action": map[string]any{
			"param": map[string]any{
				"target": append([]int(nil), target.coordinates...),
			},
		},
		"repeat": repeat,
	}

	return override
}

func buildTemplateMatchButtonOverride(template string, repeat int) map[string]any {
	return map[string]any{
		"enabled": true,
		"recognition": map[string]any{
			"type": "TemplateMatch",
			"param": map[string]any{
				"template":   []string{template},
				"threshold":  []float64{0.8},
				"green_mask": true,
			},
		},
		"action": map[string]any{
			"type": "Click",
			"param": map[string]any{
				"target":        true,
				"target_offset": []int{5, 5, -5, -5},
			},
		},
		"repeat": repeat,
	}
}

func buildInternalPipelineOverride(customActionParam string) (map[string]any, error) {
	paramValue, err := parseInternalPipelineCustomActionParam(customActionParam)
	if err != nil {
		return nil, err
	}

	override := make(map[string]any, len(quantizedSlidingActionNodes))
	for _, nodeName := range quantizedSlidingActionNodes {
		override[nodeName] = map[string]any{
			"action": map[string]any{
				"param": map[string]any{
					"custom_action_param": paramValue,
				},
			},
		}
	}

	return override, nil
}

func parseInternalPipelineCustomActionParam(customActionParam string) (any, error) {
	var paramValue any
	if err := json.Unmarshal([]byte(customActionParam), &paramValue); err != nil {
		return nil, err
	}

	if nestedParam, ok := paramValue.(string); ok {
		var nestedValue any
		if err := json.Unmarshal([]byte(nestedParam), &nestedValue); err == nil {
			return nestedValue, nil
		}
	}

	return paramValue, nil
}
