package resell

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"

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
	log.Info().Msg("Resell initialization action triggered")
	var params struct {
		MinimumProfit int `json:"MinimumProfit"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("Failed to unmarshal params")
		return false
	}
	MinimumProfit := int(params.MinimumProfit)
	// Get controller
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[Resell]Failed to get controller")
		return false
	}

	// Define three rows with different Y coordinates
	roiRows := []int{354, 484, 571}
	rowNames := []string{"第一行", "第二行", "第三行"}

	// Process multiple items by scanning across ROI
	records := make([]ProfitRecord, 0)
	maxProfit := 0

	// For each row
	for rowIdx, roiY := range roiRows {
		log.Info().Msgf("========== 当前处理 %s (Y: %d) ==========", rowNames[rowIdx], roiY)

		// Start with base ROI x coordinate
		currentROIX := 72
		maxROIX := 1200 // Reasonable upper limit to prevent infinite loops
		stepCounter := 0

		for currentROIX < maxROIX {
			log.Info().Msgf("--- Processing ROI X: %d, Y: %d ---", currentROIX, roiY)

			// Step 1: 识别商品价格
			log.Info().Msg("第一步：识别商品价格")
			stepCounter++
			delay_freezes_time(ctx, 400)
			controller.PostScreencap().Wait()

			costPrice, success := ocrExtractNumber(ctx, controller, currentROIX, roiY, 141, 31)
			if !success {
				log.Info().Msgf("没有在X=%d+141, Y=%d+31的区域内发现数字（代表没有商品）,切换至下一行", currentROIX, roiY)
				break
			}

			// Click on region 1
			centerX := currentROIX + 141/2
			centerY := roiY + 31/2
			controller.PostClick(int32(centerX), int32(centerY))

			// Step 2: 识别“查看好友价格”，包含“好友”二字则继续
			log.Info().Msg("第二步：查看好友价格")
			delay_freezes_time(ctx, 400)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 944, 446, 98, 26, "好友")
			if !success {
				log.Info().Msg("第二步：未找到“好友”字样")
				currentROIX += 150
				continue
			}
			//商品详情页右下角识别的成本价格为准
			controller.PostScreencap().Wait()
			ConfirmcostPrice, success := ocrExtractNumber(ctx, controller, 990, 490, 57, 27)
			costPrice = ConfirmcostPrice
			if !success {
				log.Info().Msg("第二步：未能识别商品详情页成本价格，继续使用列表页识别的价格")
			}
			log.Info().Msgf("第%d个商品售价: %d", stepCounter, costPrice)
			// 单击“查看好友价格”按钮
			controller.PostClick(944+98/2, 446+26/2)

			// Step 3: 检查好友列表第一位的出售价，即最高价格
			log.Info().Msg("第三步：识别好友出售价")
			//等两秒加载好友价格
			delay_freezes_time(ctx, 1000)
			controller.PostScreencap().Wait()

			salePrice, success := ocrExtractNumber(ctx, controller, 797, 294, 45, 28)
			if !success {
				log.Info().Msg("第三步：未能识别好友出售价，跳过该商品")
				currentROIX += 150
				continue
			}
			log.Info().Msgf("第三步：好友出售价: %d", salePrice)
			// 计算利润
			profit := salePrice - costPrice
			log.Info().Msgf("当前商品利润利润: %d (售价: %d - 成本: %d)", profit, salePrice, costPrice)

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
			log.Info().Msg("第四步：返回商品详情页")
			delay_freezes_time(ctx, 400)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 1039, 135, 47, 21, "返回")
			if success {
				log.Info().Msg("第四步：发现返回按钮，按ESC返回")
				controller.PostClickKey(27)
			}

			// Step 5: 识别“查看好友价格”，包含“好友”二字则按ESC关闭页面
			log.Info().Msg("第五步：关闭商品详情页")
			delay_freezes_time(ctx, 400)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 944, 446, 98, 26, "好友")
			if success {
				log.Info().Msg("第五步：关闭页面")
				controller.PostClickKey(27)
			}

			// 移动到下一列（ROI X增加150）
			currentROIX += 150
		}
	}

	// Output results using focus
	ShowMessage(ctx, "========== 识别完成 ==========")
	ShowMessage(ctx, fmt.Sprintf("总共识别到%d件商品", len(records)))
	for i, record := range records {
		log.Info().Msgf("[%d] 行: %d, 列: %d, 成本: %d, 售价: %d, 利润: %d",
			i+1, record.Row, record.Col, record.CostPrice, record.SalePrice, record.Profit)
	}

	// Find and output max profit item
	maxProfitIdx := -1
	for i, record := range records {
		if record.Profit == maxProfit {
			maxProfitIdx = i
			break
		}
	}

	var maxRecord ProfitRecord
	if maxProfitIdx >= 0 {
		maxRecord = records[maxProfitIdx]
		if maxRecord.Profit >= MinimumProfit {
			ShowMessage(ctx, fmt.Sprintf("当前利润最高商品:第%d行, 第%d列，利润%d", maxRecord.Row, maxRecord.Col, maxRecord.Profit))
			taskName := fmt.Sprintf("ResellSelectProductRow%dCol%d", maxRecord.Row, maxRecord.Col)
			ctx.OverrideNext(arg.CurrentTaskName, []string{taskName})
		} else {
			ShowMessage(ctx, fmt.Sprintf("没有利润超过%d的商品，建议把配额留至明天", MinimumProfit))
			ShowMessage(ctx, fmt.Sprintf("当前利润最高商品:第%d行, 第%d列，利润%d", maxRecord.Row, maxRecord.Col, maxRecord.Profit))
			controller.PostClickKey(27) //返回至地区管理界面
			ctx.OverrideNext(arg.CurrentTaskName, []string{"ChangeNextRegion"})
		}
	} else {
		log.Info().Msg("出现错误")
	}
	ShowMessage(ctx, "=============================")
	return true
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
		log.Info().Msg("[OCR] Failed to get screenshot")
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
		log.Info().Msgf("  [OCR] 区域 [%d, %d, %d, %d] - 无结果", x, y, width, height)
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
								log.Info().Msgf("  [OCR] 区域 [%d, %d, %d, %d] - 找到: %s -> %d", x, y, width, height, text, num)
								return num, true
							}
						}
					}
				}
			case map[string]interface{}:
				if text, ok := v["text"].(string); ok {
					// Try to extract numbers from the text
					if num, success := extractNumbersFromText(text); success {
						log.Info().Msgf("  [OCR] 区域 [%d, %d, %d, %d] - 找到: %s -> %d", x, y, width, height, text, num)
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
		log.Info().Msgf("  [OCR] 区域 [%d, %d, %d, %d] - 无结果 (keyword: %s)", x, y, width, height, keyword)
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
								log.Info().Msgf("  [OCR] 区域 [%d, %d, %d, %d] - 找到: %s (keyword: %s)", x, y, width, height, text, keyword)
								return true
							}
						}
					}
				}
			case map[string]interface{}:
				if text, ok := v["text"].(string); ok {
					if containsKeyword(text, keyword) {
						log.Info().Msgf("  [OCR] 区域 [%d, %d, %d, %d] - 找到: %s (keyword: %s)", x, y, width, height, text, keyword)
						return true
					}
				}
			}
		}
	}

	log.Info().Msgf("  [OCR] 区域 [%d, %d, %d, %d] - 无结果 (keyword: %s)", x, y, width, height, keyword)
	return false
}

// containsKeyword - Check if text contains keyword
func containsKeyword(text, keyword string) bool {
	return regexp.MustCompile(keyword).MatchString(text)
}

// ResellFinishAction - Finish Resell task custom action
type ResellFinishAction struct{}

func (a *ResellFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("[Resell] Task completed")
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

func ShowMessage(ctx *maa.Context, text string) bool {
	ctx.RunTask("Task", map[string]interface{}{
		"Task": map[string]interface{}{
			"focus": map[string]interface{}{
				"Node.Action.Starting": text,
			},
		},
	})
	return true
}

func delay_freezes_time(ctx *maa.Context, time int) bool {
	ctx.RunTask("Task", map[string]interface{}{
		"Task": map[string]interface{}{
			"pre_wait_freezes": time,
		},
	},
	)
	return true
}
