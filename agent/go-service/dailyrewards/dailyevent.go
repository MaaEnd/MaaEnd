package dailyrewards

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type dailyEventUnreadItem struct {
	Box  maa.Rect // 活动左侧item坐标
	Text string   // 活动名称
}

var dailyEventUnreadItems []dailyEventUnreadItem

type dailyEventUnreadDetail struct {
	Box maa.Rect // 活动右侧红点坐标
}

var dailyEventUnreadDetails []dailyEventUnreadDetail

type DailyEventUnreadItemInitRecognition struct{}

func (r *DailyEventUnreadItemInitRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	dailyEventUnreadItems = nil

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

		dailyEventUnreadItems = append(dailyEventUnreadItems, dailyEventUnreadItem{
			Box:  ocrResult.Box,
			Text: ocrResult.Text,
		})
		log.Debug().
			Str("text", ocrResult.Text).
			Interface("box", ocrResult.Box).
			Msg("Found unread event")
	}

	if len(dailyEventUnreadItems) == 0 {
		log.Info().Msg("No unread events found after OCR")
		return nil, false
	}

	log.Info().Int("count", len(dailyEventUnreadItems)).Msg("Unread events initialized")
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "init unread events"}`,
	}, true
}

type DailyEventUnreadItemSwitchRecognition struct{}

func (r *DailyEventUnreadItemSwitchRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if len(dailyEventUnreadItems) == 0 {
		return nil, false
	}

	// 取出第一个未读条目
	item := dailyEventUnreadItems[0]
	dailyEventUnreadItems = dailyEventUnreadItems[1:]

	log.Debug().
		Str("text", item.Text).
		Interface("box", item.Box).
		Int("remaining", len(dailyEventUnreadItems)).
		Msg("Switch unread item")

	return &maa.CustomRecognitionResult{
		Box:    item.Box,
		Detail: `{"custom": "switch unread item"}`,
	}, true
}

type DailyEventUnreadDetailInitRecognition struct{}

func (r *DailyEventUnreadDetailInitRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	dailyEventUnreadDetails = nil

	// 在屏幕右侧区域查找红点
	overrideParamRedDot := map[string]any{
		"DailyEventRecognitionRedDot": map[string]any{
			"roi": maa.Rect{-800, 0, 800, 720},
		},
	}
	detail, err := ctx.RunRecognition("DailyEventRecognitionRedDot", arg.Img, overrideParamRedDot)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run TemplateMatch for RedDot on right side")
		return nil, false
	}
	if detail == nil || !detail.Hit || detail.Results == nil || len(detail.Results.Filtered) == 0 {
		log.Info().Msg("No red dot found on right side")
		return nil, false
	}

	// 遍历所有红点，过滤掉左侧包含"前往"的按钮，收集有效红点位置
	for _, result := range detail.Results.Filtered {
		tmResult, ok := result.AsTemplateMatch()
		if !ok {
			continue
		}

		redDotBox := tmResult.Box

		// 检测红点左侧是否包含"前往"，有则跳过
		overrideParamGotoButton := map[string]any{
			"DailyEventRecognitionGotoButton": map[string]any{
				"roi": maa.Rect{
					redDotBox.X() - 200,
					redDotBox.Y(),
					200,
					50},
			},
		}

		ocrDetail, err := ctx.RunRecognition("DailyEventRecognitionGotoButton", arg.Img, overrideParamGotoButton)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to run OCR for reward text")
			continue
		}
		if ocrDetail != nil && ocrDetail.Hit {
			log.Debug().Interface("box", redDotBox).Msg("Found '前往' text, skipping this red dot")
			continue
		}
		if err != nil {
			log.Warn().Err(err).Msg("Failed to run OCR for reward text")
			continue
		}
		if ocrDetail != nil && ocrDetail.Hit {
			log.Debug().Interface("box", redDotBox).Msg("Found '前往' text, skipping this red dot")
			continue
		}

		clickBox := maa.Rect{
			redDotBox.X() - 20,
			redDotBox.Y() + 20,
			5,
			5,
		}
		dailyEventUnreadDetails = append(dailyEventUnreadDetails, dailyEventUnreadDetail{
			Box: clickBox,
		})
		log.Debug().
			Interface("redDotBox", redDotBox).
			Interface("clickBox", clickBox).
			Msg("Found claimable reward")
	}

	if len(dailyEventUnreadDetails) == 0 {
		log.Info().Msg("No claimable rewards found after filtering")
		return nil, false
	}

	log.Info().Int("count", len(dailyEventUnreadDetails)).Msg("Unread details initialized")
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "init unread details"}`,
	}, true
}

type DailyEventUnreadDetailPickRecognition struct{}

func (r *DailyEventUnreadDetailPickRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if len(dailyEventUnreadDetails) == 0 {
		return nil, false
	}

	// 取出第一个红点位置
	item := dailyEventUnreadDetails[0]
	dailyEventUnreadDetails = dailyEventUnreadDetails[1:]

	log.Debug().
		Interface("box", item.Box).
		Int("remaining", len(dailyEventUnreadDetails)).
		Msg("Pick unread detail")

	return &maa.CustomRecognitionResult{
		Box:    item.Box,
		Detail: `{"custom": "pick unread detail"}`,
	}, true
}
