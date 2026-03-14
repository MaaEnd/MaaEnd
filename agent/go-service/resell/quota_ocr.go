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
	if text := recognizeText(ctx, img, "ResellROIQuotaCurrent"); text != "" {
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
	if text := recognizeText(ctx, img, "ResellROIQuotaNextAddHours"); text != "" {
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
	if text := recognizeText(ctx, img, "ResellROIQuotaNextAddMinutes"); text != "" {
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
	if text := recognizeText(ctx, img, "ResellROIQuotaNextAddFallback"); text != "" {
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

// recognizeText 运行一个 OCR 识别节点，并返回首个非空 OCR 文本。
func recognizeText(ctx *maa.Context, img image.Image, nodeName string) string {
	if ctx == nil {
		log.Error().
			Str("node", nodeName).
			Msg("recognition context is nil")
		return ""
	}
	if img == nil {
		log.Error().
			Str("node", nodeName).
			Msg("recognition image is nil")
		return ""
	}

	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil {
		log.Error().
			Err(err).
			Str("node", nodeName).
			Msg("failed to run recognition")
		return ""
	}

	text := extractOCRText(detail)
	if text == "" {
		log.Debug().
			Str("node", nodeName).
			Msg("recognition returned empty OCR text")
		return ""
	}

	log.Info().
		Str("node", nodeName).
		Str("text", text).
		Msg("recognition OCR text")
	return text
}
