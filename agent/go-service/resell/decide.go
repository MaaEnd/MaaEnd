package resell

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ResellDecideAction 根据记录、溢出、最低利润决策下一步
type ResellDecideAction struct{}

func (a *ResellDecideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	records, overflowAmount, MinimumProfit := getState()

	if len(records) == 0 {
		log.Info().Msg("[Resell]库存已售罄，无可购买商品")
		maafocus.NodeActionStarting(ctx, "⚠️ 库存已售罄，无可购买商品")
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "ChangeNextRegionPrepare"}})
		return true
	}

	maxProfitIdx := -1
	maxProfit := 0
	for i, r := range records {
		if r.Profit > maxProfit {
			maxProfit = r.Profit
			maxProfitIdx = i
		}
	}
	if maxProfitIdx < 0 {
		log.Error().Msg("[Resell]未找到最高利润商品")
		return false
	}

	maxRecord := records[maxProfitIdx]
	log.Info().Msgf("[Resell]最高利润商品: 第%d行第%d列，利润%d", maxRecord.Row, maxRecord.Col, maxRecord.Profit)
	showMaxRecord := processMaxRecord(maxRecord)

	if maxRecord.Profit >= MinimumProfit {
		log.Info().Msgf("[Resell]利润达标，准备购买第%d行第%d列（利润：%d）", showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
		taskName := fmt.Sprintf("ResellSelectProductRow%dCol%d", maxRecord.Row, maxRecord.Col)
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: taskName}})
		return true
	}
	if overflowAmount > 0 {
		log.Info().Msgf("[Resell]配额溢出：建议购买%d件，推荐第%d行第%d列（利润：%d）",
			overflowAmount, showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
		message := fmt.Sprintf("⚠️ 配额溢出提醒\n剩余配额明天将超出上限，建议购买%d件商品\n推荐购买: 第%d行第%d列 (最高利润: %d)",
			overflowAmount, showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
		maafocus.NodeActionStarting(ctx, message)
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "ChangeNextRegionPrepare"}})
		return true
	}

	log.Info().Msgf("[Resell]没有达到最低利润%d的商品，推荐第%d行第%d列（利润：%d）",
		MinimumProfit, showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
	var message string
	if MinimumProfit >= 999999 {
		message = fmt.Sprintf("💡 已禁用自动购买/出售\n推荐购买: 第%d行第%d列 (利润: %d)",
			showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
	} else {
		message = fmt.Sprintf("💡 没有达到最低利润的商品，建议把配额留至明天\n推荐购买: 第%d行第%d列 (利润: %d)",
			showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
	}
	maafocus.NodeActionStarting(ctx, message)
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "ChangeNextRegionPrepare"}})
	return true
}
