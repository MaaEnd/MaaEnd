package bettersliding

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

// buildResetSwipeEnd returns the minimum-side end coordinate for the reset swipe.
func buildResetSwipeEnd(direction string) ([]int, error) {
	switch direction {
	case "right", "up":
		return []int{10, 700, 10, 10}, nil
	case "left", "down":
		return []int{1260, 10, 10, 10}, nil
	default:
		return nil, fmt.Errorf("unsupported direction %q", direction)
	}
}

// buildResetSwipeOverride builds the pipeline override for the reset flow.
// The BetterSlidingFindSwipeForReset gate controls whether the reset swipe runs;
// BetterSlidingReset itself only gets its end overridden in the same multi-segment
// style as BetterSlidingSwipeToMax, keeping the pipeline-defined begin anchored at
// the just-recognized slider position.
func buildResetSwipeOverride(direction string, enabled bool) (map[string]any, error) {
	end, err := buildResetSwipeEnd(direction)
	if err != nil {
		return nil, err
	}

	return map[string]any{
		nodeBetterSlidingFindSwipeForReset: map[string]any{
			"enabled": enabled,
		},
		nodeBetterSlidingReset: map[string]any{
			"action": map[string]any{
				"param": map[string]any{
					"end": []any{
						nodeBetterSlidingFindSwipeForReset,
						append([]int(nil), end...),
					},
				},
			},
		},
	}, nil
}

func buildMainInitializationOverride(
	end []int,
	sliderQuantityBox []int,
	availableQuantityBox []int,
	availableQuantityExplicit bool,
	sliderQuantityFilter *quantityFilterParam,
	availableQuantityFilter *quantityFilterParam,
	sliderQuantityOnlyRec bool,
	availableQuantityOnlyRec bool,
	swipeButton string,
	greenMask bool,
) map[string]any {
	override := map[string]any{
		nodeBetterSlidingSwipeToMax: map[string]any{
			"action": map[string]any{
				"param": map[string]any{
					"end": []any{
						nodeBetterSlidingFindStart,
						append([]int(nil), end...),
					},
				},
			},
		},
	}

	if swipeButton != "" {
		override[nodeBetterSlidingSwipeButton] = map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"template":   []string{swipeButton},
					"green_mask": greenMask,
				},
			},
		}
	}

	if len(sliderQuantityBox) == 0 {
		return override
	}

	sliderQuantityParam := map[string]any{
		"roi":      append([]int(nil), sliderQuantityBox...),
		"only_rec": sliderQuantityOnlyRec,
	}
	if sliderQuantityFilter != nil {
		sliderQuantityParam["color_filter"] = nodeBetterSlidingSliderQuantityFilter
		override[nodeBetterSlidingSliderQuantityFilter] = map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"method": sliderQuantityFilter.Method,
					"lower":  [][]int{append([]int(nil), sliderQuantityFilter.Lower...)},
					"upper":  [][]int{append([]int(nil), sliderQuantityFilter.Upper...)},
				},
			},
		}
	}

	override[nodeBetterSlidingGetSliderQuantity] = map[string]any{
		"recognition": map[string]any{
			"param": sliderQuantityParam,
		},
	}

	if availableQuantityExplicit {
		availableQuantityParam := map[string]any{
			"roi":      append([]int(nil), availableQuantityBox...),
			"only_rec": availableQuantityOnlyRec,
		}
		if availableQuantityFilter != nil {
			availableQuantityParam["color_filter"] = nodeBetterSlidingAvailableQuantityFilter
			override[nodeBetterSlidingAvailableQuantityFilter] = map[string]any{
				"recognition": map[string]any{
					"param": map[string]any{
						"method": availableQuantityFilter.Method,
						"lower":  [][]int{append([]int(nil), availableQuantityFilter.Lower...)},
						"upper":  [][]int{append([]int(nil), availableQuantityFilter.Upper...)},
					},
				},
			}
		}
		override[nodeBetterSlidingGetAvailableQuantity] = map[string]any{
			"enabled": true,
			"recognition": map[string]any{
				"param": availableQuantityParam,
			},
		}
	} else {
		override[nodeBetterSlidingGetAvailableQuantity] = map[string]any{
			"enabled": false,
		}
	}

	return override
}

func buildCheckQuantityBranchOverride(nextNode string, target buttonTarget, repeat int, greenMask bool) map[string]any {
	if nextNode != nodeBetterSlidingIncreaseQuantity && nextNode != nodeBetterSlidingDecreaseQuantity {
		return map[string]any{}
	}

	override := map[string]any{}

	repeat = clampClickRepeat(repeat)

	if target.template != "" {
		helperNode := resolveButtonHelperNode(nextNode)
		override[helperNode] = buildTemplateMatchButtonHelperOverride(target.template, greenMask)
		override[nextNode] = buildTemplateMatchButtonOverride(helperNode, repeat)
		return override
	}

	override[nextNode] = map[string]any{
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
	if override := buildCheckQuantityBranchOverride(nextNode, target, repeat, greenMask); len(override) > 0 {
		if err := ctx.OverridePipeline(override); err != nil {
			return fmt.Errorf("%w: %w", errCheckQuantityBranchPipelineOverride, err)
		}
	}
	if err := ctx.OverrideNext(currentNode, buildCheckQuantityBranchNextItems(nextNode)); err != nil {
		return fmt.Errorf("%w: %w", errCheckQuantityBranchNextOverride, err)
	}

	return nil
}

func buildCheckQuantityBranchNextItems(nextNode string) []maa.NextItem {
	nextItems := []maa.NextItem{{Name: nextNode}}
	if nextNode != nodeBetterSlidingIncreaseQuantity && nextNode != nodeBetterSlidingDecreaseQuantity {
		return nextItems
	}

	return append(nextItems, maa.NextItem{Name: nodeBetterSlidingJumpBackMoveMouse})
}

func resolveButtonHelperNode(nextNode string) string {
	switch nextNode {
	case nodeBetterSlidingIncreaseQuantity:
		return nodeBetterSlidingIncreaseButton
	case nodeBetterSlidingDecreaseQuantity:
		return nodeBetterSlidingDecreaseButton
	default:
		return ""
	}
}

func buildNodeEnableOverride(nodeName string, enabled bool) map[string]any {
	return map[string]any{
		nodeName: map[string]any{
			"enabled": enabled,
		},
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

	override := make(map[string]any, len(betterSlidingActionNodes))
	for _, nodeName := range betterSlidingActionNodes {
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
