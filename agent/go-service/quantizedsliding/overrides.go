package quantizedsliding

import (
	"encoding/json"
	"errors"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	errCheckQuantityBranchPipelineOverride = errors.New("check quantity branch pipeline override failed")
	errCheckQuantityBranchNextOverride     = errors.New("check quantity branch next override failed")
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

func buildMainInitializationOverride(end []int, quantityBox []int, quantityFilter *quantityFilterParam, quantityOnlyRec bool) map[string]any {
	quantityParam := map[string]any{
		"roi":      append([]int(nil), quantityBox...),
		"only_rec": quantityOnlyRec,
	}

	override := map[string]any{
		nodeQuantizedSlidingSwipeToMax: map[string]any{
			"action": map[string]any{
				"param": map[string]any{
					"end": append([]int(nil), end...),
				},
			},
		},
		nodeQuantizedSlidingGetQuantity: map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"roi":      quantityParam["roi"],
					"only_rec": quantityParam["only_rec"],
				},
			},
		},
	}

	if quantityFilter == nil {
		return override
	}

	quantityParam["color_filter"] = nodeQuantizedSlidingQuantityFilter
	override[nodeQuantizedSlidingGetQuantity] = map[string]any{
		"recognition": map[string]any{
			"param": quantityParam,
		},
	}
	override[nodeQuantizedSlidingQuantityFilter] = map[string]any{
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

func buildCheckQuantityBranchOverride(nextNode string, target buttonTarget, repeat int, greenMask bool) map[string]any {
	override := map[string]any{
		nodeQuantizedSlidingDone: map[string]any{
			"enabled": nextNode == nodeQuantizedSlidingDone,
		},
		nodeQuantizedSlidingIncreaseQuantity: map[string]any{
			"enabled": nextNode == nodeQuantizedSlidingIncreaseQuantity,
		},
		nodeQuantizedSlidingDecreaseQuantity: map[string]any{
			"enabled": nextNode == nodeQuantizedSlidingDecreaseQuantity,
		},
	}

	if nextNode != nodeQuantizedSlidingIncreaseQuantity && nextNode != nodeQuantizedSlidingDecreaseQuantity {
		return override
	}

	repeat = clampClickRepeat(repeat)

	if target.template != "" {
		helperNode := resolveButtonHelperNode(nextNode)
		override[helperNode] = buildTemplateMatchButtonHelperOverride(target.template, greenMask)
		override[nextNode] = buildTemplateMatchButtonOverride(helperNode, repeat)
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

func overrideCheckQuantityBranch(ctx *maa.Context, currentNode string, nextNode string, target buttonTarget, repeat int, greenMask bool) error {
	if err := ctx.OverridePipeline(buildCheckQuantityBranchOverride(nextNode, target, repeat, greenMask)); err != nil {
		return fmt.Errorf("%w: %w", errCheckQuantityBranchPipelineOverride, err)
	}
	if err := ctx.OverrideNext(currentNode, []maa.NextItem{{Name: nextNode}}); err != nil {
		return fmt.Errorf("%w: %w", errCheckQuantityBranchNextOverride, err)
	}

	return nil
}

func resolveButtonHelperNode(nextNode string) string {
	switch nextNode {
	case nodeQuantizedSlidingIncreaseQuantity:
		return nodeQuantizedSlidingIncreaseButton
	case nodeQuantizedSlidingDecreaseQuantity:
		return nodeQuantizedSlidingDecreaseButton
	default:
		return ""
	}
}

func buildTemplateMatchButtonHelperOverride(template string, greenMask bool) map[string]any {
	return map[string]any{
		"recognition": map[string]any{
			"param": map[string]any{
				"template":   []string{template},
				"green_mask": greenMask,
			},
		},
	}
}

func buildTemplateMatchButtonOverride(helperNode string, repeat int) map[string]any {
	return map[string]any{
		"enabled": true,
		"recognition": map[string]any{
			"type": "And",
			"param": map[string]any{
				"all_of":    []string{helperNode},
				"box_index": 0,
			},
		},
		"action": map[string]any{
			"type": "Click",
			"param": map[string]any{
				"target":        true,
				"target_offset": []int{5, 5, -10, -10},
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
