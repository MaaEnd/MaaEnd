package listcomplete

import (
	"encoding/json"
	"fmt"
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	scrollbarRecognitionComponentName = "ScrollbarRecognition"
	scrollbarCompleteComponentName    = "ScrollbarCompleteRecognition"

	// defaultPositionTolerance 是上下边界允许的最大位置偏差，单位为 720p 像素。
	// 调大可容忍抗锯齿和轻微抖动，但也会增加将短距离滚动误判为未移动的风险。
	defaultPositionTolerance = 2

	// scrollbarWhiteThreshold 是单个颜色通道视为白色的最低值，按测试素材标定。
	// 调低会提高半透明白条召回率，也更容易把浅色背景识别为滚动条。
	scrollbarWhiteThreshold = 200

	// scrollbarMaxGap 是白条内部允许填补的最大纵向断点，单位为 720p 像素。
	// 调大可连接更严重的透明纹理断点，也可能合并两段相邻高亮区域。
	scrollbarMaxGap = 2

	// scrollbarMinLength 是有效白条的最短纵向长度，单位为 720p 像素。
	// 调小可识别更短的滑块，也会增加孤立高亮噪声造成的误检。
	scrollbarMinLength = 5

	attachScrollbarTop     = "scrollbar_top"
	attachScrollbarBottom  = "scrollbar_bottom"
	attachScrollbarMissing = "scrollbar_missing"
)

// ScrollbarRecognition 识别滚动条白色滑块，并返回其绝对位置和相对 ROI 的边界信息。
type ScrollbarRecognition struct{}

var _ maa.CustomRecognitionRunner = &ScrollbarRecognition{}

// Run implements maa.CustomRecognitionRunner.
func (r *ScrollbarRecognition) Run(
	_ *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().Str("component", scrollbarRecognitionComponentName).Msg("nil arg or image")
		return nil, false
	}

	roi, err := resolveROI(arg.Img, arg.Roi)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", scrollbarRecognitionComponentName).
			Ints("roi", []int{arg.Roi[0], arg.Roi[1], arg.Roi[2], arg.Roi[3]}).
			Msg("invalid roi")
		return nil, false
	}

	segment, found := detectScrollbarSegment(
		arg.Img,
		image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3]),
	)
	if !found {
		return nil, false
	}

	length := segment.Bottom - segment.Top + 1
	return &maa.CustomRecognitionResult{
		Box: maa.Rect{roi[0], roi[1] + segment.Top, roi[2], length},
		Detail: marshalDetail(map[string]any{
			"top":    segment.Top,
			"bottom": segment.Bottom,
			"length": length,
		}),
	}, true
}

// ScrollbarCompleteRecognition 比较连续两次滑块位置，判断列表是否已无法继续滚动。
type ScrollbarCompleteRecognition struct{}

var _ maa.CustomRecognitionRunner = &ScrollbarCompleteRecognition{}

type scrollbarParams struct {
	// PositionTolerance 是上下边界允许的最大位置偏差，单位为 720p 像素。
	PositionTolerance int `json:"position_tolerance"`
}

type scrollbarSegment struct {
	Top    int
	Bottom int
}

// Run implements maa.CustomRecognitionRunner.
func (r *ScrollbarCompleteRecognition) Run(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		log.Error().Str("component", scrollbarCompleteComponentName).Msg("nil context, arg or image")
		return nil, false
	}

	params, err := parseScrollbarParams(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", scrollbarCompleteComponentName).
			Str("custom_recognition_param", arg.CustomRecognitionParam).
			Msg("failed to parse params")
		return nil, false
	}

	currentNode := strings.TrimSpace(arg.CurrentTaskName)
	if currentNode == "" {
		log.Error().Str("component", scrollbarCompleteComponentName).Msg("current task name is empty")
		return nil, false
	}

	roi, err := resolveROI(arg.Img, arg.Roi)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", scrollbarCompleteComponentName).
			Str("node", currentNode).
			Ints("roi", []int{arg.Roi[0], arg.Roi[1], arg.Roi[2], arg.Roi[3]}).
			Msg("invalid roi")
		return nil, false
	}

	segment, found := detectScrollbarSegment(
		arg.Img,
		image.Rect(roi[0], roi[1], roi[0]+roi[2], roi[1]+roi[3]),
	)
	if !found {
		complete, err := observeMissingScrollbar(ctx, currentNode)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", scrollbarCompleteComponentName).
				Str("node", currentNode).
				Msg("failed to record missing scrollbar observation")
			return nil, false
		}
		if !complete {
			log.Info().
				Str("component", scrollbarCompleteComponentName).
				Str("node", currentNode).
				Msg("scrollbar missing observation recorded, confirming once")
			return nil, false
		}

		log.Info().
			Str("component", scrollbarCompleteComponentName).
			Str("node", currentNode).
			Msg("scrollbar missing in consecutive observations, list is not scrollable")
		return &maa.CustomRecognitionResult{
			Box: roi,
			Detail: marshalDetail(map[string]any{
				"scrollbar_found": false,
				"complete":        true,
			}),
		}, true
	}

	previous, ready, complete, err := observeScrollbar(ctx, currentNode, segment, params.PositionTolerance)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", scrollbarCompleteComponentName).
			Str("node", currentNode).
			Msg("failed to record valid scrollbar observation")
		return nil, false
	}

	if !complete {
		log.Info().
			Str("component", scrollbarCompleteComponentName).
			Str("node", currentNode).
			Int("top", segment.Top).
			Int("bottom", segment.Bottom).
			Bool("first_run", !ready).
			Msg("scrollbar position recorded, not complete yet")
		return nil, false
	}

	log.Info().
		Str("component", scrollbarCompleteComponentName).
		Str("node", currentNode).
		Int("previous_top", previous.Top).
		Int("previous_bottom", previous.Bottom).
		Int("current_top", segment.Top).
		Int("current_bottom", segment.Bottom).
		Int("position_tolerance", params.PositionTolerance).
		Msg("scrollbar position unchanged, list complete")

	return &maa.CustomRecognitionResult{
		Box: roi,
		Detail: marshalDetail(map[string]any{
			"previous_top":       previous.Top,
			"previous_bottom":    previous.Bottom,
			"current_top":        segment.Top,
			"current_bottom":     segment.Bottom,
			"position_tolerance": params.PositionTolerance,
			"complete":           true,
		}),
	}, true
}

func parseScrollbarParams(raw string) (*scrollbarParams, error) {
	params := &scrollbarParams{PositionTolerance: defaultPositionTolerance}
	if strings.TrimSpace(raw) == "" {
		return params, nil
	}

	var input struct {
		PositionTolerance *int `json:"position_tolerance"`
	}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return nil, err
	}
	if input.PositionTolerance != nil {
		params.PositionTolerance = *input.PositionTolerance
	}
	if params.PositionTolerance < 0 {
		return nil, fmt.Errorf("position_tolerance must be non-negative, got %d", params.PositionTolerance)
	}
	return params, nil
}

func detectScrollbarSegment(img image.Image, roi image.Rectangle) (scrollbarSegment, bool) {
	if img == nil {
		return scrollbarSegment{}, false
	}
	roi = roi.Intersect(img.Bounds())
	if roi.Empty() {
		return scrollbarSegment{}, false
	}

	whiteRows := make([]bool, roi.Dy())
	for y := roi.Min.Y; y < roi.Max.Y; y++ {
		for x := roi.Min.X; x < roi.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if min(r>>8, g>>8, b>>8) >= scrollbarWhiteThreshold {
				whiteRows[y-roi.Min.Y] = true
				break
			}
		}
	}

	fillScrollbarGaps(whiteRows)
	return longestScrollbarSegment(whiteRows)
}

func fillScrollbarGaps(rows []bool) {
	for index := 0; index < len(rows); {
		if rows[index] {
			index++
			continue
		}
		gapStart := index
		for index < len(rows) && !rows[index] {
			index++
		}
		if gapStart == 0 || index == len(rows) || index-gapStart > scrollbarMaxGap {
			continue
		}
		for gapIndex := gapStart; gapIndex < index; gapIndex++ {
			rows[gapIndex] = true
		}
	}
}

func longestScrollbarSegment(rows []bool) (scrollbarSegment, bool) {
	best := scrollbarSegment{}
	bestLength := 0
	for index := 0; index < len(rows); {
		if !rows[index] {
			index++
			continue
		}
		start := index
		for index < len(rows) && rows[index] {
			index++
		}
		length := index - start
		if length > bestLength {
			best = scrollbarSegment{Top: start, Bottom: index - 1}
			bestLength = length
		}
	}
	return best, bestLength >= scrollbarMinLength
}

func scrollbarSegmentsMatch(previous, current scrollbarSegment, tolerance int) bool {
	return abs(previous.Top-current.Top) <= tolerance &&
		abs(previous.Bottom-current.Bottom) <= tolerance
}

func loadScrollbarPosition(store nodeStore, nodeName string) (scrollbarSegment, bool, error) {
	raw, err := store.GetNodeJSON(nodeName)
	if err != nil {
		return scrollbarSegment{}, false, err
	}
	var wrapper struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return scrollbarSegment{}, false, err
	}
	if wrapper.Attach == nil {
		return scrollbarSegment{}, false, nil
	}

	readyRaw, ok := wrapper.Attach[attachReady]
	if !ok {
		return scrollbarSegment{}, false, nil
	}
	var ready bool
	if err := json.Unmarshal(readyRaw, &ready); err != nil {
		return scrollbarSegment{}, false, fmt.Errorf("attach.%s must be bool: %w", attachReady, err)
	}
	if !ready {
		return scrollbarSegment{}, false, nil
	}

	var segment scrollbarSegment
	if err := unmarshalAttachInt(wrapper.Attach, attachScrollbarTop, &segment.Top); err != nil {
		return scrollbarSegment{}, false, err
	}
	if err := unmarshalAttachInt(wrapper.Attach, attachScrollbarBottom, &segment.Bottom); err != nil {
		return scrollbarSegment{}, false, err
	}
	return segment, true, nil
}

func observeMissingScrollbar(store nodeStore, nodeName string) (bool, error) {
	missing, err := loadMissingScrollbar(store, nodeName)
	if err != nil {
		return false, err
	}
	if missing {
		return true, nil
	}
	if err := saveMissingScrollbar(store, nodeName, true); err != nil {
		return false, err
	}
	return false, nil
}

// observeScrollbar 先按旧位置判断列表是否停止移动，再保存当前有效位置并清除缺失确认状态。
func observeScrollbar(
	store nodeStore,
	nodeName string,
	segment scrollbarSegment,
	positionTolerance int,
) (previous scrollbarSegment, ready bool, complete bool, err error) {
	previous, ready, err = loadScrollbarPosition(store, nodeName)
	if err != nil {
		return scrollbarSegment{}, false, false, err
	}
	complete = ready && scrollbarSegmentsMatch(previous, segment, positionTolerance)
	if err = saveScrollbarPosition(store, nodeName, segment); err != nil {
		return previous, ready, false, err
	}
	return previous, ready, complete, nil
}

func loadMissingScrollbar(store nodeStore, nodeName string) (bool, error) {
	raw, err := store.GetNodeJSON(nodeName)
	if err != nil {
		return false, err
	}
	var wrapper struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return false, err
	}
	readyRaw, ok := wrapper.Attach[attachReady]
	if !ok {
		return false, nil
	}
	var ready bool
	if err := json.Unmarshal(readyRaw, &ready); err != nil {
		return false, fmt.Errorf("attach.%s must be bool: %w", attachReady, err)
	}
	if !ready {
		return false, nil
	}
	missingRaw, ok := wrapper.Attach[attachScrollbarMissing]
	if !ok {
		return false, nil
	}
	var missing bool
	if err := json.Unmarshal(missingRaw, &missing); err != nil {
		return false, fmt.Errorf("attach.%s must be bool: %w", attachScrollbarMissing, err)
	}
	return missing, nil
}

func saveMissingScrollbar(store nodeStore, nodeName string, missing bool) error {
	raw, err := store.GetNodeJSON(nodeName)
	if err != nil {
		return err
	}
	var wrapper struct {
		Attach map[string]any `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return err
	}
	if wrapper.Attach == nil {
		wrapper.Attach = make(map[string]any)
	}
	wrapper.Attach[attachReady] = true
	wrapper.Attach[attachScrollbarMissing] = missing

	return store.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"attach": wrapper.Attach,
		},
	})
}

func unmarshalAttachInt(attach map[string]json.RawMessage, key string, target *int) error {
	raw, ok := attach[key]
	if !ok {
		return fmt.Errorf("attach.%s is required when ready is true", key)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("attach.%s must be int: %w", key, err)
	}
	return nil
}

func saveScrollbarPosition(store nodeStore, nodeName string, segment scrollbarSegment) error {
	raw, err := store.GetNodeJSON(nodeName)
	if err != nil {
		return err
	}
	var wrapper struct {
		Attach map[string]any `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return err
	}
	if wrapper.Attach == nil {
		wrapper.Attach = make(map[string]any)
	}
	wrapper.Attach[attachReady] = true
	wrapper.Attach[attachScrollbarTop] = segment.Top
	wrapper.Attach[attachScrollbarBottom] = segment.Bottom
	wrapper.Attach[attachScrollbarMissing] = false

	return store.OverridePipeline(map[string]any{
		nodeName: map[string]any{
			"attach": wrapper.Attach,
		},
	})
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
