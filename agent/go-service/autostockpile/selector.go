package autostockpile

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	swipeMaxNodeName    = "AutoStockpileSwipeMax"
	noCandidateNodeName = "AutoStockpileNoCandidate"
)

type candidateGoods struct {
	goods     GoodsItem
	threshold int
	score     int
}

// Run 执行 AutoStockpile 单商品选择逻辑。
func (a *SelectItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", "autostockpile").
			Msg("custom action arg is nil")
		return false
	}

	cfg, err := getSelectionConfigFromNode(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Str("step", "load_selection_config").
			Msg("invalid selection config")
		return false
	}

	detailJSON := extractRecoDetailJson(arg.RecognitionDetail)
	if detailJSON == "" {
		log.Error().
			Str("component", "autostockpile").
			Msg("recognition detail json is empty")
		return false
	}

	var result RecognitionResult
	if err := json.Unmarshal([]byte(detailJSON), &result); err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Msg("failed to parse recognition result")
		return false
	}
	if err := result.Validate(); err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Msg("recognition result violates contract")
		return false
	}

	goodsCount := 0
	stockBillAmount := 0
	sunday := false
	if result.Data != nil {
		goodsCount = len(result.Data.Goods)
		stockBillAmount = result.Data.StockBillAmount
		sunday = result.Data.Sunday
	}

	log.Info().
		Str("component", "autostockpile").
		Bool("overflow", result.hasOverflow()).
		Bool("sunday", sunday).
		Str("abort_reason", string(result.AbortReason)).
		Int("stock_bill_amount", stockBillAmount).
		Int("goods_count", goodsCount).
		Msg("recognition result parsed")

	if shouldShortCircuitNoCandidate(result.AbortReason) {
		reasonText, err := LookupAbortReasonZHCN(result.AbortReason)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("abort_reason", string(result.AbortReason)).
				Msg("failed to resolve abort reason message")
			return false
		}

		log.Info().
			Str("component", "autostockpile").
			Str("abort_reason", string(result.AbortReason)).
			Str("abort_reason_text", reasonText).
			Msg("recognition requested no-candidate short-circuit")
		maafocus.NodeActionStarting(ctx, fmt.Sprintf("识别阶段提前终止，跳过本次购买（%s）", reasonText))
		if err := overrideNoCandidateBranch(ctx, arg.CurrentTaskName); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", arg.CurrentTaskName).
				Str("next", noCandidateNodeName).
				Msg("failed to short-circuit abort path to no-candidate branch")
			return false
		}
		return true
	}

	data := result.Data

	// OverflowMode intentionally shares the same threshold-bypass path as SundayMode.
	// Although the option key is named AutoStockpileOverflowBuyLowPriceGoods,
	// the expected behavior is to allow above-threshold purchases when stock is overflowing.
	bypassThresholdFilter := (result.hasOverflow() && cfg.OverflowMode) || (data.Sunday && cfg.SundayMode)
	if bypassThresholdFilter {
		log.Info().
			Str("component", "autostockpile").
			Bool("overflow_allow", result.hasOverflow() && cfg.OverflowMode).
			Bool("sunday_allow", data.Sunday && cfg.SundayMode).
			Msg("allow all goods mode enabled")
	}

	selection := SelectBestProduct(*data, cfg, bypassThresholdFilter)
	if !selection.Selected {
		log.Info().
			Str("component", "autostockpile").
			Str("reason", selection.Reason).
			Msg("no qualifying product selected")
		maafocus.NodeActionStarting(ctx, fmt.Sprintf("未找到符合条件的物资 (原因: %s)", selection.Reason))
		if err := overrideNoCandidateBranch(ctx, arg.CurrentTaskName); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", arg.CurrentTaskName).
				Str("next", noCandidateNodeName).
				Msg("failed to short-circuit to no-candidate branch")
			return false
		}
		return true
	}

	quantityDecision := resolveQuantityDecision(selection, *data, cfg)
	if quantityDecision.Mode == quantityModeNoCandidate {
		log.Info().
			Str("component", "autostockpile").
			Str("selection_mode", formatSelectionMode(selection, *data, cfg)).
			Str("quantity_mode", string(quantityDecision.Mode)).
			Str("quantity_reason", quantityDecision.Reason).
			Int("max_buy", quantityDecision.MaxBuy).
			Int("quota_current", data.Quota.Current).
			Int("quota_overflow", data.Quota.Overflow).
			Int("reserve_stock_bill", cfg.ReserveStockBill).
			Msg("quantity decision requested no-candidate short-circuit")
		maafocus.NodeActionStarting(ctx, fmt.Sprintf("已命中商品，但最终不购买（%s）", quantityDecision.Reason))
		if err := overrideNoCandidateBranch(ctx, arg.CurrentTaskName); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", arg.CurrentTaskName).
				Str("next", noCandidateNodeName).
				Msg("failed to short-circuit quantity no-candidate branch")
			return false
		}
		return true
	}

	override, err := buildSelectionPipelineOverride(ctx, selection, quantityDecision)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Msg("failed to build selection pipeline override")
		return false
	}

	if err := ctx.OverridePipeline(override); err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Str("node", selectedGoodsClickNodeName+","+swipeMaxNodeName+","+swipeSpecificQuantityNodeName).
			Msg("failed to override selector pipeline")
		return false
	}

	selectionMode := formatSelectionMode(selection, *data, cfg)
	quantityLog := log.Info().
		Str("component", "autostockpile").
		Str("selection_mode", selectionMode).
		Str("template", BuildTemplatePath(selection.ProductID)).
		Str("tier", selection.CanonicalName).
		Int("threshold", selection.Threshold).
		Int("price", selection.CurrentPrice).
		Int("score", selection.Score).
		Int("max_buy", quantityDecision.MaxBuy).
		Int("quota_current", data.Quota.Current).
		Int("quota_overflow", data.Quota.Overflow).
		Int("reserve_stock_bill", cfg.ReserveStockBill).
		Str("quantity_mode", string(quantityDecision.Mode)).
		Str("quantity_reason", quantityDecision.Reason).
		Bool("swipe_max_enabled", quantityDecision.Mode == quantityModeSwipeMax).
		Bool("swipe_specific_quantity_enabled", quantityDecision.Mode == quantityModeSwipeSpecificQuantity)
	if quantityDecision.Mode == quantityModeSwipeSpecificQuantity {
		quantityLog = quantityLog.Int("quantity_target", quantityDecision.Target)
	}
	quantityLog.Msg("product selected and pipeline overridden")
	maafocus.NodeActionStarting(ctx, fmt.Sprintf("【%s】%s (价格 %d, 阈值 %d, 数量 %s)", selectionMode, selection.ProductName, selection.CurrentPrice, selection.Threshold, formatQuantityText(quantityDecision)))

	return true
}

// SelectBestProduct 按阈值与利润分数选择当前应购买的最佳商品。
func SelectBestProduct(data RecognitionData, cfg SelectionConfig, bypassThresholdFilter bool) SelectionResult {
	if len(data.Goods) == 0 {
		return SelectionResult{Selected: false, Reason: "未识别到商品"}
	}

	candidates := make([]candidateGoods, 0, len(data.Goods))
	for _, goods := range data.Goods {
		threshold := resolveTierThreshold(goods.Tier, cfg)
		score := threshold - goods.Price

		log.Debug().
			Str("component", "autostockpile").
			Str("name", goods.Name).
			Str("tier", goods.Tier).
			Int("price", goods.Price).
			Int("threshold", threshold).
			Int("score", score).
			Bool("bypass_threshold_filter", bypassThresholdFilter).
			Msg("evaluating goods")

		if !bypassThresholdFilter && score <= 0 {
			continue
		}

		candidates = append(candidates, candidateGoods{
			goods:     goods,
			threshold: threshold,
			score:     score,
		})
	}

	if len(candidates) == 0 {
		return SelectionResult{Selected: false, Reason: "没有满足条件的商品"}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].goods.Price != candidates[j].goods.Price {
			return candidates[i].goods.Price < candidates[j].goods.Price
		}
		if candidates[i].goods.Tier != candidates[j].goods.Tier {
			return candidates[i].goods.Tier < candidates[j].goods.Tier
		}
		return candidates[i].goods.Name < candidates[j].goods.Name
	})

	best := candidates[0]
	return SelectionResult{
		Selected:      true,
		ProductID:     best.goods.ID,
		ProductName:   best.goods.Name,
		CanonicalName: best.goods.Tier,
		Threshold:     best.threshold,
		CurrentPrice:  best.goods.Price,
		Score:         best.score,
	}
}

func shouldShortCircuitNoCandidate(reason AbortReason) bool {
	return reason != AbortReasonNone
}

func overrideNoCandidateBranch(ctx *maa.Context, currentTaskName string) error {
	if err := ctx.OverridePipeline(buildNoCandidateResetOverride()); err != nil {
		return fmt.Errorf("reset no-candidate pipeline state: %w", err)
	}

	if err := ctx.OverrideNext(currentTaskName, buildNoCandidateNextItems()); err != nil {
		return fmt.Errorf("override next for no-candidate branch: %w", err)
	}

	return nil
}

func buildNoCandidateResetOverride() map[string]any {
	return map[string]any{
		selectedGoodsClickNodeName: map[string]any{
			"enabled": false,
		},
		swipeMaxNodeName: map[string]any{
			"enabled": false,
		},
		swipeSpecificQuantityNodeName: map[string]any{
			"enabled": false,
		},
	}
}

func buildNoCandidateNextItems() []maa.NextItem {
	return []maa.NextItem{{Name: noCandidateNodeName}}
}

func formatSelectionMode(selection SelectionResult, data RecognitionData, cfg SelectionConfig) string {
	if selection.CurrentPrice < selection.Threshold {
		return "低价购买"
	}
	if cfg.SundayMode && data.Sunday {
		return "周日清空"
	}
	if cfg.OverflowMode && data.Quota.Overflow > 0 {
		return "防溢出"
	}
	return "低价购买"
}

func extractRecoDetailJson(rd *maa.RecognitionDetail) string {
	if rd == nil || rd.DetailJson == "" {
		return ""
	}

	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(rd.DetailJson), &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
		return string(wrapped.Best.Detail)
	}

	return rd.DetailJson
}
