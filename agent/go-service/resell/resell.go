package resell

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// ProfitRecord stores profit information for each friend
type ProfitRecord struct {
	Row       int
	Col       int
	CostPrice int
	SalePrice int
	Profit    int
}

// ResellInitAction - Initialize Resell task custom action
type ResellInitAction struct{}

func (a *ResellInitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("[Resell]开始倒卖流程")
	var params struct {
		MinimumProfit interface{} `json:"MinimumProfit"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[Resell]反序列化失败")
		return false
	}

	// Parse MinimumProfit (support both string and int)
	var MinimumProfit int
	switch v := params.MinimumProfit.(type) {
	case float64:
		MinimumProfit = int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			log.Error().Err(err).Msgf("Failed to parse MinimumProfit string: %s", v)
			return false
		}
		MinimumProfit = parsed
	default:
		log.Error().Msgf("Invalid MinimumProfit type: %T", v)
		return false
	}

	fmt.Printf("MinimumProfit: %d\n", MinimumProfit)

	// Get controller
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[Resell]无法获取控制器")
		return false
	}

	overflowAmount := 0
	log.Info().Msg("Checking quota overflow status...")
	time.Sleep(500 * time.Millisecond)
	controller.PostScreencap().Wait()

	// OCR and parse quota from two regions
	x, y, _, b := ocrAndParseQuota(ctx, controller)
	if x >= 0 && y > 0 && b >= 0 {
		overflowAmount = x + b - y
	} else {
		log.Info().Msg("Failed to parse quota or no quota found, proceeding with normal flow")
	}

	// Define three rows with different Y coordinates
	roiRows := []int{360, 484, 567}
	rowNames := []string{"第一行", "第二行", "第三行"}

	// Process multiple items by scanning across ROI
	records := make([]ProfitRecord, 0)
	maxProfit := 0

	// For each row
	for rowIdx, roiY := range roiRows {
		log.Info().Str("行", rowNames[rowIdx]).Msg("[Resell]当前处理")
		// Start with base ROI x coordinate
		currentROIX := 72
		maxROIX := 1200 // Reasonable upper limit to prevent infinite loops
		stepCounter := 0

		for currentROIX < maxROIX {
			log.Info().Int("roiX", currentROIX).Int("roiY", roiY).Msg("[Resell]商品位置")
			// Step 1: 识别商品价格
			log.Info().Msg("[Resell]第一步：识别商品价格")
			stepCounter++
			Resell_delay_freezes_time(ctx, 200)
			controller.PostScreencap().Wait()

			costPrice, success := ocrExtractNumber(ctx, controller, currentROIX, roiY, 141, 40)
			if !success {
				log.Info().Int("roiX", currentROIX).Int("roiY", roiY).Msg("[Resell]位置无数字，说明无商品，下一行")
				break
			}

			// Click on region 1
			centerX := currentROIX + 141/2
			centerY := roiY + 31/2
			controller.PostClick(int32(centerX), int32(centerY))

			// Step 2: 识别“查看好友价格”，包含“好友”二字则继续
			log.Info().Msg("[Resell]第二步：查看好友价格")
			Resell_delay_freezes_time(ctx, 200)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 944, 446, 98, 26, "好友")
			if !success {
				log.Info().Msg("[Resell]第二步：未找到“好友”字样")
				currentROIX += 150
				continue
			}
			//商品详情页右下角识别的成本价格为准
			controller.PostScreencap().Wait()
			ConfirmcostPrice, success := ocrExtractNumber(ctx, controller, 990, 490, 57, 27)
			costPrice = ConfirmcostPrice
			if !success {
				log.Info().Msg("[Resell]第二步：未能识别商品详情页成本价格，继续使用列表页识别的价格")
			}
			log.Info().Int("No.", stepCounter).Int("Cost", costPrice).Msg("[Resell]商品售价")
			// 单击“查看好友价格”按钮
			controller.PostClick(944+98/2, 446+26/2)

			// Step 3: 检查好友列表第一位的出售价，即最高价格
			log.Info().Msg("[Resell]第三步：识别好友出售价")
			//等加载好友价格
			Resell_delay_freezes_time(ctx, 600)
			controller.PostScreencap().Wait()

			salePrice, success := ocrExtractNumber(ctx, controller, 797, 294, 45, 28)
			if !success {
				log.Info().Msg("[Resell]第三步：未能识别好友出售价，跳过该商品")
				currentROIX += 150
				continue
			}
			log.Info().Int("Price", salePrice).Msg("[Resell]好友出售价")
			// 计算利润
			profit := salePrice - costPrice
			log.Info().Int("Profit", profit).Msg("[Resell]当前商品利润")

			// 根据当前roiX位置计算列
			col := (currentROIX-72)/150 + 1

			// Save record with row and column information
			record := ProfitRecord{
				Row:       rowIdx + 1,
				Col:       col,
				CostPrice: costPrice,
				SalePrice: salePrice,
				Profit:    profit,
			}
			records = append(records, record)

			if profit > maxProfit {
				maxProfit = profit
			}

			// Step 4: 检查页面右上角的“返回”按钮，按ESC返回
			log.Info().Msg("[Resell]第四步：返回商品详情页")
			Resell_delay_freezes_time(ctx, 200)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 1039, 135, 47, 21, "返回")
			if success {
				log.Info().Msg("[Resell]第四步：发现返回按钮，按ESC返回")
				controller.PostClickKey(27)
			}

			// Step 5: 识别“查看好友价格”，包含“好友”二字则按ESC关闭页面
			log.Info().Msg("[Resell]第五步：关闭商品详情页")
			Resell_delay_freezes_time(ctx, 200)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 944, 446, 98, 26, "好友")
			if success {
				log.Info().Msg("[Resell]第五步：关闭页面")
				controller.PostClickKey(27)
			}

			// 移动到下一列（ROI X增加150）
			currentROIX += 150
		}
	}

	// Output results using focus
	for i, record := range records {
		log.Info().Int("No.", i+1).Int("列", record.Col).Int("成本", record.CostPrice).Int("售价", record.SalePrice).Int("利润", record.Profit).Msg("[Resell]商品信息")
	}

	// Check if sold out
	if len(records) == 0 {
		log.Info().Msg("库存已售罄，无可购买商品")
		ResellShowMessage(ctx, "⚠️ 库存已售罄，无可购买商品")
		return true
	}

	// Find and output max profit item
	maxProfitIdx := -1
	for i, record := range records {
		if record.Profit == maxProfit {
			maxProfitIdx = i
			break
		}
	}

	if maxProfitIdx < 0 {
		log.Error().Msg("未找到最高利润商品")
		return false
	}

	maxRecord := records[maxProfitIdx]
	log.Info().Msgf("最高利润商品: 第%d行第%d列，利润%d", maxRecord.Row, maxRecord.Col, maxRecord.Profit)

	// Check if we should purchase
	if overflowAmount > 0 {
		// Quota overflow detected, show reminder and recommend purchase
		log.Info().Msgf("配额溢出：建议购买%d件商品，推荐第%d行第%d列（利润：%d）",
			overflowAmount, maxRecord.Row, maxRecord.Col, maxRecord.Profit)

		// Show message with focus
		message := fmt.Sprintf("⚠️ 配额溢出提醒\n剩余配额明天将超出上限，建议购买%d件商品\n推荐购买: 第%d行第%d列 (最高利润: %d)",
			overflowAmount, maxRecord.Row, maxRecord.Col, maxRecord.Profit)
		ResellShowMessage(ctx, message)
		return true
	} else if maxRecord.Profit >= MinimumProfit {
		// Normal mode: purchase if meets minimum profit
		log.Info().Msgf("利润达标，准备购买第%d行第%d列商品（利润：%d）",
			maxRecord.Row, maxRecord.Col, maxRecord.Profit)
		taskName := fmt.Sprintf("ResellSelectProductRow%dCol%d", maxRecord.Row, maxRecord.Col)
		ctx.OverrideNext(arg.CurrentTaskName, []string{taskName})
		return true
	} else {
		// No profitable item, show recommendation
		log.Info().Msgf("没有达到最低利润%d的商品，推荐第%d行第%d列（利润：%d）",
			MinimumProfit, maxRecord.Row, maxRecord.Col, maxRecord.Profit)

		// Show message with focus
		message := fmt.Sprintf("💡 没有达到最低利润的商品，建议把配额留至明天\n推荐购买: 第%d行第%d列 (利润: %d)",
			maxRecord.Row, maxRecord.Col, maxRecord.Profit)
		ResellShowMessage(ctx, message)
		return true
	}
}

// extractNumbersFromText - Extract all digits from text and return as integer
func extractNumbersFromText(text string) (int, bool) {
	re := regexp.MustCompile(`\d+`)
	matches := re.FindAllString(text, -1)
	if len(matches) > 0 {
		// Concatenate all digit sequences found
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

// ocrExtractNumber - OCR region and extract first number found
func ocrExtractNumber(ctx *maa.Context, controller *maa.Controller, x, y, width, height int) (int, bool) {
	img := controller.CacheImage()
	if img == nil {
		log.Info().Msg("[OCR] 截图失败")
		return 0, false
	}

	ocrParam := &maa.NodeOCRParam{
		ROI:       maa.NewTargetRect(maa.Rect{x, y, width, height}),
		OrderBy:   "Expected",
		Expected:  []string{"[0-9]+"},
		Threshold: 0.3,
	}

	detail := ctx.RunRecognitionDirect(maa.NodeRecognitionTypeOCR, ocrParam, img)
	if detail == nil || detail.DetailJson == "" {
		log.Info().Int("x", x).Int("y", y).Int("width", width).Int("height", height).Msg("[OCR] 区域无结果")
		return 0, false
	}

	var rawResults map[string]interface{}
	err := json.Unmarshal([]byte(detail.DetailJson), &rawResults)
	if err != nil {
		log.Error().Err(err).Msg("Failed to parse OCR DetailJson")
		return 0, false
	}

	// Extract from "best" results first, then "all"
	for _, key := range []string{"best", "all"} {
		if data, ok := rawResults[key]; ok {
			switch v := data.(type) {
			case []interface{}:
				if len(v) > 0 {
					if result, ok := v[0].(map[string]interface{}); ok {
						if text, ok := result["text"].(string); ok {
							// Try to extract numbers from the text
							if num, success := extractNumbersFromText(text); success {
								log.Info().Int("x", x).Int("y", y).Int("width", width).Int("height", height).Str("originText", text).Int("num", num).Msg("[OCR] 区域找到数字")
								return num, true
							}
						}
					}
				}
			case map[string]interface{}:
				if text, ok := v["text"].(string); ok {
					// Try to extract numbers from the text
					if num, success := extractNumbersFromText(text); success {
						log.Info().Int("x", x).Int("y", y).Int("width", width).Int("height", height).Str("originText", text).Int("num", num).Msg("[OCR] 区域找到数字")
						return num, true
					}
				}
			}
		}
	}

	return 0, false
}

// ocrExtractText - OCR region and check if recognized text contains keyword
func ocrExtractText(ctx *maa.Context, controller *maa.Controller, x, y, width, height int, keyword string) bool {
	img := controller.CacheImage()
	if img == nil {
		log.Info().Msg("[OCR] 未能获取截图")
		return false
	}

	ocrParam := &maa.NodeOCRParam{
		ROI:       maa.NewTargetRect(maa.Rect{x, y, width, height}),
		OrderBy:   "Expected",
		Expected:  []string{".*"},
		Threshold: 0.8,
	}

	detail := ctx.RunRecognitionDirect(maa.NodeRecognitionTypeOCR, ocrParam, img)
	if detail == nil || detail.DetailJson == "" {
		log.Info().Int("x", x).Int("y", y).Int("width", width).Int("height", height).Str("keyword", keyword).Msg("[OCR] 区域无对应字符")
		return false
	}

	var rawResults map[string]interface{}
	err := json.Unmarshal([]byte(detail.DetailJson), &rawResults)
	if err != nil {
		return false
	}

	// Check filtered results first, then best results
	for _, key := range []string{"filtered", "best", "all"} {
		if data, ok := rawResults[key]; ok {
			switch v := data.(type) {
			case []interface{}:
				if len(v) > 0 {
					if result, ok := v[0].(map[string]interface{}); ok {
						if text, ok := result["text"].(string); ok {
							if containsKeyword(text, keyword) {
								log.Info().Int("x", x).Int("y", y).Int("width", width).Int("height", height).Str("originText", text).Str("keyword", keyword).Msg("[OCR] 区域找到对应字符")
								return true
							}
						}
					}
				}
			case map[string]interface{}:
				if text, ok := v["text"].(string); ok {
					if containsKeyword(text, keyword) {
						log.Info().Int("x", x).Int("y", y).Int("width", width).Int("height", height).Str("originText", text).Str("keyword", keyword).Msg("[OCR] 区域找到对应字符")
						return true
					}
				}
			}
		}
	}

	log.Info().Int("x", x).Int("y", y).Int("width", width).Int("height", height).Str("keyword", keyword).Msg("[OCR] 区域无对应字符")
	return false
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
	ctx.RunTask("[Resell]TaskDelay", map[string]interface{}{
		"[Resell]TaskDelay": map[string]interface{}{
			"pre_wait_freezes": time,
		},
	},
	)
	return true
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

	img := controller.CacheImage()
	if img == nil {
		log.Error().Msg("Failed to get screenshot for quota OCR")
		return x, y, hoursLater, b
	}

	// OCR region 1: [180, 135, 75, 30] to get "x/y"
	ocrParam1 := &maa.NodeOCRParam{
		ROI:       maa.NewTargetRect(maa.Rect{180, 135, 75, 30}),
		OrderBy:   "Expected",
		Expected:  []string{".*"},
		Threshold: 0.3,
	}

	detail1 := ctx.RunRecognitionDirect(maa.NodeRecognitionTypeOCR, ocrParam1, img)
	if detail1 != nil && detail1.DetailJson != "" {
		var rawResults1 map[string]interface{}
		if err := json.Unmarshal([]byte(detail1.DetailJson), &rawResults1); err == nil {
			for _, key := range []string{"best", "all"} {
				if data, ok := rawResults1[key]; ok {
					if text := extractTextFromOCRResult(data); text != "" {
						log.Info().Msgf("Quota region 1 OCR: %s", text)
						// Parse "x/y" format
						re := regexp.MustCompile(`(\d+)/(\d+)`)
						if matches := re.FindStringSubmatch(text); len(matches) >= 3 {
							x, _ = strconv.Atoi(matches[1])
							y, _ = strconv.Atoi(matches[2])
							log.Info().Msgf("Parsed quota region 1: x=%d, y=%d", x, y)
						}
						break
					}
				}
			}
		}
	}

	// OCR region 2: [250, 130, 110, 30] to get "a小时后+b" or "a分钟后+b"
	ocrParam2 := &maa.NodeOCRParam{
		ROI:       maa.NewTargetRect(maa.Rect{250, 130, 110, 30}),
		OrderBy:   "Expected",
		Expected:  []string{".*"},
		Threshold: 0.3,
	}

	detail2 := ctx.RunRecognitionDirect(maa.NodeRecognitionTypeOCR, ocrParam2, img)
	if detail2 != nil && detail2.DetailJson != "" {
		var rawResults2 map[string]interface{}
		if err := json.Unmarshal([]byte(detail2.DetailJson), &rawResults2); err == nil {
			for _, key := range []string{"best", "all"} {
				if data, ok := rawResults2[key]; ok {
					if text := extractTextFromOCRResult(data); text != "" {
						log.Info().Msgf("Quota region 2 OCR: %s", text)
						// Try pattern with hours
						reHours := regexp.MustCompile(`(\d+)\s*小时.*?[+]\s*(\d+)`)
						if matches := reHours.FindStringSubmatch(text); len(matches) >= 3 {
							hoursLater, _ = strconv.Atoi(matches[1])
							b, _ = strconv.Atoi(matches[2])
							log.Info().Msgf("Parsed quota region 2 (hours): hoursLater=%d, b=%d", hoursLater, b)
							break
						}
						// Try pattern with minutes
						reMinutes := regexp.MustCompile(`(\d+)\s*分钟.*?[+]\s*(\d+)`)
						if matches := reMinutes.FindStringSubmatch(text); len(matches) >= 3 {
							b, _ = strconv.Atoi(matches[2])
							hoursLater = 0
							log.Info().Msgf("Parsed quota region 2 (minutes): b=%d", b)
							break
						}
						// Fallback: just find "+b"
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
	}

	return x, y, hoursLater, b
}

// extractTextFromOCRResult - Extract text string from OCR result data
func extractTextFromOCRResult(data interface{}) string {
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
	return ""
}

// ResellShowMessage - Show message to user with focus
func ResellShowMessage(ctx *maa.Context, text string) bool {
	ctx.RunTask("[Resell]TaskShowMessage", map[string]interface{}{
		"[Resell]TaskShowMessage": map[string]interface{}{
			"recognition": "DirectHit",
			"action":      "DoNothing",
			"focus": map[string]interface{}{
				"Node.Action.Starting": text,
			},
		},
	})
	return true
}
