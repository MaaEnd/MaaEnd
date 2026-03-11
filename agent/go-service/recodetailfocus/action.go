package recodetailfocus

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	defaultContentTemplate = "text={text}"
	managedOCRNode         = "__RecoDetailFocusOCR"
)

type RecoDetailFocusAction struct{}

type recoDetailFocusParam struct {
	Recognition map[string]any `json:"recognition"`
	Text        string         `json:"text"`
}

func (a *RecoDetailFocusAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Msg("RecoDetailFocusAction got nil context or custom action arg")
		return false
	}
	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("param", arg.CustomActionParam).
		Msg("RecoDetailFocusAction start")

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

	recognitionConfig := normalizeRecognitionParam(params.Recognition)
	if len(recognitionConfig) == 0 {
		log.Error().
			Str("node", arg.CurrentTaskName).
			Msg("RecoDetailFocusAction missing custom_action_param.recognition")
		return false
	}

	override := map[string]any{
		managedOCRNode: recognitionConfig,
	}
	if err := ctx.OverridePipeline(override); err != nil {
		log.Error().
			Err(err).
			Str("node", arg.CurrentTaskName).
			Interface("override", override).
			Msg("RecoDetailFocusAction failed to override OCR node")
		return false
	}

	contentTemplate := defaultContentTemplate
	if strings.TrimSpace(params.Text) != "" {
		contentTemplate = params.Text
	}

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().
			Str("node", arg.CurrentTaskName).
			Msg("RecoDetailFocusAction controller is nil")
		return false
	}
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().
			Err(err).
			Str("node", arg.CurrentTaskName).
			Msg("RecoDetailFocusAction cache image failed")
		return false
	}

	detail, err := ctx.RunRecognition(managedOCRNode, img)
	if err != nil {
		log.Error().
			Err(err).
			Str("node", arg.CurrentTaskName).
			Str("ocr_node", managedOCRNode).
			Msg("RecoDetailFocusAction run OCR node failed")
		return false
	}

	detailHit := detail != nil && detail.Hit
	ocrText, _ := extractBestOCRText(detail)
	if ocrText == "" {
		ocrText = "N/A"
	}

	content := renderContent(contentTemplate, ocrText, arg.CurrentTaskName, detailHit)
	maafocus.NodeActionStarting(ctx, content)

	log.Info().
		Str("node", arg.CurrentTaskName).
		Bool("recognition_hit", detailHit).
		Str("ocr_text", ocrText).
		Str("content", content).
		Msg("RecoDetailFocusAction rendered")
	return true
}

func normalizeRecognitionParam(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	recognition := make(map[string]any, len(raw)+1)
	for k, v := range raw {
		recognition[k] = v
	}
	// 默认按 OCR 节点处理，允许调用侧显式覆盖 recognition 字段。
	if _, ok := recognition["recognition"]; !ok {
		recognition["recognition"] = "OCR"
	}
	return recognition
}

func extractBestOCRText(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil || !detail.Hit || detail.Results == nil || detail.Results.Best == nil {
		return "", false
	}
	ocrResult, ok := detail.Results.Best.AsOCR()
	if !ok {
		return "", false
	}
	text := strings.TrimSpace(ocrResult.Text)
	if text == "" {
		return "", false
	}
	return text, true
}

func renderContent(tpl, ocrText, nodeName string, hit bool) string {
	return strings.NewReplacer(
		"{text}", ocrText,
		"{node}", nodeName,
		"{hit}", strconv.FormatBool(hit),
	).Replace(tpl)
}
