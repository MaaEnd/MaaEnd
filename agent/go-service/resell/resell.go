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
	log.Info().Msg("[Resell]开始倒卖流程")
	var params struct {
		MinimumProfit int `json:"MinimumProfit"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Msg("[Resell]反序列化失败")
		return false
	}
	MinimumProfit := int(params.MinimumProfit)
	// Get controller
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[Resell]无法获取控制器")
		return false
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
			ResellShowMessage(ctx, fmt.Sprintf("总共识别到%d件商品，当前利润最高商品:第%d行, 第%d列，利润%d", len(records), maxRecord.Row, maxRecord.Col, maxRecord.Profit))
			taskName := fmt.Sprintf("ResellSelectProductRow%dCol%d", maxRecord.Row, maxRecord.Col)
			ctx.OverrideNext(arg.CurrentTaskName, []string{taskName})
		} else {
			ResellShowMessage(ctx, fmt.Sprintf("总共识别到%d件商品,没有利润超过%d的商品，建议把配额留至明天,当前利润最高商品:第%d行, 第%d列，利润%d", len(records), MinimumProfit, maxRecord.Row, maxRecord.Col, maxRecord.Profit))
			controller.PostClickKey(27) //返回至地区管理界面
			ctx.OverrideNext(arg.CurrentTaskName, []string{"ChangeNextRegion"})
		}
	} else {
		log.Info().Msg("出现错误")
	}
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

func ResellShowMessage(ctx *maa.Context, text string) bool {
	ctx.RunTask("[Resell]TaskShowMessage", map[string]interface{}{
		"[Resell]TaskShowMessage": map[string]interface{}{
			"focus": map[string]interface{}{
				"Node.Action.Starting": text,
			},
		},
	})
	return true
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
