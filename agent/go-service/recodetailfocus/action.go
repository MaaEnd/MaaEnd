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
	Text      string `json:"text"`
	ROI       any    `json:"roi"`
	Roi       any    `json:"Roi"`
	Box       any    `json:"box"`
	ROIOffset any    `json:"roi_offset"`
	Expected  any    `json:"expected"`
	Expected2 any    `json:"Expected"`
	RefreshImage  bool `json:"refresh_image"`
	RefreshImage2 bool `json:"refreshImage"`
	GetImg    bool   `json:"getimg"`
	GetImg2   bool   `json:"getImg"`
}

func (a *RecoDetailFocusAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	contentTemplate := defaultContentTemplate
	targetROI := any([]int{0, 0, 1280, 720})
	var targetROIOffset any
	var targetExpected any
	refreshImage := false
	if arg.CustomActionParam != "" {
		var p recoDetailFocusParam
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
			log.Error().
				Err(err).
				Str("param", arg.CustomActionParam).
				Msg("RecoDetailFocusAction parse custom_action_param failed")
			return false
		}
		if strings.TrimSpace(p.Text) != "" {
			contentTemplate = p.Text
		}
		if roi, ok := pickROI(p); ok {
			targetROI = roi
		}
		if p.ROIOffset != nil {
			targetROIOffset = p.ROIOffset
		}
		if p.Expected != nil {
			targetExpected = p.Expected
		} else if p.Expected2 != nil {
			targetExpected = p.Expected2
		}
		refreshImage = p.RefreshImage || p.RefreshImage2 || p.GetImg || p.GetImg2
	}

	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("template", contentTemplate).
		Str("roi", stringifyValue(targetROI)).
		Str("roi_offset", stringifyValue(targetROIOffset)).
		Str("expected", stringifyValue(targetExpected)).
		Bool("refresh_image", refreshImage).
		Msg("RecoDetailFocusAction parsed params")

	ocrText, ok := runOCR(ctx, arg, targetROI, targetROIOffset, targetExpected, refreshImage)
	if !ok {
		return false
	}

	roiText := stringifyValue(targetROI)
	content := renderContent(contentTemplate, roiText, ocrText)
	maafocus.NodeActionStarting(ctx, content)

	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("roi", roiText).
		Str("ocr_text", ocrText).
		Str("content", content).
		Msg("RecoDetailFocusAction rendered")
	return true
}

func pickROI(p recoDetailFocusParam) (any, bool) {
	candidates := []any{p.ROI, p.Roi, p.Box}
	for _, c := range candidates {
		if c != nil {
			return c, true
		}
	}
	return nil, false
}

func runOCR(ctx *maa.Context, arg *maa.CustomActionArg, roi any, roiOffset any, expected any, refreshImage bool) (string, bool) {
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
		Str("node", arg.CurrentTaskName).
		Str("ocr_node", internalOCRNodeName).
		Str("override_roi", stringifyValue(roi)).
		Bool("override_has_roi_offset", roiOffset != nil).
		Str("override_roi_offset", stringifyValue(roiOffset)).
		Bool("override_has_expected", expected != nil).
		Str("override_expected", stringifyValue(expected)).
		Msg("RecoDetailFocusAction override fields")

	log.Info().
		Str("node", arg.CurrentTaskName).
		Bool("refresh_image", refreshImage).
		Interface("ocr_override", override).
		Msg("RecoDetailFocusAction run OCR with override")

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("RecoDetailFocusAction controller is nil")
		return "", false
	}

	if refreshImage {
		controller.PostScreencap().Wait()
	}

	img, err := controller.CacheImage()
	if err != nil {
		log.Error().Err(err).Bool("refresh_image", refreshImage).Msg("RecoDetailFocusAction get cached image failed")
		return "", false
	}

	detail, err := ctx.RunRecognition(internalOCRNodeName, img, override)
	if err != nil {
		log.Error().Err(err).Str("node", internalOCRNodeName).Msg("RecoDetailFocusAction run OCR failed")
		return "", false
	}

	log.Info().
		Str("node", internalOCRNodeName).
		Bool("hit", detail != nil && detail.Hit).
		Msg("RecoDetailFocusAction OCR result status")

	if detail == nil || !detail.Hit {
		log.Warn().Str("node", internalOCRNodeName).Msg("RecoDetailFocusAction OCR no hit")
		return "N/A", true
	}
	text := "N/A"
	if detail.Results != nil {
		for _, group := range [][]*maa.RecognitionResult{{detail.Results.Best}, detail.Results.Filtered, detail.Results.All} {
			for _, r := range group {
				if r == nil {
					continue
				}
				if ocr, ok := r.AsOCR(); ok && strings.TrimSpace(ocr.Text) != "" {
					text = strings.TrimSpace(ocr.Text)
					log.Info().
						Str("node", internalOCRNodeName).
						Str("ocr_text", text).
						Msg("RecoDetailFocusAction OCR extracted text")
					return text, true
				}
			}
		}
	}
	log.Info().
		Str("node", internalOCRNodeName).
		Str("ocr_text", text).
		Msg("RecoDetailFocusAction OCR extracted default text")
	return text, true
}

func stringifyValue(v any) string {
	if v == nil {
		return "N/A"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func renderContent(tpl string, roiText string, ocrText string) string {
	replacer := strings.NewReplacer(
		"{roi}", roiText,
		"{{roi}}", roiText,
		"{text}", ocrText,
		"{{text}}", ocrText,
	)
	return replacer.Replace(tpl)
}
