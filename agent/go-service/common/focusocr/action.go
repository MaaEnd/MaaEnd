package focusocr

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const componentName = "FocusOCRAction"

var _ maa.CustomActionRunner = &Action{}

// Action screenshots, runs a Pipeline recognition node, and maafocus-prints the OCR hit.
type Action struct{}

type params struct {
	Node  string `json:"node"`
	Focus string `json:"focus"`
}

func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || ctx.GetTasker() == nil || ctx.GetTasker().GetController() == nil {
		log.Error().Str("component", componentName).Msg("nil context, arg, tasker or controller")
		return false
	}

	var p params
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("parse params failed")
		return false
	}
	p.Node = strings.TrimSpace(p.Node)
	if p.Node == "" {
		log.Error().Str("component", componentName).Msg("node is required")
		return false
	}

	ctrl := ctx.GetTasker().GetController()
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Error().Err(err).Str("component", componentName).Msg("screencap failed")
		return false
	}

	detail, err := ctx.RunRecognition(p.Node, img)
	if err != nil || detail == nil || !detail.Hit {
		log.Error().Err(err).Str("component", componentName).Str("node", p.Node).Msg("recognition miss")
		return false
	}
	selected, err := recogtarget.SelectDetail(ctx, p.Node, detail)
	if err != nil || selected == nil || selected.Results == nil {
		log.Error().Err(err).Str("component", componentName).Str("node", p.Node).Msg("no ocr detail")
		return false
	}

	hits := selected.Results.Filtered
	if len(hits) == 0 {
		hits = selected.Results.All
	}
	text := firstOCRText(hits)
	if text == "" {
		log.Error().Str("component", componentName).Msg("ocr text empty")
		return false
	}
	focus := text
	if f := strings.TrimSpace(p.Focus); f != "" {
		focus = fmt.Sprintf(f, text)
	}
	maafocus.Print(ctx, focus)
	return true
}

func firstOCRText(hits []*maa.RecognitionResult) string {
	for _, hit := range hits {
		if hit == nil {
			continue
		}
		ocr, ok := hit.AsOCR()
		if !ok || ocr == nil {
			continue
		}
		if text := strings.TrimSpace(ocr.Text); text != "" {
			return text
		}
	}
	return ""
}
