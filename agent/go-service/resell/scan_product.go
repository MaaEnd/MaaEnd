package resell

import (
	"encoding/json"
	"fmt"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ResellScanAction 入口：解析 row/col，OverrideNext 到 Step1
type ResellScanAction struct{}

func (a *ResellScanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
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
	setScanPos(rowIdx, col)
	pricePipelineName := fmt.Sprintf("ResellROIProductRow%dCol%dPrice", rowIdx, col)
	_ = ctx.OverridePipeline(map[string]any{
		"ResellScanStart": map[string]any{
			"recognition": "Or",
			"any_of":      []string{pricePipelineName},
		},
	})
	if controller := ctx.GetTasker().GetController(); controller != nil {
		MoveMouseSafe(controller) // 为 Step1 的 OCR 识别挪开鼠标
	}
	return true
}

// ResellScanCostAction Step2 确认成本价：从 RecognitionDetail 提取并存储详情页成本
type ResellScanCostAction struct{}

func (a *ResellScanCostAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	text := extractOCRText(arg.RecognitionDetail)
	if text != "" {
		if num, ok := extractNumbersFromText(text); ok {
			setScanCostPrice(num)
			log.Info().Int("costPrice", num).Msg("[Resell]详情页成本价已更新")
		}
	}
	if controller := ctx.GetTasker().GetController(); controller != nil {
		MoveMouseSafe(controller) // 为下一步 ViewFriendPrice 的 OCR 挪开鼠标
	}
	return true
}

// ResellScanFriendPriceAction Step3：从 RecognitionDetail 提取好友出售价、追加利润记录（识别由 Pipeline Or ResellROIFriendSalePrice 完成）
type ResellScanFriendPriceAction struct{}

func (a *ResellScanFriendPriceAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	rowIdx, col := getScanPos()
	costPrice := getScanCostPrice()

	text := extractOCRText(arg.RecognitionDetail)
	if text == "" {
		log.Info().Msg("[Resell]未能识别好友出售价")
		resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, false)
		return true
	}
	salePrice, ok := extractNumbersFromText(text)
	if !ok {
		log.Info().Str("text", text).Msg("[Resell]好友出售价区域无有效数字")
		resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, false)
		return true
	}
	if controller := ctx.GetTasker().GetController(); controller != nil {
		MoveMouseSafe(controller) // 为下一步返回按钮的识别挪开鼠标
	}
	profit := salePrice - costPrice
	record := ProfitRecord{Row: rowIdx, Col: col, CostPrice: costPrice, SalePrice: salePrice, Profit: profit}
	appendRecord(record)
	return true
}

// ResellScanSkipEmptyAction Step1 识别失败（无商品）时跳过当前格
type ResellScanSkipEmptyAction struct{}

func (a *ResellScanSkipEmptyAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	rowIdx, col := getScanPos()
	log.Info().Int("行", rowIdx).Int("列", col).Msg("[Resell]位置无数字，无商品，跳下一格")
	resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, true)
	return true
}

// ResellScanNextAction 跳下一格或进入决策（OverrideNext）
type ResellScanNextAction struct{}

func (a *ResellScanNextAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	rowIdx, col := getScanPos()
	resellScanOverrideNext(ctx, arg.CurrentTaskName, rowIdx, col, false)
	return true
}

// resellScanOverrideNext 设置下一格：通过 OverridePipeline 写入 row/col，再 OverrideNext
func resellScanOverrideNext(ctx *maa.Context, currentTask string, row, col int, breakRow bool) {
	nextRow, nextCol, done := computeNextScanPos(row, col, breakRow)
	if done {
		ctx.OverrideNext(currentTask, []maa.NextItem{{Name: "ResellDecide"}})
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
	ctx.OverrideNext(currentTask, []maa.NextItem{{Name: "ResellScan"}})
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
