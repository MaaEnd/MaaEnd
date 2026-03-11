package recodetailfocus

import (
	"encoding/json"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/rs/zerolog/log"
)

const (
	defaultContentTemplate = "text={text}"
)

type RecoDetailFocusAction struct{}

type recoDetailFocusParam struct {
	Text string `json:"text"`
}

func (a *RecoDetailFocusAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Msg("RecoDetailFocusAction got nil custom action arg")
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

	contentTemplate := defaultContentTemplate
	if strings.TrimSpace(params.Text) != "" {
		contentTemplate = params.Text
	}
	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("template", contentTemplate).
		Msg("RecoDetailFocusAction template resolved")

	ocrText, detailHit := collectOCRTextFromAction(arg)
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

func collectOCRTextFromAction(arg *maa.CustomActionArg) (string, bool) {
	if arg == nil || arg.RecognitionDetail == nil {
		log.Warn().Msg("RecoDetailFocusAction recognition detail missing")
		return "N/A", false
	}

	detail := arg.RecognitionDetail
	filteredCount := 0
	allCount := 0
	if detail.Results != nil {
		filteredCount = len(detail.Results.Filtered)
		allCount = len(detail.Results.All)
	}
	log.Info().
		Str("node", arg.CurrentTaskName).
		Bool("hit", detail.Hit).
		Bool("has_results", detail.Results != nil).
		Int("filtered_count", filteredCount).
		Int("all_count", allCount).
		Msg("RecoDetailFocusAction recognition detail summary")

	if !detail.Hit || detail.Results == nil {
		log.Warn().
			Str("node", arg.CurrentTaskName).
			Bool("hit", detail.Hit).
			Msg("RecoDetailFocusAction recognition not hit or empty result")
		return "N/A", detail.Hit
	}

	best := detail.Results.Best
	if best == nil {
		log.Warn().
			Str("node", arg.CurrentTaskName).
			Msg("RecoDetailFocusAction OCR best result missing")
		return "N/A", true
	}

	ocrResult, ok := best.AsOCR()
	if !ok {
		log.Warn().
			Str("node", arg.CurrentTaskName).
			Msg("RecoDetailFocusAction best result is not OCR")
		return "N/A", true
	}

	text := strings.TrimSpace(ocrResult.Text)
	if text == "" {
		log.Warn().
			Str("node", arg.CurrentTaskName).
			Msg("RecoDetailFocusAction OCR best text empty")
		return "N/A", true
	}
	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("best_text", text).
		Msg("RecoDetailFocusAction OCR best text extracted")

	return text, true
}

func renderContent(tpl, ocrText, nodeName string, hit bool) string {
	return strings.NewReplacer(
		"{text}", ocrText,
		"{node}", nodeName,
		"{hit}", strconv.FormatBool(hit),
	).Replace(tpl)
}
