package resell

import (
	"fmt"
	"regexp"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var scanNodeRe = regexp.MustCompile(`ResellScanRow(\d+)Col(\d+)`)

// ResellScanProductAction 扫描单个 (row,col) 商品，追加利润记录，跳转到下一格或决策节点
type ResellScanProductAction struct{}

func (a *ResellScanProductAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	matches := scanNodeRe.FindStringSubmatch(arg.CurrentTaskName)
	if len(matches) != 3 {
		log.Error().Str("task", arg.CurrentTaskName).Msg("[Resell]无法解析扫描节点名")
		return false
	}
	rowIdx := 0
	col := 0
	fmt.Sscanf(matches[1], "%d", &rowIdx)
	fmt.Sscanf(matches[2], "%d", &col)

	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Msg("[Resell]无法获取控制器")
		return false
	}

	// Step 1: 识别商品价格
	log.Info().Int("行", rowIdx).Int("列", col).Msg("[Resell]扫描商品")
	pricePipelineName := fmt.Sprintf("ResellROIProductRow%dCol%dPrice", rowIdx, col)
	ResellDelayFreezesTime(ctx, 200)
	MoveMouseSafe(controller)
	costPrice, clickX, clickY, success := ocrExtractNumberWithCenter(ctx, controller, pricePipelineName)
	if !success {
		MoveMouseSafe(controller)
		costPrice, clickX, clickY, success = ocrExtractNumberWithCenter(ctx, controller, pricePipelineName)
		if !success {
			log.Info().Int("行", rowIdx).Int("列", col).Msg("[Resell]位置无数字，无商品，跳下一格")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{getNextScanNode(rowIdx, col, true)})
			return true
		}
	}
	controller.PostClick(int32(clickX), int32(clickY))

	// Step 2: 识别「查看好友价格」
	log.Info().Msg("[Resell]查看好友价格")
	ResellDelayFreezesTime(ctx, 200)
	MoveMouseSafe(controller)
	_, friendBtnX, friendBtnY, success := ocrExtractTextWithCenter(ctx, controller, "ResellROIViewFriendPrice")
	if !success {
		log.Info().Msg("[Resell]未找到查看好友价格按钮")
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{getNextScanNode(rowIdx, col, false)})
		return true
	}
	MoveMouseSafe(controller)
	ConfirmcostPrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "ResellROIDetailCostPrice")
	if success {
		costPrice = ConfirmcostPrice
	} else {
		MoveMouseSafe(controller)
		if c, _, _, ok := ocrExtractNumberWithCenter(ctx, controller, "ResellROIDetailCostPrice"); ok {
			costPrice = c
		}
	}
	controller.PostClick(int32(friendBtnX), int32(friendBtnY))

	// Step 3: 等待并识别好友出售价
	if _, err := ctx.RunTask("ResellWaitFriendPrice", nil); err != nil {
		log.Info().Err(err).Msg("[Resell]未能识别好友出售价")
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{getNextScanNode(rowIdx, col, false)})
		return true
	}
	MoveMouseSafe(controller)
	salePrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "ResellROIFriendSalePrice")
	if !success {
		MoveMouseSafe(controller)
		salePrice, _, _, success = ocrExtractNumberWithCenter(ctx, controller, "ResellROIFriendSalePrice")
		if !success {
			log.Info().Msg("[Resell]未能识别好友出售价")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{getNextScanNode(rowIdx, col, false)})
			return true
		}
	}
	profit := salePrice - costPrice
	record := ProfitRecord{Row: rowIdx, Col: col, CostPrice: costPrice, SalePrice: salePrice, Profit: profit}
	appendRecord(record)

	// Step 4: 返回好友价格页 -> 商品详情页
	ResellDelayFreezesTime(ctx, 200)
	MoveMouseSafe(controller)
	if _, err := ctx.RunTask("ResellROIReturnButton", nil); err != nil {
		log.Warn().Err(err).Msg("[Resell]返回按钮点击失败")
	}
	ResellDelayFreezesTime(ctx, 200)
	MoveMouseSafe(controller)
	if _, err := ctx.RunTask("CloseButtonType1", nil); err != nil {
		log.Warn().Err(err).Msg("[Resell]关闭页面失败")
	}

	ctx.OverrideNext(arg.CurrentTaskName, []maa.NodeNextItem{getNextScanNode(rowIdx, col, false)})
	return true
}

func getNextScanNode(row, col int, breakRow bool) maa.NodeNextItem {
	if breakRow {
		if row < 3 {
			return maa.NodeNextItem{Name: fmt.Sprintf("ResellScanRow%dCol1", row+1)}
		}
		return maa.NodeNextItem{Name: "ResellDecide"}
	}
	if col < 8 {
		return maa.NodeNextItem{Name: fmt.Sprintf("ResellScanRow%dCol%d", row, col+1)}
	}
	if row < 3 {
		return maa.NodeNextItem{Name: fmt.Sprintf("ResellScanRow%dCol1", row+1)}
	}
	return maa.NodeNextItem{Name: "ResellDecide"}
}
