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
		log.Error().Msg("Failed to get controller")
		log.Info().Msg("\n[Resell] Get controller failed\n")
		return false
	}

	// Define three rows with different Y coordinates
	roiRows := []int{354, 484, 571}
	rowNames := []string{"Row1", "Row2", "Row3"}

	// Process multiple items by scanning across ROI
	records := make([]ProfitRecord, 0)
	maxProfit := 0

	// For each row
	for rowIdx, roiY := range roiRows {
		log.Info().Msgf("\n========== Processing %s (Y: %d) ==========\n", rowNames[rowIdx], roiY)

		// Start with base ROI x coordinate
		currentROIX := 72
		maxROIX := 1200 // Reasonable upper limit to prevent infinite loops
		stepCounter := 0

		for currentROIX < maxROIX {
			log.Info().Msgf("--- Processing ROI X: %d, Y: %d ---", currentROIX, roiY)

			// Step 1: 识别商品价格
			log.Info().Msg("Step 1 - Requesting screenshot")
			stepCounter++
			time.Sleep(1000 * time.Millisecond)
			controller.PostScreencap().Wait()

			costPrice, success := ocrExtractNumber(ctx, controller, currentROIX, roiY, 141, 31)
			if !success {
				log.Info().Msgf("Region 1 (Cost): No digit found at X=%d, Y=%d, switching to next row", currentROIX, roiY)
				break
			}
			log.Info().Msgf("Step 1 - Cost Price: %d", costPrice)

			// Click on region 1
			centerX := currentROIX + 141/2
			centerY := roiY + 31/2
			controller.PostClick(int32(centerX), int32(centerY))

			// Step 2: 识别“查看好友价格”，包含“好友”二字则继续
			log.Info().Msg("Step 2 - Requesting screenshot")
			time.Sleep(500 * time.Millisecond)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 944, 446, 98, 26, "好友")
			if !success {
				log.Info().Msg("Step 2 - Friend indicator: Not found, skipping")
				currentROIX += 150
				continue
			}
			log.Info().Msg("Step 2 - Friend indicator: Found")

			// 单击“查看好友价格”按钮
			controller.PostClick(944+98/2, 446+26/2)

			// Step 3: 检查好友列表第一位的出售价，即最高价格
			log.Info().Msg("Step 3 - Requesting screenshot")
			//等两秒加载好友价格
			time.Sleep(2000 * time.Millisecond)
			controller.PostScreencap().Wait()

			salePrice, success := ocrExtractNumber(ctx, controller, 795, 297, 54, 24)
			if !success {
				log.Info().Msg("Step 3 - Sale Price: No digit found, skipping")
				currentROIX += 150
				continue
			}
			log.Info().Msgf("Step 3 - Sale Price: %d", salePrice)
			// 计算利润
			profit := salePrice - costPrice
			log.Info().Msgf("Profit: %d (Sale: %d - Cost: %d)", profit, salePrice, costPrice)

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

			// Step 4: 检查页面右上角的“返回”按钮，点击返回
			log.Info().Msg("Step 4 - Requesting screenshot")
			time.Sleep(500 * time.Millisecond)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 1039, 135, 47, 21, "返回")
			if success {
				log.Info().Msg("Step 4 - Return button: Found, clicking")
				controller.PostClick(1039+47/2, 135+21/2)
			}

			// Step 5: 识别“查看好友价格”，包含“好友”二字则点击右上角的“X”关闭页面
			log.Info().Msg("Step 5 - Requesting screenshot")
			time.Sleep(500 * time.Millisecond)
			controller.PostScreencap().Wait()

			success = ocrExtractText(ctx, controller, 944, 446, 98, 26, "好友")
			if success {
				log.Info().Msg("Step 6 - Template match: Matched and clicked")
				controller.PostClick(1067+26/2, 135+23/2)
			}

			// 移动到下一列（ROI X增加150）
			currentROIX += 150
		}
	}

	// Output results
	fmt.Printf("\n========== 识别完成 ==========\n")
	fmt.Printf("总共识别到%d件商品\n", len(records))
	for i, record := range records {
		fmt.Printf("[%d] 行: %d, 列: %d, 成本: %d, 售价: %d, 利润: %d\n",
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
			fmt.Printf("\n当前利润最高商品:第%d行, 第%d列，利润%d\n", maxRecord.Row, maxRecord.Col, maxRecord.Profit)
			taskName := fmt.Sprintf("ResellSelectProductRow%dCol%d", maxRecord.Row, maxRecord.Col)
			ctx.OverrideNext(arg.CurrentTaskName, []string{taskName})
		} else {
			fmt.Printf("\n没有利润超过%d的商品，建议把配额留至明天\n", MinimumProfit)
			fmt.Printf("\n当前利润最高商品:第%d行, 第%d列，利润%d\n", maxRecord.Row, maxRecord.Col, maxRecord.Profit)
		}
	} else {
		fmt.Printf("\n出现错误\n", maxProfit)
	}
	fmt.Printf("===================================\n")
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
		log.Info().Msg("\n[OCR] Failed to get screenshot\n")
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
		log.Info().Msgf("  [OCR] Region [%d, %d, %d, %d] - No result\n", x, y, width, height)
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
								log.Info().Msgf("  [OCR] Region [%d, %d, %d, %d] - Found: %s -> %d\n", x, y, width, height, text, num)
								return num, true
							}
						}
					}
				}
			case map[string]interface{}:
				if text, ok := v["text"].(string); ok {
					// Try to extract numbers from the text
					if num, success := extractNumbersFromText(text); success {
						log.Info().Msgf("  [OCR] Region [%d, %d, %d, %d] - Found: %s -> %d\n", x, y, width, height, text, num)
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
		log.Info().Msg("\n[OCR] Failed to get screenshot\n")
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
		log.Info().Msgf("  [OCR] Region [%d, %d, %d, %d] - No result (keyword: %s)\n", x, y, width, height, keyword)
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
								log.Info().Msgf("  [OCR] Region [%d, %d, %d, %d] - Found: %s (keyword: %s)\n", x, y, width, height, text, keyword)
								return true
							}
						}
					}
				}
			case map[string]interface{}:
				if text, ok := v["text"].(string); ok {
					if containsKeyword(text, keyword) {
						log.Info().Msgf("  [OCR] Region [%d, %d, %d, %d] - Found: %s (keyword: %s)\n", x, y, width, height, text, keyword)
						return true
					}
				}
			}
		}
	}

	log.Info().Msgf("  [OCR] Region [%d, %d, %d, %d] - Not found (keyword: %s)\n", x, y, width, height, keyword)
	return false
}

// containsKeyword - Check if text contains keyword
func containsKeyword(text, keyword string) bool {
	return regexp.MustCompile(keyword).MatchString(text)
}

// ResellFinishAction - Finish Resell task custom action
type ResellFinishAction struct{}

func (a *ResellFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("Resell task completed")
	log.Info().Msg("\n[Resell] Task completed\n")
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
