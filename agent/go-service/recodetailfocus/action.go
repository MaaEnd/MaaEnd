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

	ocrText, detailHit := collectOCRTextFromAction(arg)
	content := renderContent(contentTemplate, "N/A", ocrText, arg.CurrentTaskName, detailHit)
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
	if !detail.Hit || detail.Results == nil {
		log.Warn().
			Str("node", arg.CurrentTaskName).
			Bool("hit", detail.Hit).
			Msg("RecoDetailFocusAction recognition not hit or empty result")
		return "N/A", detail.Hit
	}

	seen := make(map[string]struct{})
	collected := make([]string, 0, 4)
	for _, group := range [][]*maa.RecognitionResult{{detail.Results.Best}, detail.Results.Filtered, detail.Results.All} {
		for _, r := range group {
			if r == nil {
				continue
			}
			ocrResult, ok := r.AsOCR()
			if !ok {
				continue
			}
			text := strings.TrimSpace(ocrResult.Text)
			if text == "" {
				continue
			}
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			collected = append(collected, text)
		}
	}

	if len(collected) == 0 {
		log.Warn().
			Str("node", arg.CurrentTaskName).
			Msg("RecoDetailFocusAction OCR result exists but text empty")
		return "N/A", true
	}

	return strings.Join(collected, " | "), true
}

func renderContent(tpl, roiText, ocrText, nodeName string, hit bool) string {
	return strings.NewReplacer(
		"{roi}", roiText,
		"{text}", ocrText,
		"{node}", nodeName,
		"{hit}", fmt.Sprintf("%t", hit),
	).Replace(tpl)
}
