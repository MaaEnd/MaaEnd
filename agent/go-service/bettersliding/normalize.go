package bettersliding

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
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

// resolveRecognitionParam 把 String|Object 归一为 recognition.param 补丁。
//
//   - String：节点引用，读取该节点的 recognition.param（绝不取 type / recognition）；
//   - Object：直接作为补丁，禁止含 recognition / type / action 键；
//   - nil：返回空补丁（表示该参数未配置）。
//
// 返回值恒为非 nil map，便于直接写入 override。
func resolveRecognitionParam(ctx *maa.Context, raw any) (map[string]any, error) {
	if raw == nil {
		return map[string]any{}, nil
	}

	switch value := raw.(type) {
	case string:
		return resolveRecognitionParamFromNode(ctx, value)

	case map[string]any:
		return validateRecognitionPatch(value)

	default:
		return nil, fmt.Errorf(
			"expected a node reference string or a recognition param object, got %T",
			raw,
		)
	}
}

func resolveRecognitionParamFromNode(ctx *maa.Context, nodeName string) (map[string]any, error) {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		return nil, fmt.Errorf("node reference must not be empty")
	}
	if ctx == nil {
		return nil, fmt.Errorf("context is nil, cannot resolve node reference %q", nodeName)
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil {
		return nil, fmt.Errorf("get node %s json: %w", nodeName, err)
	}
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("node %s json is empty", nodeName)
	}

	return extractRecognitionParam(raw)
}

// forbiddenRecognitionPatchKeys 是补丁中禁止出现的键：它们属于 recognition / action 层级，
// 出现即视为「替换识别类型」的误用，必须显式报错而不是静默忽略。
var forbiddenRecognitionPatchKeys = map[string]struct{}{
	"recognition": {},
	"type":        {},
	"action":      {},
}

func validateRecognitionPatch(patch map[string]any) (map[string]any, error) {
	if patch == nil {
		return map[string]any{}, nil
	}
	for key := range patch {
		if _, forbidden := forbiddenRecognitionPatchKeys[key]; forbidden {
			return nil, fmt.Errorf(
				"recognition param patch must not contain %q, only recognition.param keys are allowed",
				key,
			)
		}
	}

	return patch, nil
}

// nodeLevelJSONKeys 是扁平节点写法中与识别参数无关的节点级字段。
var nodeLevelJSONKeys = map[string]struct{}{
	"recognition":         {},
	"action":              {},
	"next":                {},
	"on_error":            {},
	"anchor":              {},
	"inverse":             {},
	"enabled":             {},
	"max_hit":             {},
	"pre_delay":           {},
	"post_delay":          {},
	"pre_wait_freezes":    {},
	"post_wait_freezes":   {},
	"repeat":              {},
	"repeat_delay":        {},
	"repeat_wait_freezes": {},
	"focus":               {},
	"attach":              {},
	"rate_limit":          {},
	"timeout":             {},
	"desc":                {},
	"box_index":           {},
}

// extractRecognitionParam 从 GetNodeJSON 返回的节点 JSON 中取出 recognition.param。
// 兼容 v2（recognition:{type,param}）与扁平（recognition:"OCR" + 顶层参数）两形态。
func extractRecognitionParam(raw string) (map[string]any, error) {
	var node map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		return nil, fmt.Errorf("unmarshal node json: %w", err)
	}

	recognitionRaw, ok := node["recognition"]
	if !ok || len(recognitionRaw) == 0 || string(recognitionRaw) == "null" {
		return map[string]any{}, nil
	}

	// 扁平写法：recognition 是识别类型字符串，参数平铺在节点顶层。
	var recognitionType string
	if err := json.Unmarshal(recognitionRaw, &recognitionType); err == nil {
		return flatRecognitionParam(node)
	}

	var recognitionObject map[string]json.RawMessage
	if err := json.Unmarshal(recognitionRaw, &recognitionObject); err != nil {
		return nil, fmt.Errorf("unmarshal recognition: %w", err)
	}

	paramRaw, hasParam := recognitionObject["param"]
	if !hasParam || len(paramRaw) == 0 || string(paramRaw) == "null" {
		return map[string]any{}, nil
	}

	patch := map[string]any{}
	if err := json.Unmarshal(paramRaw, &patch); err != nil {
		return nil, fmt.Errorf("unmarshal recognition.param: %w", err)
	}
	if patch == nil {
		patch = map[string]any{}
	}

	return patch, nil
}

func flatRecognitionParam(node map[string]json.RawMessage) (map[string]any, error) {
	patch := make(map[string]any, len(node))
	for key, value := range node {
		if _, isNodeLevel := nodeLevelJSONKeys[key]; isNodeLevel {
			continue
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, fmt.Errorf("unmarshal node key %s: %w", key, err)
		}
		patch[key] = decoded
	}

	return patch, nil
}

// resolveFilterPatch 解析 Filter 参数：返回内建 Filter 节点名（供 color_filter 引用）
// 与要写入该节点的识别参数补丁。未配置时两者皆为空。
func resolveFilterPatch(ctx *maa.Context, raw any, builtin string) (string, map[string]any, error) {
	if raw == nil {
		return "", nil, nil
	}

	patch, err := resolveRecognitionParam(ctx, raw)
	if err != nil {
		return "", nil, err
	}
	if len(patch) == 0 {
		return "", nil, fmt.Errorf("filter recognition param patch is empty")
	}

	return builtin, patch, nil
}

// applyColorFilter 给 Quantity 的 OCR 补丁挂上 color_filter 节点名。
// 补丁自身已声明 color_filter 时保持原值（补丁优先）；未配置 Filter 时不写入。
func applyColorFilter(patch map[string]any, filterNode string) {
	if filterNode == "" || len(patch) == 0 {
		return
	}
	if _, exists := patch["color_filter"]; exists {
		return
	}

	patch["color_filter"] = filterNode
}

// resolveButtonTarget 归一化 IncreaseButton / DecreaseButton：
// 数组为点击坐标（int[2|4]），String|Object 为模板识别补丁（默认 green_mask: true）。
func resolveButtonTarget(ctx *maa.Context, raw any) (buttonTarget, error) {
	if raw == nil {
		return buttonTarget{}, nil
	}

	switch raw.(type) {
	case []any, []int, []float64:
		coordinates, err := normalizeButton(raw)
		if err != nil {
			return buttonTarget{}, err
		}

		return buttonTarget{coordinates: coordinates}, nil
	}

	patch, err := resolveRecognitionParam(ctx, raw)
	if err != nil {
		return buttonTarget{}, err
	}
	if len(patch) == 0 {
		return buttonTarget{}, fmt.Errorf("button recognition param patch is empty")
	}
	if _, has := patch["green_mask"]; !has {
		patch["green_mask"] = defaultGreenMask
	}

	return buttonTarget{patch: patch}, nil
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
		return nil, fmt.Errorf("unsupported int slice type %T", raw)
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

// isMinimumTargetShortCircuit 判断本次是否走「目标即滑条最小值 1」的短路路径。
// 仅在 TargetQuantityType 为 Value、原始目标为 1 且未启用 ReverseTarget 时成立：
// Percentage 模式与 ReverseTarget 的有效目标依赖运行时 availableQuantity，无法在进入流程前判定。
func isMinimumTargetShortCircuit(targetQuantity int, targetQuantityType string, reverseTarget bool) bool {
	return targetQuantity == 1 &&
		targetQuantityType == TargetQuantityTypeValue &&
		!reverseTarget
}

// minimumTargetShortCircuitNext 返回短路时被覆写 next 的节点：
// ResetBeforeFindStart 时在复位滑动完成后收尾（覆写 BetterSlidingReset.next），
// 否则在清空命中计数后直接收尾（覆写 BetterSlidingClearMaxHit.next）。
func minimumTargetShortCircuitNext(resetBeforeFindStart bool) string {
	if resetBeforeFindStart {
		return nodeBetterSlidingReset
	}

	return nodeBetterSlidingClearMaxHit
}

// normalizeFineTuneQuantity 归一化 FineTuneQuantity：
//
//	未提供（present=false，含显式 null）-> 默认 enabled（始终微调）；
//	bool                               -> 布尔语义；
//	整数值                             -> 阈值语义，阈值须 >= 1；超过 maxFineTuneThreshold
//	                                      时饱和为 enabled（始终微调）。
//
// 非整数、其他类型与 < 1 的值均返回错误。
//
// present 由调用方（hasNonNullRawKey）判定：显式 null 与键缺失一样视为「未提供」，
// 因此这里不再单独区分 null。
func normalizeFineTuneQuantity(raw any, present bool) (fineTuneQuantity, error) {
	if !present {
		return defaultFineTuneQuantity, nil
	}

	switch v := raw.(type) {
	case bool:
		return fineTuneQuantity{enabled: v}, nil
	case float64:
		if v != math.Trunc(v) {
			return fineTuneQuantity{}, fmt.Errorf("FineTuneQuantity must be a bool or an integer, got %v", v)
		}

		return newThresholdFineTuneQuantity(v)
	default:
		return fineTuneQuantity{}, fmt.Errorf(
			"FineTuneQuantity must be a bool or an integer >= 1, got %T",
			raw,
		)
	}
}

func newThresholdFineTuneQuantity(v float64) (fineTuneQuantity, error) {
	if v < 1 {
		return fineTuneQuantity{}, fmt.Errorf("FineTuneQuantity threshold must be >= 1, got %v", v)
	}
	// 阈值超过 maxFineTuneThreshold 时已超出 int 可表示范围，且远大于任何真实数量差值，
	// 语义上等价于「始终微调」：饱和为 enabled，不截断、不报错（文档只约束 N >= 1）。
	if v > maxFineTuneThreshold {
		log.Warn().
			Float64("fine_tune_quantity", v).
			Int("max_fine_tune_threshold", maxFineTuneThreshold).
			Msg("FineTuneQuantity threshold above max, treated as always fine-tune")

		return fineTuneQuantity{enabled: true}, nil
	}

	return fineTuneQuantity{thresholdMode: true, threshold: int(v)}, nil
}

// normalizeFineTuneFallback 归一化 FineTuneFallback：空串或 null（JSON null 解析为空串）
// 归一为 none；大小写不敏感地接受 none / more / less 并返回小写规范值；其他值返回错误。
func normalizeFineTuneFallback(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return FineTuneFallbackNone, nil
	}

	switch strings.ToLower(s) {
	case FineTuneFallbackNone:
		return FineTuneFallbackNone, nil
	case FineTuneFallbackMore:
		return FineTuneFallbackMore, nil
	case FineTuneFallbackLess:
		return FineTuneFallbackLess, nil
	default:
		return "", fmt.Errorf(
			"invalid FineTuneFallback %q, expected %q, %q or %q",
			raw,
			FineTuneFallbackNone,
			FineTuneFallbackMore,
			FineTuneFallbackLess,
		)
	}
}

func isSwipeOnlyMode(params betterSlidingParam) bool {
	return !params.presence.TargetQuantity &&
		!params.presence.SliderQuantity &&
		!params.presence.SliderQuantityFilter &&
		!params.presence.AvailableQuantity &&
		!params.presence.AvailableQuantityFilter &&
		!params.presence.IncreaseButton &&
		!params.presence.DecreaseButton &&
		!params.presence.OutOfRangeOverrideEnable &&
		!params.presence.TargetReachableOverrideEnable &&
		!params.presence.TargetQuantityType &&
		!params.presence.ReverseTarget &&
		!params.presence.CenterPointOffset &&
		!params.presence.ClampTargetToSliderMax &&
		!params.presence.FineTuneQuantity &&
		!params.presence.FineTuneFallback
}
