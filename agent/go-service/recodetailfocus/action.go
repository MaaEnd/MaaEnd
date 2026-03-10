package recodetailfocus

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/rs/zerolog/log"
)

const (
	defaultContentTemplate = "roi={roi}, text={text}"
	internalOCRNodeName    = "__RecoDetailFocusOCR"
)

type RecoDetailFocusAction struct{}

type recoDetailFocusParam struct {
	Text         string `json:"text"`
	ROI          any    `json:"roi"`
	ROIOffset    any    `json:"roi_offset"`
	Expected     any    `json:"expected"`
	RefreshImage bool   `json:"refresh_image"`
}

func (a *RecoDetailFocusAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Msg("RecoDetailFocusAction got nil custom action arg")
		return false
	}

	var params recoDetailFocusParam
	if arg.CustomActionParam != "" {
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().
				Err(err).
				Str("param", arg.CustomActionParam).
				Msg("RecoDetailFocusAction failed to parse custom_action_param")
			return false
		}
	}

	contentTemplate := defaultContentTemplate
	if strings.TrimSpace(params.Text) != "" {
		contentTemplate = params.Text
	}

	targetROI := normalizeROI(params.ROI)
	targetROIOffset := normalizeROIOffset(params.ROIOffset)
	targetExpected := normalizeExpected(params.Expected)

	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("roi", stringify(targetROI)).
		Str("roi_offset", stringify(targetROIOffset)).
		Str("expected", stringify(targetExpected)).
		Bool("refresh_image", params.RefreshImage).
		Msg("RecoDetailFocusAction normalized params")

	ocrText, ok := runOCR(ctx, targetROI, targetROIOffset, targetExpected, params.RefreshImage)
	if !ok {
		return false
	}

	roiText := stringify(targetROI)
	content := renderContent(contentTemplate, roiText, ocrText)
	maafocus.NodeActionStarting(ctx, content)

	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("content", content).
		Msg("RecoDetailFocusAction rendered")
	return true
}

func normalizeROI(v any) any {
	if isEmptyValue(v) {
		return []int{0, 0, 0, 0}
	}
	return v
}

func normalizeROIOffset(v any) any {
	if isEmptyValue(v) {
		return []int{0, 0, 0, 0}
	}
	return v
}

func normalizeExpected(v any) any {
	if isEmptyValue(v) {
		return ""
	}
	return v
}

func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case []int:
		return len(x) == 0
	case []string:
		return len(x) == 0
	}
	return false
}

func runOCR(ctx *maa.Context, roi, roiOffset, expected any, refreshImage bool) (string, bool) {
	if ctx == nil {
		log.Error().Msg("RecoDetailFocusAction context is nil")
		return "N/A", true
	}

	nodeOverride := map[string]any{
		"roi": roi,
	}
	if roiOffset != nil {
		nodeOverride["roi_offset"] = roiOffset
	}
	if expected != nil {
		nodeOverride["expected"] = expected
	}

	override := map[string]any{
		internalOCRNodeName: nodeOverride,
	}
	log.Info().
		Str("ocr_node", internalOCRNodeName).
		Str("override", stringify(override)).
		Bool("refresh_image", refreshImage).
		Msg("RecoDetailFocusAction OCR attempt")

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("RecoDetailFocusAction controller is nil")
		return "N/A", true
	}

	if refreshImage {
		controller.PostScreencap().Wait()
	}

	img, err := controller.CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("RecoDetailFocusAction get cached image failed")
		return "N/A", true
	}

	detail, err := ctx.RunRecognition(internalOCRNodeName, img, override)
	if err != nil {
		log.Error().
			Err(err).
			Str("override", stringify(override)).
			Msg("RecoDetailFocusAction run OCR failed")

		fallbackNodeOverride := map[string]any{
			"roi": roi,
		}
		fallbackOverride := map[string]any{
			internalOCRNodeName: fallbackNodeOverride,
		}
		log.Warn().
			Str("override", stringify(fallbackOverride)).
			Msg("RecoDetailFocusAction retry OCR with minimal override")

		detail, err = ctx.RunRecognition(internalOCRNodeName, img, fallbackOverride)
		if err != nil {
			log.Error().
				Err(err).
				Str("override", stringify(fallbackOverride)).
				Msg("RecoDetailFocusAction OCR fallback failed")
			return "N/A", true
		}
	}

	if detail == nil || !detail.Hit {
		log.Warn().Str("node", internalOCRNodeName).Msg("RecoDetailFocusAction OCR no hit")
		return "N/A", true
	}

	if detail.Results != nil {
		for _, group := range [][]*maa.RecognitionResult{
			{detail.Results.Best},
			detail.Results.Filtered,
			detail.Results.All,
		} {
			for _, r := range group {
				if r == nil {
					continue
				}
				if ocr, ok := r.AsOCR(); ok && strings.TrimSpace(ocr.Text) != "" {
					return strings.TrimSpace(ocr.Text), true
				}
			}
		}
	}

	return "N/A", true
}

func stringify(v any) string {
	if v == nil {
		return "N/A"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func renderContent(tpl, roiText, ocrText string) string {
	return strings.NewReplacer(
		"{roi}", roiText,
		"{text}", ocrText,
	).Replace(tpl)
}
