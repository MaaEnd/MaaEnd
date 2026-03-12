package resell

import (
	"image"
	"strings"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ocrAndParseQuotaFromImg - OCR and parse quota from two regions on given image
// Region 1 [180, 135, 75, 30]: "x/y" format (current/total quota)
// Region 2 [250, 130, 110, 30]: "a小时后+b" or "a分钟后+b" format (time + increment)
// Returns: x (current), y (max), hoursLater (0 for minutes, actual hours for hours), b (to be added)
func ocrAndParseQuota(ctx *maa.Context, img image.Image) (x int, y int, hoursLater int, b int) {
	x = -1
	y = -1
	hoursLater = -1
	b = -1

	// Region 1: 配额当前值 "x/y" 格式，由 Pipeline expected 过滤
	detail1, err := ctx.RunRecognition("ResellROIQuotaCurrent", img, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for region 1")
		return x, y, hoursLater, b
	}
	if text := extractOCRText(detail1); text != "" {
		log.Info().Msgf("Quota region 1 OCR: %s", text)
		parts := strings.Split(text, "/")
		if len(parts) >= 2 {
			if val, ok := extractIntegerFromText(parts[0]); ok {
				x = val
			}
			if val, ok := extractIntegerFromText(parts[1]); ok {
				y = val
			}
			log.Info().Msgf("Parsed quota region 1: x=%d, y=%d", x, y)
		}
	}

	// Region 2: 配额下次增加，依次尝试三个 Pipeline 节点（小时 / 分钟 / 兜底）
	// 尝试 "a小时后+b" 格式
	if detail2h, err := ctx.RunRecognition("ResellROIQuotaNextAddHours", img, nil); err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for region 2 (hours)")
	} else if text := extractOCRText(detail2h); text != "" {
		log.Info().Msgf("Quota region 2 OCR (hours): %s", text)
		parts := strings.Split(text, "+")
		if len(parts) >= 2 {
			if val, ok := extractIntegerFromText(parts[0]); ok {
				hoursLater = val
			}
			if val, ok := extractIntegerFromText(parts[1]); ok {
				b = val
			}
			log.Info().Msgf("Parsed quota region 2 (hours): hoursLater=%d, b=%d", hoursLater, b)
			return x, y, hoursLater, b
		}
	}

	// 尝试 "a分钟后+b" 格式
	if detail2m, err := ctx.RunRecognition("ResellROIQuotaNextAddMinutes", img, nil); err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for region 2 (minutes)")
	} else if text := extractOCRText(detail2m); text != "" {
		log.Info().Msgf("Quota region 2 OCR (minutes): %s", text)
		parts := strings.Split(text, "+")
		if len(parts) >= 2 {
			if val, ok := extractIntegerFromText(parts[1]); ok {
				b = val
			}
			hoursLater = 0
			log.Info().Msgf("Parsed quota region 2 (minutes): b=%d", b)
			return x, y, hoursLater, b
		}
	}

	// 兜底：仅匹配 "+b"
	if detail2f, err := ctx.RunRecognition("ResellROIQuotaNextAddFallback", img, nil); err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for region 2 (fallback)")
	} else if text := extractOCRText(detail2f); text != "" {
		log.Info().Msgf("Quota region 2 OCR (fallback): %s", text)
		parts := strings.Split(text, "+")
		if len(parts) >= 2 {
			if val, ok := extractIntegerFromText(parts[len(parts)-1]); ok {
				b = val
			}
			hoursLater = 0
			log.Info().Msgf("Parsed quota region 2 (fallback): b=%d", b)
		}
	}

	return x, y, hoursLater, b
}
