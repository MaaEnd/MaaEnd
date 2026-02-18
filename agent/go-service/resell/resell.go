package resell

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
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
	Resell_delay_freezes_time(ctx, 500)
	MoveMouseSafe(controller)
	controller.PostScreencap().Wait()

	// OCR and parse quota from two regions
	x, y, _, b := ocrAndParseQuota(ctx, controller)
	if x >= 0 && y > 0 && b >= 0 {
		overflowAmount = x + b - y
	} else {
		log.Info().Msg("Failed to parse quota or no quota found, proceeding with normal flow")
	}

	// The recognition areas for single-row and multi-row products are different, so they need to be handled separately
	rowNames := []string{"第一行", "第二行", "第三行"}
	maxCols := 8 // Maximum 8 columns per row

	// Process multiple items by scanning across ROI
	records := make([]ProfitRecord, 0)
	maxProfit := 0

	// For each row
	for rowIdx := 0; rowIdx < 3; rowIdx++ {
		log.Info().Str("行", rowNames[rowIdx]).Msg("[Resell]当前处理")

		// For each column
		for col := 1; col <= maxCols; col++ {
			log.Info().Int("行", rowIdx+1).Int("列", col).Msg("[Resell]商品位置")
			// Step 1: 识别商品价格
			log.Info().Msg("[Resell]第一步：识别商品价格")
			Resell_delay_freezes_time(ctx, 200)
			MoveMouseSafe(controller)
			controller.PostScreencap().Wait()

			// 构建Pipeline名称
			pricePipelineName := fmt.Sprintf("Resell_ROI_Product_Row%d_Col%d_Price", rowIdx+1, col)
			costPrice, clickX, clickY, success := ocrExtractNumberWithCenter(ctx, controller, pricePipelineName)
			if !success {
				//失败就重试一遍
				MoveMouseSafe(controller)
				controller.PostScreencap().Wait()
				costPrice, clickX, clickY, success = ocrExtractNumberWithCenter(ctx, controller, pricePipelineName)
				if !success {
					log.Info().Int("行", rowIdx+1).Int("列", col).Msg("[Resell]位置无数字，说明无商品，下一行")
					break
				}
			}

			// Click on product
			controller.PostClick(int32(clickX), int32(clickY))

			// Step 2: 识别“查看好友价格”，包含“好友”二字则继续
			log.Info().Msg("[Resell]第二步：查看好友价格")
			Resell_delay_freezes_time(ctx, 200)
			MoveMouseSafe(controller)
			controller.PostScreencap().Wait()

			_, friendBtnX, friendBtnY, success := ocrExtractTextWithCenter(ctx, controller, "Resell_ROI_ViewFriendPrice", "好友")
			if !success {
				log.Info().Msg("[Resell]第二步：未找到“好友”字样")
				continue
			}
			//商品详情页右下角识别的成本价格为准
			MoveMouseSafe(controller)
			controller.PostScreencap().Wait()
			ConfirmcostPrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "Resell_ROI_DetailCostPrice")
			if success {
				costPrice = ConfirmcostPrice
			} else {
				//失败就重试一遍
				MoveMouseSafe(controller)
				controller.PostScreencap().Wait()
				ConfirmcostPrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "Resell_ROI_DetailCostPrice")
				if success {
					costPrice = ConfirmcostPrice
				} else {
					log.Info().Msg("[Resell]第二步：未能识别商品详情页成本价格，继续使用列表页识别的价格")
				}
			}
			log.Info().Int("行", rowIdx+1).Int("列", col).Int("Cost", costPrice).Msg("[Resell]商品售价")
			// 单击"查看好友价格"按钮
			controller.PostClick(int32(friendBtnX), int32(friendBtnY))

			// Step 3: 检查好友列表第一位的出售价，即最高价格
			log.Info().Msg("[Resell]第三步：识别好友出售价")
			//等加载好友价格
			Resell_delay_freezes_time(ctx, 600)
			MoveMouseSafe(controller)
			controller.PostScreencap().Wait()

			salePrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "Resell_ROI_FriendSalePrice")
			if !success {
				//失败就重试一遍
				MoveMouseSafe(controller)
				controller.PostScreencap().Wait()
				salePrice, _, _, success = ocrExtractNumberWithCenter(ctx, controller, "Resell_ROI_FriendSalePrice")
				if !success {
					log.Info().Msg("[Resell]第三步：未能识别好友出售价，跳过该商品")
					continue
				}
			}
			log.Info().Int("Price", salePrice).Msg("[Resell]好友出售价")
			// 计算利润
			profit := salePrice - costPrice
			log.Info().Int("Profit", profit).Msg("[Resell]当前商品利润")

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
			MoveMouseSafe(controller)
			controller.PostScreencap().Wait()

			_, _, _, success = ocrExtractTextWithCenter(ctx, controller, "Resell_ROI_ReturnButton", "返回")
			if success {
				log.Info().Msg("[Resell]第四步：发现返回按钮，按ESC返回")
				controller.PostClickKey(27)
			}

			// Step 5: 识别“查看好友价格”，包含“好友”二字则按ESC关闭页面
			log.Info().Msg("[Resell]第五步：关闭商品详情页")
			Resell_delay_freezes_time(ctx, 200)
			MoveMouseSafe(controller)
			controller.PostScreencap().Wait()

			_, _, _, success = ocrExtractTextWithCenter(ctx, controller, "Resell_ROI_ViewFriendPrice", "好友")
			if success {
				log.Info().Msg("[Resell]第五步：关闭页面")
				controller.PostClickKey(27)
			}
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
	showMaxRecord := processMaxRecord(maxRecord)

	// Check if we should purchase
	if overflowAmount > 0 {
		// Quota overflow detected, show reminder and recommend purchase
		log.Info().Msgf("配额溢出：建议购买%d件商品，推荐第%d行第%d列（利润：%d）",
			overflowAmount, showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)

		// Show message with focus
		message := fmt.Sprintf("⚠️ 配额溢出提醒\n剩余配额明天将超出上限，建议购买%d件商品\n推荐购买: 第%d行第%d列 (最高利润: %d)",
			overflowAmount, showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
		ResellShowMessage(ctx, message)
		//进入下个地区
		taskName := "ChangeNextRegionPrepare"
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{
			{Name: taskName},
		})
		return true
	} else if maxRecord.Profit >= MinimumProfit {
		// Normal mode: purchase if meets minimum profit
		log.Info().Msgf("利润达标，准备购买第%d行第%d列商品（利润：%d）",
			showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
		taskName := fmt.Sprintf("ResellSelectProductRow%dCol%d", maxRecord.Row, maxRecord.Col)
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{
			{Name: taskName},
		})
		return true
	} else {
		// No profitable item, show recommendation
		log.Info().Msgf("没有达到最低利润%d的商品，推荐第%d行第%d列（利润：%d）",
			MinimumProfit, showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)

		// Show message with focus
		var message string
		if MinimumProfit >= 999999 {
			// Auto buy/sell is disabled (MinimumProfit set to 999999)
			message = fmt.Sprintf("💡 已禁用自动购买/出售\n推荐购买: 第%d行第%d列 (利润: %d)",
				showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
		} else {
			// Normal case: profit threshold not met
			message = fmt.Sprintf("💡 没有达到最低利润的商品，建议把配额留至明天\n推荐购买: 第%d行第%d列 (利润: %d)",
				showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
		}
		ResellShowMessage(ctx, message)
		//进入下个地区
		taskName := "ChangeNextRegionPrepare"
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{
			{Name: taskName},
		})
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
	for _, results := range [][]*maa.RecognitionResult{detail.Results.Best, detail.Results.All} {
		if len(results) > 0 {
			if ocrResult, ok := results[0].AsOCR(); ok {
				if num, success := extractNumbersFromText(ocrResult.Text); success {
					// 计算中心坐标
					centerX := ocrResult.Box.X() + ocrResult.Box.Width()/2
					centerY := ocrResult.Box.Y() + ocrResult.Box.Height()/2
					log.Info().Str("pipeline", pipelineName).Str("originText", ocrResult.Text).Int("num", num).Msg("[OCR] 区域找到数字")
					if num >= 7000 || num <= 100 {
						//数字不合理，抛弃
						log.Info().Str("pipeline", pipelineName).Str("originText", ocrResult.Text).Int("num", num).Msg("[OCR] 数字不合理，抛弃")
						success = false
						// 如果数字>=10000，则是误识别票券为1，只保留后四位，数据仍然可用
						if num >= 10000 {
							adjustedNum := num % 10000
							log.Info().Str("pipeline", pipelineName).Str("originText", ocrResult.Text).Int("originalNum", num).Int("adjustedNum", adjustedNum).Msg("[OCR] 数字>=10000，已截取后四位")
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

// ocrExtractTextWithCenter - OCR region using pipeline name and check if recognized text contains keyword, return center coordinates
func ocrExtractTextWithCenter(ctx *maa.Context, controller *maa.Controller, pipelineName string, keyword string) (bool, int, int, bool) {
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
		log.Info().Str("pipeline", pipelineName).Str("keyword", keyword).Msg("[OCR] 区域无对应字符")
		return false, 0, 0, false
	}

	// 优先从 Filtered 结果中提取，然后是 Best、All
	for _, results := range [][]*maa.RecognitionResult{detail.Results.Filtered, detail.Results.Best, detail.Results.All} {
		if len(results) > 0 {
			if ocrResult, ok := results[0].AsOCR(); ok {
				if containsKeyword(ocrResult.Text, keyword) {
					// 计算中心坐标
					centerX := ocrResult.Box.X() + ocrResult.Box.Width()/2
					centerY := ocrResult.Box.Y() + ocrResult.Box.Height()/2
					log.Info().Str("pipeline", pipelineName).Str("originText", ocrResult.Text).Str("keyword", keyword).Msg("[OCR] 区域找到对应字符")
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

	// OCR region 1: 使用预定义的配额当前值Pipeline
	detail1, err := ctx.RunRecognition("Resell_ROI_Quota_Current", img, nil)
	if err != nil {
		log.Error().
			Err(err).
			Msg("Failed to run recognition for region 1")
		return x, y, hoursLater, b
	}
	if detail1 != nil && detail1.Results != nil {
		for _, results := range [][]*maa.RecognitionResult{detail1.Results.Best, detail1.Results.All} {
			if len(results) > 0 {
				if ocrResult, ok := results[0].AsOCR(); ok && ocrResult.Text != "" {
					log.Info().Msgf("Quota region 1 OCR: %s", ocrResult.Text)
					// Parse "x/y" format
					re := regexp.MustCompile(`(\d+)/(\d+)`)
					if matches := re.FindStringSubmatch(ocrResult.Text); len(matches) >= 3 {
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
		log.Error().
			Err(err).
			Msg("Failed to run recognition for region 2")
		return x, y, hoursLater, b
	}
	if detail2 != nil && detail2.Results != nil {
		for _, results := range [][]*maa.RecognitionResult{detail2.Results.Best, detail2.Results.All} {
			if len(results) > 0 {
				if ocrResult, ok := results[0].AsOCR(); ok && ocrResult.Text != "" {
					log.Info().Msgf("Quota region 2 OCR: %s", ocrResult.Text)
					// Try pattern with hours
					reHours := regexp.MustCompile(`(\d+)\s*小时.*?[+]\s*(\d+)`)
					if matches := reHours.FindStringSubmatch(ocrResult.Text); len(matches) >= 3 {
						hoursLater, _ = strconv.Atoi(matches[1])
						b, _ = strconv.Atoi(matches[2])
						log.Info().Msgf("Parsed quota region 2 (hours): hoursLater=%d, b=%d", hoursLater, b)
						break
					}
					// Try pattern with minutes
					reMinutes := regexp.MustCompile(`(\d+)\s*分钟.*?[+]\s*(\d+)`)
					if matches := reMinutes.FindStringSubmatch(ocrResult.Text); len(matches) >= 3 {
						b, _ = strconv.Atoi(matches[2])
						hoursLater = 0
						log.Info().Msgf("Parsed quota region 2 (minutes): b=%d", b)
						break
					}
					// Fallback: just find "+b"
					reFallback := regexp.MustCompile(`[+]\s*(\d+)`)
					if matches := reFallback.FindStringSubmatch(ocrResult.Text); len(matches) >= 2 {
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

// ResellShowMessage - Show message to user with focus
func ResellShowMessage(ctx *maa.Context, text string) bool {
	ctx.RunTask("Resell_TaskShowMessage", map[string]interface{}{
		"Resell_TaskShowMessage": map[string]interface{}{
			"recognition": "DirectHit",
			"action":      "DoNothing",
			"focus": map[string]interface{}{
				"Node.Action.Starting": text,
			},
		},
	})
	return true
}

func processMaxRecord(record ProfitRecord) ProfitRecord {
	result := record
	if result.Row >= 2 {
		result.Row = result.Row - 1
	}
	return result
}
