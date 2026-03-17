package autostockpile

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const swipeMaxNodeName = "AutoStockpileSwipeMax"

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

	log.Info().
		Str("component", "autostockpile").
		Bool("overflow", result.Overflow).
		Bool("sunday", result.Sunday).
		Int("goods_count", len(result.Goods)).
		Msg("recognition result parsed")

	// OverflowMode intentionally shares the same threshold-bypass path as SundayMode.
	// Although the option key is named AutoStockpileOverflowBuyLowPriceGoods,
	// the expected behavior is to allow above-threshold purchases when stock is overflowing.
	bypassThresholdFilter := (result.Overflow && cfg.OverflowMode) || (result.Sunday && cfg.SundayMode)
	if bypassThresholdFilter {
		log.Info().
			Str("component", "autostockpile").
			Bool("overflow_allow", result.Overflow && cfg.OverflowMode).
			Bool("sunday_allow", result.Sunday && cfg.SundayMode).
			Msg("allow all goods mode enabled")
	}

	selection := SelectBestProduct(result, cfg, bypassThresholdFilter)
	if !selection.Selected {
		log.Info().
			Str("component", "autostockpile").
			Str("reason", selection.Reason).
			Msg("no qualifying product selected")
		maafocus.NodeActionStarting(ctx, fmt.Sprintf("未找到符合条件的物资 (原因: %s)", selection.Reason))
		return true
	}

	if err := ctx.OverridePipeline(map[string]any{
		"AutoStockpileSelectedGoodsClick": map[string]any{
			"enabled":  true,
			"template": []string{BuildTemplatePath(selection.ProductID)},
		},
	}); err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Str("node", "AutoStockpileSelectedGoodsClick").
			Msg("failed to override pipeline for selected goods (click)")
		return false
	}

	overrideEnable, enableSwipeMax, enableSpecificQuantity := resolveSwipeEnable(selection, result, cfg)
	if overrideEnable {
		if err := ctx.OverridePipeline(map[string]any{
			swipeMaxNodeName: map[string]any{
				"enabled": enableSwipeMax,
			},
			swipeSpecificQuantityNodeName: map[string]any{
				"enabled": enableSpecificQuantity,
			},
		}); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", swipeMaxNodeName+","+swipeSpecificQuantityNodeName).
				Msg("failed to override pipeline for swipe quantity controls")
			return false
		}
	}

	log.Info().
		Str("component", "autostockpile").
		Str("template", BuildTemplatePath(selection.ProductID)).
		Str("tier", selection.CanonicalName).
		Int("threshold", selection.Threshold).
		Int("price", selection.CurrentPrice).
		Int("score", selection.Score).
		Bool("swipe_max_enabled", enableSwipeMax).
		Bool("swipe_specific_quantity_enabled", enableSpecificQuantity).
		Int("overflow_amount", result.OverflowAmount).
		Msg("product selected and pipeline overridden")
	maafocus.NodeActionStarting(ctx, fmt.Sprintf("【%s】%s (价格 %d, 阈值 %d)", formatSelectionMode(selection, result, cfg), selection.ProductName, selection.CurrentPrice, selection.Threshold))

	return true
}

// SelectBestProduct 按阈值与利润分数选择当前应购买的最佳商品。
func SelectBestProduct(result RecognitionResult, cfg SelectionConfig, bypassThresholdFilter bool) SelectionResult {
	if len(result.Goods) == 0 {
		return SelectionResult{Selected: false, Reason: "未识别到商品"}
	}

	candidates := make([]candidateGoods, 0, len(result.Goods))
	for _, goods := range result.Goods {
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

func resolveSwipeEnable(selection SelectionResult, result RecognitionResult, cfg SelectionConfig) (bool, bool, bool) {
	if !selection.Selected {
		return false, false, false
	}

	enableSwipeMax := selection.CurrentPrice < selection.Threshold || (cfg.SundayMode && result.Sunday)
	if enableSwipeMax {
		return true, true, false
	}
	if cfg.OverflowMode && result.OverflowAmount > 0 {
		return true, false, true
	}
	return true, true, false
}

func formatSelectionMode(selection SelectionResult, result RecognitionResult, cfg SelectionConfig) string {
	if selection.CurrentPrice < selection.Threshold {
		return "低价购买"
	}
	if cfg.SundayMode && result.Sunday {
		return "周日清空"
	}
	if cfg.OverflowMode && result.OverflowAmount > 0 {
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

func resolveTierThreshold(tierID string, cfg SelectionConfig) int {
	if threshold, ok := cfg.PriceLimits[tierID]; ok && threshold > 0 {
		return threshold
	}
	return resolveFallbackThreshold(cfg.FallbackThreshold)
}

func resolveFallbackThreshold(raw int) int {
	if raw > 0 {
		return raw
	}
	return defaultFallbackBuyThreshold
}
