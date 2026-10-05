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

// buildCheckQuantityBranchOverride 把按钮补丁与位置筛选一起落到 Helper 节点。
// placement 非空时由 resolveButtonPlacement 补上 roi / order_by / index，
// 让「+」「−」在方向已知时按端点一侧挑选，而不是按分数挑。
func buildCheckQuantityBranchOverride(nextNode string, target buttonTarget, repeat int, placement *buttonPlacement) map[string]any {
	if nextNode != nodeBetterSlidingIncreaseQuantity && nextNode != nodeBetterSlidingDecreaseQuantity {
		return map[string]any{}
	}

	override := map[string]any{}

	repeat = clampClickRepeat(repeat)

	if len(target.patch) > 0 {
		helperNode := resolveButtonHelperNode(nextNode)
		override[helperNode] = buildTemplateMatchButtonHelperOverride(target.patch, placement)
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

// buildTemplateMatchButtonHelperOverride 把按钮识别参数补丁落到 Helper 节点。
// green_mask 由调用方在 normalize 阶段注入，这里只透传补丁本身。
// placement 非空时补上补丁未声明的 roi / order_by / index，把匹配限定在滑条所在行（列）
// 的端点一侧：移动端「+」「−」都是白色圆形按钮，只差中间一笔，按分数常把「−」当成「+」。
// 补丁里显式写死的字段以调用方配置为准，不覆盖。
func buildTemplateMatchButtonHelperOverride(patch map[string]any, placement *buttonPlacement) map[string]any {
	param := make(map[string]any, len(patch)+3)
	for key, value := range patch {
		param[key] = value
	}

	if placement != nil {
		if _, has := param["roi"]; !has {
			param["roi"] = append([]int(nil), placement.roi...)
		}
		if _, has := param["order_by"]; !has {
			param["order_by"] = placement.orderBy
		}
		if _, has := param["index"]; !has {
			param["index"] = placement.index
		}
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
