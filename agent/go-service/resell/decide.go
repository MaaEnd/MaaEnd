package resell

import (
	"fmt"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
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
		maafocus.NodeActionStarting(ctx, i18n.T("resell.stock_empty"))
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
		maafocus.NodeActionStarting(ctx, i18n.T("resell.quota_overflow", overflowAmount, showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit))
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "ChangeNextRegionPrepare"}})
		return true
	}

	log.Info().Msgf("[Resell]没有达到最低利润%d的商品，推荐第%d行第%d列（利润：%d）",
		MinimumProfit, showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
	var message string
	if MinimumProfit >= 999999 {
		message = i18n.T("resell.auto_buy_disabled", showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
	} else {
		message = i18n.T("resell.below_min_profit", showMaxRecord.Row, showMaxRecord.Col, showMaxRecord.Profit)
	}
	maafocus.NodeActionStarting(ctx, message)
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "ChangeNextRegionPrepare"}})
	return true
}
