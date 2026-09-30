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
					"green_mask": defaultGreenMask,
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

// 坐标基准与 Pipeline 一致：720p
const (
	screenWidth  = 1280
	screenHeight = 720
)

// buttonPlacement 把 Increase/Decrease 的模板匹配限定在滑条所在的那一行（列），并按轴向排序后
// 取靠终点（Increase）或靠起点（Decrease）的那一个，不按分数挑：
// 移动端「+」「−」都是白色圆形按钮，只差中间一笔，按分数常把「−」当成「+」
type buttonPlacement struct {
	roi     []int
	orderBy string
	index   int
}

// resolveButtonPlacement 由起点框与滑动方向推出按钮的筛选方式；方向未知或起点缺失时返回 nil，保持按分数匹配。
// 「right」「up」时终点在右 / 上，「left」「down」时在左 / 下（与 buildSwipeEnd 一致）
func resolveButtonPlacement(startBox []int, direction string, increase bool) *buttonPlacement {
	if len(startBox) < 4 {
		return nil
	}
	towardEndIsFirst := false
	horizontal := true
	switch direction {
	case "right":
	case "left":
		towardEndIsFirst = true
	case "up":
		horizontal = false
		towardEndIsFirst = true
	case "down":
		horizontal = false
	default:
		return nil
	}

	pickFirst := towardEndIsFirst == increase
	index := -1
	if pickFirst {
		index = 0
	}
	if horizontal {
		pad := startBox[3]
		return &buttonPlacement{
			roi:     []int{0, startBox[1] - pad, screenWidth, startBox[3] + 2*pad},
			orderBy: "Horizontal",
			index:   index,
		}
	}
	pad := startBox[2]
	return &buttonPlacement{
		roi:     []int{startBox[0] - pad, 0, startBox[2] + 2*pad, screenHeight},
		orderBy: "Vertical",
		index:   index,
	}
}

func buildCheckQuantityBranchOverride(nextNode string, target buttonTarget, repeat int, placement *buttonPlacement) map[string]any {
	if nextNode != nodeBetterSlidingIncreaseQuantity && nextNode != nodeBetterSlidingDecreaseQuantity {
		return map[string]any{}
	}

	override := map[string]any{}

	repeat = clampClickRepeat(repeat)

	if target.template != "" {
		helperNode := resolveButtonHelperNode(nextNode)
		override[helperNode] = buildTemplateMatchButtonHelperOverride(target.template, placement)
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

func overrideCheckQuantityBranch(
	ctx *maa.Context,
	currentNode string,
	nextNode string,
	target buttonTarget,
	repeat int,
	placement *buttonPlacement,
) error {
	if override := buildCheckQuantityBranchOverride(nextNode, target, repeat, placement); len(override) > 0 {
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

func buildTemplateMatchButtonHelperOverride(template string, placement *buttonPlacement) map[string]any {
	param := map[string]any{
		"template":   []string{template},
		"green_mask": defaultGreenMask,
	}
	if placement != nil {
		param["roi"] = append([]int(nil), placement.roi...)
		param["order_by"] = placement.orderBy
		param["index"] = placement.index
	}
	return map[string]any{
		"recognition": map[string]any{
			"param": param,
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
