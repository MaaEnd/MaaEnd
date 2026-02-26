package resell

import (
	"encoding/json"
	"fmt"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ResellScanProductAction 扫描单个 (row,col) 商品，追加利润记录，跳转到下一格或决策节点
// row/col 通过 custom_action_param 传入，可由 OverridePipeline 运行时覆盖
type ResellScanProductAction struct{}

func (a *ResellScanProductAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	rowIdx, col := 1, 1
	if arg.CustomActionParam != "" {
		var params struct {
			Row int `json:"row"`
			Col int `json:"col"`
		}
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
			log.Error().Err(err).Str("param", arg.CustomActionParam).Msg("[Resell]无法解析 custom_action_param")
			return false
		}
		if params.Row >= 1 && params.Row <= 3 && params.Col >= 1 && params.Col <= 8 {
			rowIdx, col = params.Row, params.Col
		}
	}

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
			resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, true)
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
		resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, false)
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
		resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, false)
		return true
	}
	MoveMouseSafe(controller)
	salePrice, _, _, success := ocrExtractNumberWithCenter(ctx, controller, "ResellROIFriendSalePrice")
	if !success {
		MoveMouseSafe(controller)
		salePrice, _, _, success = ocrExtractNumberWithCenter(ctx, controller, "ResellROIFriendSalePrice")
		if !success {
			log.Info().Msg("[Resell]未能识别好友出售价")
			resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, false)
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

	resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, false)
	return true
}

// resellScanOverrideNext 设置下一格：通过 OverridePipeline 写入 row/col 到 ResellScan 的 custom_action_param，再 OverrideNext
func resellScanOverrideNext(ctx *maa.Context, currentTask string, row, col int, breakRow bool) {
	nextRow, nextCol, done := computeNextScanPos(row, col, breakRow)
	if done {
		ctx.OverrideNext(currentTask, []maa.NodeNextItem{{Name: "ResellDecide"}})
		return
	}
	_ = ctx.OverridePipeline(map[string]any{
		"ResellScan": map[string]any{
			"custom_action_param": map[string]any{
				"row": nextRow,
				"col": nextCol,
			},
		},
	})
	ctx.OverrideNext(currentTask, []maa.NodeNextItem{{Name: "ResellScan"}})
}

func computeNextScanPos(row, col int, breakRow bool) (nextRow, nextCol int, done bool) {
	if breakRow {
		if row < 3 {
			return row + 1, 1, false
		}
		return 0, 0, true
	}
	if col < 8 {
		return row, col + 1, false
	}
	if row < 3 {
		return row + 1, 1, false
	}
	return 0, 0, true
}
