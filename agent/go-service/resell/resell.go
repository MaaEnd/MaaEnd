package resell

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
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
	ResellDelayFreezesTime(ctx, 500)
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
			ResellDelayFreezesTime(ctx, 200)
			MoveMouseSafe(controller)

			// 构建Pipeline名称
			pricePipelineName := fmt.Sprintf("ResellROIProductRow%dCol%dPrice", rowIdx+1, col)
			costPrice, clickX, clickY, success := ocrExtractNumberWithCenter(ctx, controller, pricePipelineName)
			if !success {
				//失败就重试一遍
				MoveMouseSafe(controller)
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
			ResellDelayFreezesTime(ctx, 200)
			MoveMouseSafe(controller)

			_, friendBtnX, friendBtnY, success := ocrExtractTextWithCenter(ctx, controller, "ResellROIViewFriendPrice")
			if !success {
				log.Info().Msg("[Resell]第二步：未找到查看好友价格按钮")
				continue
			}
			//商品详情页右上角识别的成本价格为准
			MoveMouseSafe(controller)
			ConfirmcostPrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "ResellROIDetailCostPrice")
			if success {
				costPrice = ConfirmcostPrice
			} else {
				//失败就重试一遍
				MoveMouseSafe(controller)
				ConfirmcostPrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "ResellROIDetailCostPrice")
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
			// 等加载好友价格：Pipeline next 轮询 ResellROIFriendSalePrice / ResellROIFriendLoading
			if _, err := ctx.RunTask("ResellWaitFriendPrice", nil); err != nil {
				log.Info().Err(err).Msg("[Resell]第三步：未能识别好友出售价，跳过该商品")
				continue
			}
			MoveMouseSafe(controller)

			salePrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "ResellROIFriendSalePrice")
			if !success {
				//失败就重试一遍
				MoveMouseSafe(controller)
				salePrice, _, _, success = ocrExtractNumberWithCenter(ctx, controller, "ResellROIFriendSalePrice")
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
			ResellDelayFreezesTime(ctx, 200)
			MoveMouseSafe(controller)

			if _, err := ctx.RunTask("ResellROIReturnButton", nil); err != nil {
				log.Warn().Err(err).Msg("[Resell]第四步：返回按钮点击失败")
			} else {
				log.Info().Msg("[Resell]第四步：发现返回按钮，点击返回")
			}

			// Step 5: 识别商品详情页关闭按钮，直接点击关闭
			log.Info().Msg("[Resell]第五步：关闭商品详情页")
			ResellDelayFreezesTime(ctx, 200)
			MoveMouseSafe(controller)

			if _, err := ctx.RunTask("CloseButtonType1", nil); err != nil {
				log.Warn().Err(err).Msg("[Resell]第五步：关闭页面失败")
			} else {
				log.Info().Msg("[Resell]第五步：关闭页面")
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
		maafocus.NodeActionStarting(ctx, "⚠️ 库存已售罄，无可购买商品")
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
		maafocus.NodeActionStarting(ctx, message)
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
		maafocus.NodeActionStarting(ctx, message)
		//进入下个地区
		taskName := "ChangeNextRegionPrepare"
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{
			{Name: taskName},
		})
		return true
	}
}
