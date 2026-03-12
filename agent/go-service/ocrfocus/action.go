package ocrfocus

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const defaultTextTemplate = "当前识别结果: {text}"
const managedOCRNode = "__OCRFocusManagedOCR"

type OCRFocusAction struct{}

type ocrFocusParam struct {
	Text        string         `json:"text"`
	Recognition map[string]any `json:"recognition"`
	ROIOffset   []int          `json:"roi_offset"`
}

func (a *OCRFocusAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Msg("OCRFocusAction got nil context or custom action arg")
		return false
	}

	template := defaultTextTemplate
	var params ocrFocusParam
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().
				Err(err).
				Str("param", arg.CustomActionParam).
				Msg("OCRFocusAction failed to parse custom_action_param")
			return false
		}
		if strings.TrimSpace(params.Text) != "" {
			template = params.Text
		}
	}

	detail := arg.RecognitionDetail
	if len(params.Recognition) > 0 {
		var err error
		detail, err = runOCRWithOverride(ctx, detail, params.Recognition, params.ROIOffset)
		if err != nil {
			log.Error().Err(err).Msg("OCRFocusAction failed to run override OCR")
			maafocus.NodeActionStarting(ctx, "OCR识别执行失败")
			return true
		}
	}

	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		maafocus.NodeActionStarting(ctx, "当前节点没有可用识别结果")
		return true
	}

	ocrRes, ok := detail.Results.Best.AsOCR()
	if !ok {
		maafocus.NodeActionStarting(ctx, "当前识别结果不是OCR")
		return true
	}

	text := strings.TrimSpace(ocrRes.Text)
	if text == "" {
		maafocus.NodeActionStarting(ctx, "OCR未识别到任何文本")
		return true
	}

	content := strings.ReplaceAll(template, "{text}", text)
	maafocus.NodeActionStarting(ctx, content)
	return true
}

func runOCRWithOverride(ctx *maa.Context, baseDetail *maa.RecognitionDetail, recognition map[string]any, roiOffset []int) (*maa.RecognitionDetail, error) {
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		return nil, errors.New("controller is nil")
	}
	img, err := controller.CacheImage()
	if err != nil {
		return nil, err
	}

	overrideRecognition := cloneMap(recognition)
	if len(roiOffset) == 4 {
		if baseBox, ok := extractBestBox(baseDetail); ok {
			roi := []int{
				baseBox[0] + roiOffset[0],
				baseBox[1] + roiOffset[1],
				baseBox[2] + roiOffset[2],
				baseBox[3] + roiOffset[3],
			}
			overrideRecognition["roi"] = roi
		}
	}

	override := map[string]any{
		managedOCRNode: overrideRecognition,
	}
	return ctx.RunRecognition(managedOCRNode, img, override)
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func extractBestBox(detail *maa.RecognitionDetail) ([4]int, bool) {
	var out [4]int
	if detail == nil || strings.TrimSpace(detail.DetailJson) == "" {
		return out, false
	}
	var parsed struct {
		Best struct {
			Box []int `json:"box"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &parsed); err != nil {
		return out, false
	}
	if len(parsed.Best.Box) < 4 {
		return out, false
	}
	out = [4]int{parsed.Best.Box[0], parsed.Best.Box[1], parsed.Best.Box[2], parsed.Best.Box[3]}
	return out, true
}
