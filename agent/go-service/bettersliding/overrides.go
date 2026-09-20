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

// buildReset2SwipeEnd returns the swipe end rect for the BetterSlidingReset2 node.
// The reset moves the slider to the opposite side of the precise click: when the click
// sits near Start the Reset2 swipe ends at the maximum side, and vice versa.
func buildReset2SwipeEnd(direction string, side reset2Side) ([]int, error) {
	if side == reset2SideTowardEnd {
		return buildSwipeEnd(direction)
	}

	return buildResetSwipeEnd(direction)
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

// buildMainInitializationOverride 把参数补丁落到各自 Helper 节点：
// 补丁只写 recognition.param，不写 type，框架按「同类型继承」保留原节点类型与未提及字段。
// 空补丁表示该参数未配置，不产生 override。
func buildMainInitializationOverride(
	end []int,
	swipeButtonPatch map[string]any,
	sliderQuantityPatch map[string]any,
	sliderQuantityFilterPatch map[string]any,
	availableQuantityPatch map[string]any,
	availableQuantityFilterPatch map[string]any,
	availableQuantityExplicit bool,
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

	if len(swipeButtonPatch) > 0 {
		override[nodeBetterSlidingSwipeButton] = buildRecognitionParamOverride(swipeButtonPatch)
	}

	if len(sliderQuantityFilterPatch) > 0 {
		override[nodeBetterSlidingSliderQuantityFilter] = buildRecognitionParamOverride(sliderQuantityFilterPatch)
	}

	if len(sliderQuantityPatch) > 0 {
		override[nodeBetterSlidingGetSliderQuantity] = buildRecognitionParamOverride(sliderQuantityPatch)
	}

	if len(availableQuantityFilterPatch) > 0 {
		override[nodeBetterSlidingAvailableQuantityFilter] = buildRecognitionParamOverride(availableQuantityFilterPatch)
	}

	if availableQuantityExplicit {
		override[nodeBetterSlidingGetAvailableQuantity] = map[string]any{
			"enabled":     true,
			"recognition": map[string]any{"param": availableQuantityPatch},
		}
	} else {
		override[nodeBetterSlidingGetAvailableQuantity] = map[string]any{
			"enabled": false,
		}
	}

	return override
}

func buildRecognitionParamOverride(patch map[string]any) map[string]any {
	return map[string]any{
		"recognition": map[string]any{
			"param": patch,
		},
	}
}

func buildCheckQuantityBranchOverride(nextNode string, target buttonTarget, repeat int) map[string]any {
	if nextNode != nodeBetterSlidingIncreaseQuantity && nextNode != nodeBetterSlidingDecreaseQuantity {
		return map[string]any{}
	}

	override := map[string]any{}

	repeat = clampClickRepeat(repeat)

	if len(target.patch) > 0 {
		helperNode := resolveButtonHelperNode(nextNode)
		override[helperNode] = buildTemplateMatchButtonHelperOverride(target.patch)
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

func overrideCheckQuantityBranch(ctx *maa.Context, currentNode string, nextNode string, target buttonTarget, repeat int) error {
	if override := buildCheckQuantityBranchOverride(nextNode, target, repeat); len(override) > 0 {
		if err := ctx.OverridePipeline(override); err != nil {
			return fmt.Errorf("%w: %w", errCheckQuantityBranchPipelineOverride, err)
		}
	}
	if err := ctx.OverrideNext(currentNode, []maa.NextItem{{Name: nextNode}}); err != nil {
		return fmt.Errorf("%w: %w", errCheckQuantityBranchNextOverride, err)
	}

	return nil
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

// buildTemplateMatchButtonHelperOverride 把按钮识别参数补丁落到 Helper 节点。
// green_mask 由调用方在 normalize 阶段注入，这里只透传补丁本身。
func buildTemplateMatchButtonHelperOverride(patch map[string]any) map[string]any {
	return map[string]any{
		"recognition": map[string]any{
			"param": patch,
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
