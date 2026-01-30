package friendvisit

import (
	"encoding/json"
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

type ProductionAssistOCRAction struct{}

func (a *ProductionAssistOCRAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[FriendVisit]Controller is nil")
		return false
	}

	controller.PostScreencap().Wait()
	img := controller.CacheImage()
	if img == nil {
		log.Error().Msg("[FriendVisit]Failed to get screenshot")
		return false
	}

	text := ocrText(ctx, img, []int{1224, 71, 10, 16}, 0.6)
	norm := strings.TrimSpace(text)
	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("roi", "1224,71,10,16").
		Str("text", text).
		Msg("[FriendVisit]OCR result")
	if norm != "" && norm != "0" && norm != "O" && norm != "o" {
		log.Info().Str("node", arg.CurrentTaskName).Str("next", "ProductionAssist").Msg("[FriendVisit]OCR route")
		ctx.OverrideNext(arg.CurrentTaskName, []string{"ProductionAssist"})
		return true
	}

	log.Info().Str("node", arg.CurrentTaskName).Str("next", "ClueExchangeOCR").Msg("[FriendVisit]OCR route")
	ctx.OverrideNext(arg.CurrentTaskName, []string{"ClueExchangeOCR"})
	return true
}

type ClueExchangeOCRAction struct{}

func (a *ClueExchangeOCRAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[FriendVisit]Controller is nil")
		return false
	}

	controller.PostScreencap().Wait()
	img := controller.CacheImage()
	if img == nil {
		log.Error().Msg("[FriendVisit]Failed to get screenshot")
		return false
	}

	text := ocrText(ctx, img, []int{1165, 71, 10, 16}, 0.6)
	norm := strings.TrimSpace(text)
	log.Info().
		Str("node", arg.CurrentTaskName).
		Str("roi", "1165,71,10,16").
		Str("text", text).
		Msg("[FriendVisit]OCR result")
	if norm != "" && norm != "0" && norm != "O" && norm != "o" {
		log.Info().Str("node", arg.CurrentTaskName).Str("next", "ClueExchange").Msg("[FriendVisit]OCR route")
		ctx.OverrideNext(arg.CurrentTaskName, []string{"ClueExchange"})
		return true
	}

	log.Info().Str("node", arg.CurrentTaskName).Str("next", "VisitEnd").Msg("[FriendVisit]OCR route")
	ctx.OverrideNext(arg.CurrentTaskName, []string{"VisitEnd"})
	return true
}

func ocrText(ctx *maa.Context, img image.Image, roi []int, threshold float64) string {
	if len(roi) != 4 {
		return ""
	}
	param := &maa.NodeOCRParam{
		ROI:       maa.NewTargetRect(maa.Rect{roi[0], roi[1], roi[2], roi[3]}),
		OrderBy:   "Expected",
		Expected:  []string{".*"},
		Threshold: threshold,
	}
	detail := ctx.RunRecognitionDirect(maa.NodeRecognitionTypeOCR, param, img)
	if detail == nil || detail.DetailJson == "" {
		return ""
	}
	return extractTextFromOCR(detail.DetailJson)
}

func extractTextFromOCR(detailJSON string) string {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(detailJSON), &raw); err != nil {
		return ""
	}

	for _, key := range []string{"filtered", "best", "all"} {
		if data, ok := raw[key]; ok {
			switch v := data.(type) {
			case []interface{}:
				if len(v) > 0 {
					if result, ok := v[0].(map[string]interface{}); ok {
						if text, ok := result["text"].(string); ok {
							return text
						}
					}
				}
			case map[string]interface{}:
				if text, ok := v["text"].(string); ok {
					return text
				}
			}
		}
	}

	return ""
}
