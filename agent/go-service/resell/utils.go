package resell

import (
	"encoding/json"
	"fmt"
	"image"
	"strconv"
	"strings"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func extractNumbersFromText(text string) (int, bool) {
	var digitsOnly []byte
	for i := 0; i < len(text); i++ {
		if text[i] >= '0' && text[i] <= '9' {
			digitsOnly = append(digitsOnly, text[i])
		}
	}
	if len(digitsOnly) > 0 {
		if num, err := strconv.Atoi(string(digitsOnly)); err == nil {
			return num, true
		}
	}
	return 0, false
}

// MoveMouseSafe moves the mouse to a safe location (10, 10) to avoid blocking OCR
func MoveMouseSafe(controller *maa.Controller) {
	// Use PostClick to move mouse to a safe corner
	// We use (10, 10) to avoid title bar buttons or window borders
	controller.PostTouchMove(0, 10, 10, 0)
	// Small delay to ensure mouse move completes
	time.Sleep(50 * time.Millisecond)
}

// ResellFinishAction - Finish Resell task custom action
type ResellFinishAction struct{}

func (a *ResellFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("[Resell]运行结束")
	return true
}

// ExecuteResellTask - Execute Resell main task
func ExecuteResellTask(tasker *maa.Tasker) error {
	if tasker == nil {
		return fmt.Errorf("tasker is nil")
	}

	if !tasker.Initialized() {
		return fmt.Errorf("tasker not initialized")
	}

	tasker.PostTask("ResellMain").Wait()

	return nil
}

func extractOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil {
		return ""
	}
	if detail.Results != nil {
		for _, results := range [][]*maa.RecognitionResult{
			{detail.Results.Best},
			detail.Results.Filtered,
			detail.Results.All,
		} {
			if len(results) > 0 && results[0] != nil {
				if ocrResult, ok := results[0].AsOCR(); ok && ocrResult.Text != "" {
					return ocrResult.Text
				}
			}
		}
	}
	if detail.DetailJson != "" {
		if text, _, _, _, _ := extractOCRFromDetailJson(detail.DetailJson); text != "" {
			return text
		}
	}
	return ""
}

// extractOCRFromDetailJson 从 Or/OCR 的 DetailJson 提取 text 和 box（用于 Or 识别时 Results 无直接 OCR 的兜底）
func extractOCRFromDetailJson(detailJson string) (text string, boxX, boxY, boxW, boxH int) {
	// 尝试 Or 结构：detail 为数组，首个子项含 detail.best
	var orStruct struct {
		Detail []struct {
			Detail struct {
				Best struct {
					Text string `json:"text"`
					Box  []int  `json:"box"`
				} `json:"best"`
			} `json:"detail"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(detailJson), &orStruct); err == nil && len(orStruct.Detail) > 0 {
		b := orStruct.Detail[0].Detail.Best
		if b.Text != "" && len(b.Box) >= 4 {
			return b.Text, b.Box[0], b.Box[1], b.Box[2], b.Box[3]
		}
	}
	// 尝试直接 OCR 结构：best 含 text 和 box
	var ocrStruct struct {
		Best struct {
			Text string `json:"text"`
			Box  []int  `json:"box"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(detailJson), &ocrStruct); err == nil && ocrStruct.Best.Text != "" && len(ocrStruct.Best.Box) >= 4 {
		b := ocrStruct.Best.Box
		return ocrStruct.Best.Text, b[0], b[1], b[2], b[3]
	}
	return "", 0, 0, 0, 0
}

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
			if val, ok := extractNumbersFromText(parts[0]); ok {
				x = val
			}
			if val, ok := extractNumbersFromText(parts[1]); ok {
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
			if val, ok := extractNumbersFromText(parts[0]); ok {
				hoursLater = val
			}
			if val, ok := extractNumbersFromText(parts[1]); ok {
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
			if val, ok := extractNumbersFromText(parts[1]); ok {
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
			if val, ok := extractNumbersFromText(parts[len(parts)-1]); ok {
				b = val
			}
			hoursLater = 0
			log.Info().Msgf("Parsed quota region 2 (fallback): b=%d", b)
		}
	}

	return x, y, hoursLater, b
}

func processMaxRecord(record ProfitRecord) ProfitRecord {
	result := record
	if result.Row >= 2 {
		result.Row = result.Row - 1
	}
	return result
}
