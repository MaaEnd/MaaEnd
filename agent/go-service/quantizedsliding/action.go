package quantizedsliding

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type quantizedSlidingParam struct {
	Target         int    `json:"Target"`
	QuantityBox    []int  `json:"QuantityBox"`
	Direction      string `json:"Direction"`
	IncreaseButton any    `json:"IncreaseButton"`
	DecreaseButton any    `json:"DecreaseButton"`
}

// QuantizedSlidingAction 实现量化滑动选择功能,用于处理游戏中需要通过滑动选择数量的 UI 场景。
// 该动作会自动识别滑动条的起点和终点位置,根据目标数量精确计算点击位置,
// 并通过微调按钮进行最终调整以达到目标值。
//
// 参数说明:
//   - Target: 目标数量
//   - QuantityBox: OCR 识别数量的 ROI 区域 [x,y,w,h]
//   - Direction: 滑动方向 (left/right/up/down)
//   - IncreaseButton: 增加数量按钮的坐标
//   - DecreaseButton: 减少数量按钮的坐标
type QuantizedSlidingAction struct {
	Target         int
	QuantityBox    []int
	Direction      string
	IncreaseButton buttonTarget
	DecreaseButton buttonTarget

	startBox    []int
	endBox      []int
	maxQuantity int
	logger      zerolog.Logger
}

type buttonTarget struct {
	coordinates []int
	template    string
}

func (b buttonTarget) logValue() any {
	if b.template != "" {
		return b.template
	}

	return append([]int(nil), b.coordinates...)
}

var quantizedSlidingActionNodes = []string{
	"QuantizedSlidingMain",
	"QuantizedSlidingFindStart",
	"QuantizedSlidingGetMaxQuantity",
	"QuantizedSlidingFindEnd",
	"QuantizedSlidingCheckQuantity",
	"QuantizedSlidingDone",
}

const maxClickRepeat = 30

// centerPointOffset 用于微调点击位置
var centerPointOffset = [2]int{-10, 0}

var _ maa.CustomActionRunner = &QuantizedSlidingAction{}

func (a *QuantizedSlidingAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", "QuantizedSliding").
			Msg("got nil custom action arg")
		return false
	}

	a.logger = log.With().
		Str("component", "QuantizedSliding").
		Str("task", arg.CurrentTaskName).
		Logger()

	if !isQuantizedSlidingActionNode(arg.CurrentTaskName) {
		return a.runInternalPipeline(ctx, arg)
	}

	var params quantizedSlidingParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		a.logger.Error().
			Err(err).
			Str("param", arg.CustomActionParam).
			Msg("failed to parse custom_action_param")
		return false
	}

	increaseButton, err := normalizeButtonParam(params.IncreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize increase button")
		return false
	}

	decreaseButton, err := normalizeButtonParam(params.DecreaseButton)
	if err != nil {
		a.logger.Error().
			Err(err).
			Msg("failed to normalize decrease button")
		return false
	}

	a.Target = params.Target
	a.QuantityBox = append([]int(nil), params.QuantityBox...)
	a.Direction = strings.ToLower(strings.TrimSpace(params.Direction))
	a.IncreaseButton = increaseButton
	a.DecreaseButton = decreaseButton

	a.logger.Info().
		Int("target", a.Target).
		Ints("quantity_box", a.QuantityBox).
		Str("direction", a.Direction).
		Interface("increase_button", a.IncreaseButton.logValue()).
		Interface("decrease_button", a.DecreaseButton.logValue()).
		Msg("parsed custom action parameters")

	switch arg.CurrentTaskName {
	case "QuantizedSlidingMain":
		return a.handleMain(ctx, arg)
	case "QuantizedSlidingFindStart":
		return a.handleFindStart(ctx, arg)
	case "QuantizedSlidingGetMaxQuantity":
		return a.handleGetMaxQuantity(ctx, arg)
	case "QuantizedSlidingFindEnd":
		return a.handleFindEnd(ctx, arg)
	case "QuantizedSlidingCheckQuantity":
		return a.handleCheckQuantity(ctx, arg)
	case "QuantizedSlidingDone":
		return a.handleDone(ctx, arg)
	default:
		a.logger.Warn().Msg("unknown current task name")
		return false
	}
}

func (a *QuantizedSlidingAction) handleMain(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	a.resetState()

	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}

	if len(a.QuantityBox) != 4 {
		a.logger.Error().
			Ints("quantity_box", a.QuantityBox).
			Msg("invalid quantity box, expected [x,y,w,h]")
		return false
	}

	end, err := buildSwipeEnd(a.Direction)
	if err != nil {
		a.logger.Error().
			Str("direction", a.Direction).
			Err(err).
			Msg("invalid direction")
		return false
	}

	override := buildMainInitializationOverride(end, a.QuantityBox)

	if err := ctx.OverridePipeline(override); err != nil {
		a.logger.Error().Err(err).Msg("failed to override pipeline for main initialization")
		return false
	}

	a.logger.Info().
		Str("direction", a.Direction).
		Ints("end", end).
		Ints("quantity_roi", a.QuantityBox).
		Msg("main initialization completed with pipeline overrides")
	return true
}

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

func buildMainInitializationOverride(end []int, quantityBox []int) map[string]any {
	return map[string]any{
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
					"roi": append([]int(nil), quantityBox...),
				},
			},
		},
	}
}

func (a *QuantizedSlidingAction) handleFindStart(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil || arg.RecognitionDetail == nil {
		a.logger.Error().Msg("recognition detail is nil")
		return false
	}

	box, ok := extractHitBox(arg.RecognitionDetail)
	if !ok {
		a.logger.Error().Msg("failed to extract start box from recognition detail")
		return false
	}

	a.startBox = box
	a.logger.Info().Ints("start_box", a.startBox).Msg("start box recorded")
	return true
}

func (a *QuantizedSlidingAction) handleGetMaxQuantity(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		a.logger.Error().Msg("custom action arg is nil")
		return false
	}

	maxQuantity, err := parseOCRText(arg.RecognitionDetail)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to parse max quantity from ocr")
		return false
	}

	a.maxQuantity = maxQuantity
	if a.maxQuantity < a.Target {
		a.logger.Error().
			Int("max_quantity", a.maxQuantity).
			Int("target", a.Target).
			Msg("max quantity lower than target")
		return false
	}

	a.logger.Info().
		Int("max_quantity", a.maxQuantity).
		Int("target", a.Target).
		Msg("max quantity parsed")
	return true
}

func extractHitBox(recognitionDetail *maa.RecognitionDetail) ([]int, bool) {
	if recognitionDetail == nil {
		return nil, false
	}

	if len(recognitionDetail.Box) >= 4 {
		return []int{recognitionDetail.Box[0], recognitionDetail.Box[1], recognitionDetail.Box[2], recognitionDetail.Box[3]}, true
	}

	if recognitionDetail.Results == nil {
		return nil, false
	}

	if recognitionDetail.Results.Best != nil {
		if tm, ok := recognitionDetail.Results.Best.AsTemplateMatch(); ok {
			return []int{tm.Box.X(), tm.Box.Y(), tm.Box.Width(), tm.Box.Height()}, true
		}
		if ocr, ok := recognitionDetail.Results.Best.AsOCR(); ok {
			return []int{ocr.Box.X(), ocr.Box.Y(), ocr.Box.Width(), ocr.Box.Height()}, true
		}
	}

	for _, result := range recognitionDetail.Results.Filtered {
		if tm, ok := result.AsTemplateMatch(); ok {
			return []int{tm.Box.X(), tm.Box.Y(), tm.Box.Width(), tm.Box.Height()}, true
		}
		if ocr, ok := result.AsOCR(); ok {
			return []int{ocr.Box.X(), ocr.Box.Y(), ocr.Box.Width(), ocr.Box.Height()}, true
		}
	}

	for _, result := range recognitionDetail.Results.All {
		if tm, ok := result.AsTemplateMatch(); ok {
			return []int{tm.Box.X(), tm.Box.Y(), tm.Box.Width(), tm.Box.Height()}, true
		}
		if ocr, ok := result.AsOCR(); ok {
			return []int{ocr.Box.X(), ocr.Box.Y(), ocr.Box.Width(), ocr.Box.Height()}, true
		}
	}

	return nil, false
}

func (a *QuantizedSlidingAction) handleFindEnd(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}
	if arg == nil || arg.RecognitionDetail == nil {
		a.logger.Error().Msg("recognition detail is nil")
		return false
	}
	if a.maxQuantity <= 1 {
		a.logger.Error().
			Int("max_quantity", a.maxQuantity).
			Msg("invalid max quantity for precise click calculation")
		return false
	}

	endBox, ok := extractHitBox(arg.RecognitionDetail)
	if !ok {
		a.logger.Error().Msg("failed to extract end box from recognition detail")
		return false
	}
	a.endBox = endBox

	if len(a.startBox) < 4 {
		a.logger.Error().
			Ints("start_box", a.startBox).
			Msg("start box is invalid")
		return false
	}
	if len(a.endBox) < 4 {
		a.logger.Error().
			Ints("end_box", a.endBox).
			Msg("end box is invalid")
		return false
	}

	startX, startY := centerPoint(a.startBox)
	endX, endY := centerPoint(a.endBox)

	numerator := a.Target - 1
	denominator := a.maxQuantity - 1
	if denominator == 0 {
		a.logger.Error().
			Int("max_quantity", a.maxQuantity).
			Msg("denominator is zero in precise click calculation")
		return false
	}

	clickX := startX + (endX-startX)*numerator/denominator
	clickY := startY + (endY-startY)*numerator/denominator

	if err := ctx.OverridePipeline(map[string]any{
		"QuantizedSlidingPreciseClick": map[string]any{
			"action": map[string]any{
				"param": map[string]any{
					"target": []int{clickX, clickY},
				},
			},
		},
	}); err != nil {
		a.logger.Error().Err(err).Msg("failed to override precise click target")
		return false
	}

	a.logger.Info().
		Ints("start_box", a.startBox).
		Ints("end_box", a.endBox).
		Int("target", a.Target).
		Int("max_quantity", a.maxQuantity).
		Int("click_x", clickX).
		Int("click_y", clickY).
		Msg("precise click calculated")
	return true
}

func (a *QuantizedSlidingAction) handleCheckQuantity(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}

	if arg == nil {
		a.logger.Error().Msg("custom action arg is nil")
		return false
	}

	currentQuantity, err := parseOCRText(arg.RecognitionDetail)
	if err != nil {
		a.logger.Error().Err(err).Msg("failed to parse current quantity from ocr")
		return false
	}

	switch {
	case currentQuantity == a.Target:
		if err := ctx.OverridePipeline(buildCheckQuantityBranchOverride("QuantizedSlidingDone", buttonTarget{}, 0)); err != nil {
			a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target).
				Msg("failed to override done node")
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Str("next", "QuantizedSlidingDone").
			Msg("quantity matched target")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "QuantizedSlidingDone"}}); err != nil {
			a.logger.Error().Err(err).Msg("failed to override next to done")
			return false
		}
		return true
	case currentQuantity < a.Target:
		diff := a.Target - currentQuantity
		repeat := clampClickRepeat(diff)
		if err := ctx.OverridePipeline(buildCheckQuantityBranchOverride("QuantizedSlidingIncreaseQuantity", a.IncreaseButton, repeat)); err != nil {
			a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target).
				Int("diff", diff).
				Int("repeat", repeat).
				Interface("increase_button", a.IncreaseButton.logValue()).
				Msg("failed to override increase quantity node")
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Int("diff", diff).
			Int("repeat", repeat).
			Interface("button", a.IncreaseButton.logValue()).
			Str("next", "QuantizedSlidingIncreaseQuantity").
			Msg("quantity below target, branch to increase")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "QuantizedSlidingIncreaseQuantity"}}); err != nil {
			a.logger.Error().Err(err).Msg("failed to override next to increase quantity")
			return false
		}
		return true
	default:
		diff := currentQuantity - a.Target
		repeat := clampClickRepeat(diff)
		if err := ctx.OverridePipeline(buildCheckQuantityBranchOverride("QuantizedSlidingDecreaseQuantity", a.DecreaseButton, repeat)); err != nil {
			a.logger.Error().
				Err(err).
				Int("current_quantity", currentQuantity).
				Int("target", a.Target).
				Int("diff", diff).
				Int("repeat", repeat).
				Interface("decrease_button", a.DecreaseButton.logValue()).
				Msg("failed to override decrease quantity node")
			return false
		}

		a.logger.Info().
			Int("current_quantity", currentQuantity).
			Int("target", a.Target).
			Int("diff", diff).
			Int("repeat", repeat).
			Interface("button", a.DecreaseButton.logValue()).
			Str("next", "QuantizedSlidingDecreaseQuantity").
			Msg("quantity above target, branch to decrease")
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "QuantizedSlidingDecreaseQuantity"}}); err != nil {
			a.logger.Error().Err(err).Msg("failed to override next to decrease quantity")
			return false
		}
		return true
	}
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

func clampClickRepeat(repeat int) int {
	if repeat < 0 {
		return 0
	}
	if repeat > maxClickRepeat {
		return maxClickRepeat
	}

	return repeat
}

func (a *QuantizedSlidingAction) handleDone(_ *maa.Context, _ *maa.CustomActionArg) bool {
	a.logger.Info().Msg("quantity adjustment completed")
	return true
}

func isQuantizedSlidingActionNode(taskName string) bool {
	for _, nodeName := range quantizedSlidingActionNodes {
		if taskName == nodeName {
			return true
		}
	}

	return false
}

func (a *QuantizedSlidingAction) runInternalPipeline(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil {
		a.logger.Error().Msg("context is nil")
		return false
	}

	override, err := buildInternalPipelineOverride(arg.CustomActionParam)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("caller", arg.CurrentTaskName).
			Msg("failed to build internal quantized sliding pipeline override")
		return false
	}

	detail, err := ctx.RunTask("QuantizedSlidingMain", override)
	if err != nil {
		a.logger.Error().
			Err(err).
			Str("caller", arg.CurrentTaskName).
			Msg("failed to run internal quantized sliding pipeline")
		return false
	}
	if detail == nil {
		a.logger.Error().
			Str("caller", arg.CurrentTaskName).
			Msg("internal quantized sliding pipeline returned nil detail")
		return false
	}
	if !detail.Status.Success() {
		a.logger.Error().
			Str("caller", arg.CurrentTaskName).
			Int64("subtask_id", detail.ID).
			Str("subtask_status", detail.Status.String()).
			Msg("internal quantized sliding pipeline failed")
		return false
	}

	a.logger.Info().
		Str("caller", arg.CurrentTaskName).
		Int64("subtask_id", detail.ID).
		Str("subtask_status", detail.Status.String()).
		Msg("internal quantized sliding pipeline completed")
	return true
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

func centerPoint(rect []int) (int, int) {
	if len(rect) < 4 {
		return 0, 0
	}
	return rect[0] + rect[2]/2 + centerPointOffset[0], rect[1] + rect[3]/2 + centerPointOffset[1]
}

func parseOCRText(recognitionDetail *maa.RecognitionDetail) (int, error) {
	if recognitionDetail == nil {
		return 0, fmt.Errorf("recognition detail is nil")
	}

	text := extractOCRText(recognitionDetail)

	if text == "" {
		return 0, fmt.Errorf("ocr text not found in recognition detail")
	}

	var digits strings.Builder
	for _, r := range text {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	if digits.Len() == 0 {
		return 0, fmt.Errorf("ocr text has no digit: %s", text)
	}

	value, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0, err
	}

	return value, nil
}

func extractOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil {
		return ""
	}

	if text := extractOCRTextFromResults(detail.Results); text != "" {
		return text
	}

	for _, child := range detail.CombinedResult {
		if text := extractOCRText(child); text != "" {
			return text
		}
	}

	return extractOCRTextFromDetailJSON(detail.DetailJson)
}

func extractOCRTextFromResults(results *maa.RecognitionResults) string {
	if results == nil {
		return ""
	}

	for _, group := range [][]*maa.RecognitionResult{{results.Best}, results.Filtered, results.All} {
		for _, result := range group {
			if result == nil {
				continue
			}

			ocrResult, ok := result.AsOCR()
			if !ok {
				continue
			}

			text := strings.TrimSpace(ocrResult.Text)
			if text != "" {
				return text
			}
		}
	}

	return ""
}

func extractOCRTextFromDetailJSON(detailJSON string) string {
	detailJSON = strings.TrimSpace(detailJSON)
	if detailJSON == "" || detailJSON == "null" {
		return ""
	}

	var direct struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
			Text   string          `json:"text"`
		} `json:"best"`
		Detail json.RawMessage `json:"detail"`
		Text   string          `json:"text"`
	}
	if err := json.Unmarshal([]byte(detailJSON), &direct); err == nil {
		if text := strings.TrimSpace(direct.Best.Text); text != "" {
			return text
		}
		if text := strings.TrimSpace(direct.Text); text != "" {
			return text
		}
		if text := extractOCRTextFromRawJSON(direct.Best.Detail); text != "" {
			return text
		}
		if text := extractOCRTextFromRawJSON(direct.Detail); text != "" {
			return text
		}
	}

	var combined struct {
		Detail []struct {
			Detail json.RawMessage `json:"detail"`
			Text   string          `json:"text"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(detailJSON), &combined); err == nil {
		for _, item := range combined.Detail {
			if text := strings.TrimSpace(item.Text); text != "" {
				return text
			}
			if text := extractOCRTextFromRawJSON(item.Detail); text != "" {
				return text
			}
		}
	}

	var combinedArray []struct {
		Detail json.RawMessage `json:"detail"`
		Text   string          `json:"text"`
	}
	if err := json.Unmarshal([]byte(detailJSON), &combinedArray); err == nil {
		for _, item := range combinedArray {
			if text := strings.TrimSpace(item.Text); text != "" {
				return text
			}
			if text := extractOCRTextFromRawJSON(item.Detail); text != "" {
				return text
			}
		}
	}

	return ""
}

func extractOCRTextFromRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var detailString string
	if err := json.Unmarshal(raw, &detailString); err == nil {
		return extractOCRTextFromDetailJSON(detailString)
	}

	return extractOCRTextFromDetailJSON(string(raw))
}

func (a *QuantizedSlidingAction) resetState() {
	a.startBox = nil
	a.endBox = nil
	a.maxQuantity = 0
}
