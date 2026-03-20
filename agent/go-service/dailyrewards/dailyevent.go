package dailyrewards

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type dailyEventUnreadItem struct {
	Box  maa.Rect `json:"box"`
	Text string   `json:"text"`
}

type dailyEventUnreadDetail struct {
	Box maa.Rect // 活动右侧红点坐标
}

var dailyEventUnreadDetails []dailyEventUnreadDetail

type DailyEventUnreadItemInitRecognition struct{}

// Compile-time interface checks
var (
	_ maa.CustomRecognitionRunner = &DailyEventUnreadItemInitRecognition{}
	_ maa.CustomActionRunner      = &DailyEventUnreadItemInitAction{}
)

func (r *DailyEventUnreadItemInitRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var items []dailyEventUnreadItem

	// 在左侧区域查找所有红点图标
	overrideParamRedDot := map[string]any{
		"DailyEventRecognitionRedDot": map[string]any{
			"roi": maa.Rect{0, 0, 300, 720},
		},
	}
	detail, err := ctx.RunRecognition("DailyEventRecognitionRedDot", arg.Img, overrideParamRedDot)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run TemplateMatch for RedDot")
		return nil, false
	}
	if detail == nil || !detail.Hit || detail.Results == nil || len(detail.Results.Filtered) == 0 {
		log.Info().Msg("No red dot found in event list")
		return nil, false
	}

	// 遍历所有红点位置，在其左下侧区域调用OCR获取文本坐标，确认是否为未读活动
	for _, result := range detail.Results.Filtered {
		tmResult, ok := result.AsTemplateMatch()
		if !ok {
			continue
		}

		redDotBox := tmResult.Box
		overrideParamItemText := map[string]any{
			"DailyEventRecognitionItemText": map[string]any{
				"roi": maa.Rect{
					0,
					redDotBox.Y(),
					redDotBox.X(),
					60, // 一个列表项高度大约60
				},
			},
		}

		ocrDetail, err := ctx.RunRecognition("DailyEventRecognitionItemText", arg.Img, overrideParamItemText)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to run OCR for event text")
			continue
		}
		if ocrDetail == nil || !ocrDetail.Hit || ocrDetail.Results == nil || len(ocrDetail.Results.Filtered) == 0 {
			continue
		}

		ocrResult, ok := ocrDetail.Results.Filtered[0].AsOCR()
		if !ok {
			continue
		}

		duplicate := false
		for _, existing := range items {
			if existing.Text == ocrResult.Text {
				duplicate = true
				break
			}
		}
		if duplicate {
			log.Debug().Str("text", ocrResult.Text).Msg("Skipping duplicate unread event")
			continue
		}

		items = append(items, dailyEventUnreadItem{
			Box:  ocrResult.Box,
			Text: ocrResult.Text,
		})
		log.Debug().
			Str("text", ocrResult.Text).
			Interface("box", ocrResult.Box).
			Msg("Found unread event")
	}

	if len(items) == 0 {
		log.Info().Msg("No unread events found after OCR")
		return nil, false
	}

	log.Info().Int("count", len(items)).Msg("Unread events initialized")

	detailJSON, err := json.Marshal(map[string]any{
		"items": items,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal unread items result")
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detailJSON),
	}, true
}

type DailyEventUnreadItemInitAction struct{}

func (a *DailyEventUnreadItemInitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg.RecognitionDetail == nil {
		log.Error().
			Str("component", "DailyEventUnreadItemInitAction").
			Msg("recognition detail is nil")
		return false
	}
	if arg.RecognitionDetail.Results == nil || arg.RecognitionDetail.Results.Best == nil {
		log.Error().
			Str("component", "DailyEventUnreadItemInitAction").
			Msg("results or best is nil")
		return false
	}
	customResult, ok := arg.RecognitionDetail.Results.Best.AsCustom()
	if !ok {
		log.Error().
			Str("component", "DailyEventUnreadItemInitAction").
			Msg("failed to get custom recognition result")
		return false
	}
	var result struct {
		Items []dailyEventUnreadItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(customResult.Detail), &result); err != nil {
		log.Error().
			Err(err).
			Str("component", "DailyEventUnreadItemInitAction").
			Msg("failed to parse recognition detail")
		return false
	}

	actionResult := true
	for _, item := range result.Items {
		log.Info().
			Str("component", "DailyEventUnreadItemInitAction").
			Str("text", item.Text).
			Interface("box", item.Box).
			Msg("processing unread event item")

		override := map[string]any{
			"DailyEventUnreadItemSwitch": map[string]any{
				"roi": item.Box,
			},
		}
		detail, err := ctx.RunTask("DailyEventUnreadItemSwitch", override)
		if err != nil {
			log.Error().
				Err(err).
				Str("task", "DailyEventUnreadItemSwitch").
				Str("text", item.Text).
				Interface("box", item.Box).
				Msg("DailyEventUnreadItemSwitch task failed")
			actionResult = false
		}
		log.Debug().
			Str("component", "DailyEventUnreadItemInitAction").
			Interface("detail", detail).
			Msg("DailyEventUnreadItemSwitch task result")
	}

	return actionResult
}
