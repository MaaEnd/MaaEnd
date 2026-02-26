package resell

import (
	"fmt"
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

// ocrExtractNumberWithCenter - OCR region using pipeline name and return number with center coordinates
func ocrExtractNumberWithCenter(ctx *maa.Context, controller *maa.Controller, pipelineName string) (int, int, int, bool) {
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().
			Err(err).
			Msg("[OCR] 截图失败")
		return 0, 0, 0, false
	}
	if img == nil {
		log.Info().Msg("[OCR] 截图失败")
		return 0, 0, 0, false
	}

	// 使用 RunRecognition 调用预定义的 pipeline 节点
	detail, err := ctx.RunRecognition(pipelineName, img, nil)
	if err != nil {
		log.Error().
			Err(err).
			Msg("[OCR] 识别失败")
		return 0, 0, 0, false
	}
	if detail == nil || detail.Results == nil {
		log.Info().Str("pipeline", pipelineName).Msg("[OCR] 区域无结果")
		return 0, 0, 0, false
	}

	// 优先从 Best 结果中提取，然后是 All
	for _, results := range [][]*maa.RecognitionResult{{detail.Results.Best}, detail.Results.All} {
		if len(results) > 0 && results[0] != nil {
			if ocrResult, ok := results[0].AsOCR(); ok {
				if num, success := extractNumbersFromText(ocrResult.Text); success {
					// 计算中心坐标
					centerX := ocrResult.Box.X() + ocrResult.Box.Width()/2
					centerY := ocrResult.Box.Y() + ocrResult.Box.Height()/2
					log.Info().Str("pipeline", pipelineName).Str("originText", ocrResult.Text).Int("num", num).Msg("[OCR] 区域找到数字")
					return num, centerX, centerY, success
				}
			}
		}
	}

	return 0, 0, 0, false
}

// ocrExtractTextWithCenter - OCR region using pipeline name and check if pipeline filtered results exist, return center coordinates.
// Keyword matching is delegated to the pipeline's "expected" field, so no redundant check is needed in Go.
func ocrExtractTextWithCenter(ctx *maa.Context, controller *maa.Controller, pipelineName string) (bool, int, int, bool) {
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().
			Err(err).
			Msg("[OCR] 未能获取截图")
		return false, 0, 0, false
	}
	if img == nil {
		log.Info().Msg("[OCR] 未能获取截图")
		return false, 0, 0, false
	}

	// 使用 RunRecognition 调用预定义的 pipeline 节点
	detail, err := ctx.RunRecognition(pipelineName, img, nil)
	if err != nil {
		log.Error().
			Err(err).
			Msg("[OCR] 识别失败")
		return false, 0, 0, false
	}
	if detail == nil || detail.Results == nil {
		log.Info().Str("pipeline", pipelineName).Msg("[OCR] 区域无对应字符")
		return false, 0, 0, false
	}

	// Pipeline 的 expected 字段已负责文本过滤，Filtered 非空即表示匹配成功
	if len(detail.Results.Filtered) > 0 && detail.Results.Filtered[0] != nil {
		if ocrResult, ok := detail.Results.Filtered[0].AsOCR(); ok {
			centerX := ocrResult.Box.X() + ocrResult.Box.Width()/2
			centerY := ocrResult.Box.Y() + ocrResult.Box.Height()/2
			log.Info().Str("pipeline", pipelineName).Str("originText", ocrResult.Text).Msg("[OCR] 区域找到对应字符")
			return true, centerX, centerY, true
		}
	}

	log.Info().Str("pipeline", pipelineName).Msg("[OCR] 区域无对应字符")
	return false, 0, 0, false
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

func ResellDelayFreezesTime(ctx *maa.Context, time int) bool {
	ctx.RunTask("ResellTaskDelay", map[string]interface{}{
		"ResellTaskDelay": map[string]interface{}{
			"pre_wait_freezes": time,
		},
	},
	)
	return true
}

func extractOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.Results == nil {
		return ""
	}
	for _, results := range [][]*maa.RecognitionResult{{detail.Results.Best}, detail.Results.All} {
		if len(results) > 0 && results[0] != nil {
			if ocrResult, ok := results[0].AsOCR(); ok && ocrResult.Text != "" {
				return ocrResult.Text
			}
		}
	}
	return ""
}

// ocrAndParseQuota - OCR and parse quota from two regions
// Region 1 [180, 135, 75, 30]: "x/y" format (current/total quota)
// Region 2 [250, 130, 110, 30]: "a小时后+b" or "a分钟后+b" format (time + increment)
// Returns: x (current), y (max), hoursLater (0 for minutes, actual hours for hours), b (to be added)
func ocrAndParseQuota(ctx *maa.Context, controller *maa.Controller) (x int, y int, hoursLater int, b int) {
	x = -1
	y = -1
	hoursLater = -1
	b = -1

	img, err := controller.CacheImage()
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to get screenshot for quota OCR")
		return x, y, hoursLater, b
	}
	if img == nil {
		log.Error().Msg("Failed to get screenshot for quota OCR")
		return x, y, hoursLater, b
	}

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
