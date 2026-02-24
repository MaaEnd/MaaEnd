package resell

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// extractNumbersFromText - Extract all digits from text and return as integer
func extractNumbersFromText(text string) (int, bool) {
	re := regexp.MustCompile(`\d+`)
	matches := re.FindAllString(text, -1)
	if len(matches) > 0 {
		digitsOnly := ""
		for _, match := range matches {
			digitsOnly += match
		}
		if num, err := strconv.Atoi(digitsOnly); err == nil {
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

// ocrExtractNumberWithCenter - OCR region using pipeline name and return number with center coordinates.
// Pipeline nodes use Custom + ImageBinarization, which returns CustomRecognitionResult
// with OCR text in the Detail field.
func ocrExtractNumberWithCenter(ctx *maa.Context, controller *maa.Controller, pipelineName string) (int, int, int, bool) {
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("[OCR] 截图失败")
		return 0, 0, 0, false
	}
	if img == nil {
		log.Info().Msg("[OCR] 截图失败")
		return 0, 0, 0, false
	}

	// 使用 RunRecognition 调用预定义的 pipeline 节点
	detail, err := ctx.RunRecognition(pipelineName, img, nil)
	if err != nil {
		log.Error().Err(err).Msg("[OCR] 识别失败")
		return 0, 0, 0, false
	}
	if detail == nil || detail.Results == nil {
		log.Info().Str("pipeline", pipelineName).Msg("[OCR] 区域无结果")
		return 0, 0, 0, false
	}

	// 优先从 Best 结果中提取，然后是 All
	for _, results := range [][]*maa.RecognitionResult{detail.Results.Best, detail.Results.All} {
		if len(results) > 0 {
			if customResult, ok := results[0].AsCustom(); ok {
				if num, success := extractNumbersFromText(customResult.Detail); success {
					// 计算中心坐标
					centerX := customResult.Box.X() + customResult.Box.Width()/2
					centerY := customResult.Box.Y() + customResult.Box.Height()/2
					log.Info().Str("pipeline", pipelineName).Str("originText", customResult.Detail).Int("num", num).Msg("[OCR] 区域找到数字")
					if num >= 7000 || num <= 100 {
						log.Info().Str("pipeline", pipelineName).Str("originText", customResult.Detail).Int("num", num).Msg("[OCR] 数字不合理，抛弃")
						success = false
						if num >= 10000 {
							adjustedNum := num % 10000
							log.Info().Str("pipeline", pipelineName).Str("originText", customResult.Detail).Int("originalNum", num).Int("adjustedNum", adjustedNum).Msg("[OCR] 数字>=10000，已截取后四位")
							num = adjustedNum
							success = true
						}
					}
					return num, centerX, centerY, success
				}
			}
		}
	}

	return 0, 0, 0, false
}

// ocrExtractTextWithCenter - OCR region using pipeline name and check if recognized
// text contains keyword, return center coordinates.
func ocrExtractTextWithCenter(ctx *maa.Context, controller *maa.Controller, pipelineName string, keyword string) (bool, int, int, bool) {
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().Err(err).Msg("[OCR] 未能获取截图")
		return false, 0, 0, false
	}
	if img == nil {
		log.Info().Msg("[OCR] 未能获取截图")
		return false, 0, 0, false
	}

	detail, err := ctx.RunRecognition(pipelineName, img, nil)
	if err != nil {
		log.Error().Err(err).Msg("[OCR] 识别失败")
		return false, 0, 0, false
	}
	if detail == nil || detail.Results == nil {
		log.Info().Str("pipeline", pipelineName).Str("keyword", keyword).Msg("[OCR] 区域无对应字符")
		return false, 0, 0, false
	}

	// 优先从 Filtered 结果中提取，然后是 Best、All
	for _, results := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.Best, detail.Results.All} {
		if len(results) > 0 {
			if customResult, ok := results[0].AsCustom(); ok {
				if containsKeyword(customResult.Detail, keyword) {
					// 计算中心坐标
					centerX := customResult.Box.X() + customResult.Box.Width()/2
					centerY := customResult.Box.Y() + customResult.Box.Height()/2
					log.Info().Str("pipeline", pipelineName).Str("originText", customResult.Detail).Str("keyword", keyword).Msg("[OCR] 区域找到对应字符")
					return true, centerX, centerY, true
				}
			}
		}
	}

	log.Info().Str("pipeline", pipelineName).Str("keyword", keyword).Msg("[OCR] 区域无对应字符")
	return false, 0, 0, false
}

// containsKeyword - Check if text contains keyword
func containsKeyword(text, keyword string) bool {
	return regexp.MustCompile(keyword).MatchString(text)
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

func Resell_delay_freezes_time(ctx *maa.Context, time int) bool {
	ctx.RunTask("Resell_TaskDelay", map[string]interface{}{
		"Resell_TaskDelay": map[string]interface{}{
			"pre_wait_freezes": time,
		},
	},
	)
	return true
}

// ocrAndParseQuota - OCR and parse quota from two regions.
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
		log.Error().Err(err).Msg("Failed to get screenshot for quota OCR")
		return x, y, hoursLater, b
	}
	if img == nil {
		log.Error().Msg("Failed to get screenshot for quota OCR")
		return x, y, hoursLater, b
	}

	// OCR region 1: 使用预定义的配额当前值Pipeline
	detail1, err := ctx.RunRecognition("Resell_ROI_Quota_Current", img, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for region 1")
		return x, y, hoursLater, b
	}
	if detail1 != nil && detail1.Results != nil {
		for _, results := range [][]*maa.RecognitionResult{detail1.Results.Best, detail1.Results.All} {
			if len(results) > 0 {
				if customResult, ok := results[0].AsCustom(); ok && customResult.Detail != "" {
					log.Info().Msgf("Quota region 1 OCR: %s", customResult.Detail)
					// Parse "x/y" format
					re := regexp.MustCompile(`(\d+)/(\d+)`)
					if matches := re.FindStringSubmatch(customResult.Detail); len(matches) >= 3 {
						x, _ = strconv.Atoi(matches[1])
						y, _ = strconv.Atoi(matches[2])
						log.Info().Msgf("Parsed quota region 1: x=%d, y=%d", x, y)
					}
					break
				}
			}
		}
	}

	// OCR region 2: 使用预定义的配额下次增加Pipeline
	detail2, err := ctx.RunRecognition("Resell_ROI_Quota_NextAdd", img, nil)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run recognition for region 2")
		return x, y, hoursLater, b
	}
	if detail2 != nil && detail2.Results != nil {
		for _, results := range [][]*maa.RecognitionResult{detail2.Results.Best, detail2.Results.All} {
			if len(results) > 0 {
				if customResult, ok := results[0].AsCustom(); ok && customResult.Detail != "" {
					text := customResult.Detail
					log.Info().Msgf("Quota region 2 OCR: %s", text)
					// Parse "a小时后+b" or "a分钟后+b" format
					reHours := regexp.MustCompile(`(\d+)\s*小时.*?[+]\s*(\d+)`)
					if matches := reHours.FindStringSubmatch(text); len(matches) >= 3 {
						hoursLater, _ = strconv.Atoi(matches[1])
						b, _ = strconv.Atoi(matches[2])
						log.Info().Msgf("Parsed quota region 2 (hours): hoursLater=%d, b=%d", hoursLater, b)
						break
					}
					// Parse "a分钟后+b" format
					reMinutes := regexp.MustCompile(`(\d+)\s*分钟.*?[+]\s*(\d+)`)
					if matches := reMinutes.FindStringSubmatch(text); len(matches) >= 3 {
						b, _ = strconv.Atoi(matches[2])
						hoursLater = 0
						log.Info().Msgf("Parsed quota region 2 (minutes): b=%d", b)
						break
					}
					// Parse fallback format
					reFallback := regexp.MustCompile(`[+]\s*(\d+)`)
					if matches := reFallback.FindStringSubmatch(text); len(matches) >= 2 {
						b, _ = strconv.Atoi(matches[1])
						hoursLater = 0
						log.Info().Msgf("Parsed quota region 2 (fallback): b=%d", b)
					}
					break
				}
			}
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
