package autostockpile

import (
	"encoding/json"
	"sort"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &SelectItemAction{}

// SelectItemAction 根据识别结果执行商品选择动作。
type SelectItemAction struct{}

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

	detailJSON := extractCustomRecognitionDetailJSON(arg.RecognitionDetail)
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
	if result.Data != nil {
		goodsCount = len(result.Data.Goods)
	}

	log.Info().
		Str("component", "autostockpile").
		Bool("overflow", result.hasOverflow()).
		Str("abort_reason", string(result.AbortReason)).
		Int("goods_count", goodsCount).
		Msg("recognition result parsed")

	if shouldStopTask(result.AbortReason) {
		return stopTaskWithFocus(ctx, result.AbortReason, nil)
	}
	if shouldRouteSkip(result.AbortReason) {
		return routeSkipWithAbortReason(ctx, arg.CurrentTaskName, result.AbortReason, nil, i18n.T("autostockpile.recognition_early_end"))
	}

	data := result.Data
	region, err := resolveGoodsRegionFromActionArg(arg)
	if err != nil {
		return stopTaskWithFocus(ctx, AbortReasonRegionResolveFailedFatal, err)
	}
	log.Info().
		Str("component", "autostockpile").
		Str("region", region).
		Msg("selector region resolved")

	serverTimeOffset, err := loadServerTimeOffsetFromAttach(ctx, attachNodeName)
	if err != nil {
		return stopTaskWithFocus(ctx, AbortReasonSelectionConfigInvalidFatal, err)
	}
	applyWeekdayAdjustment := serverTimeOffset != nil
	serverLocation := locationFromUTCOffset(serverTimeOffset)

	log.Info().
		Str("component", "autostockpile").
		Str("region", region).
		Str("server_location", serverLocation.String()).
		Msg("selector server time resolved")

	cfg, err := buildSelectionConfig(region, serverLocation, applyWeekdayAdjustment)
	if err != nil {
		return stopTaskWithFocus(ctx, AbortReasonSelectionConfigInvalidFatal, err)
	}

	bypassThresholdFilter := result.hasOverflow()
	if bypassThresholdFilter {
		log.Info().
			Str("component", "autostockpile").
			Bool("overflow_allow", result.hasOverflow()).
			Msg("allow all goods mode enabled")
	}

	selection, quantityDecision, err := computeDecision(*data, cfg, bypassThresholdFilter)
	if err != nil {
		return stopTaskWithFocus(ctx, mapComputeDecisionErrorToAbortReason(err), err)
	}
	if !selection.Selected {
		log.Info().
			Str("component", "autostockpile").
			Str("reason", selection.Reason).
			Msg("no qualifying product selected")
		maafocus.Print(ctx, i18n.T("autostockpile.no_qualifying_product", selection.Reason))
		if err := overrideSkipBranch(ctx); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", arg.CurrentTaskName).
				Msg("failed to enable skip branch")
			return false
		}
		return true
	}

	if quantityDecision.Mode == quantityModeSkip {
		log.Info().
			Str("component", "autostockpile").
			Str("selection_mode", formatSelectionMode(selection, *data)).
			Str("quantity_mode", string(quantityDecision.Mode)).
			Str("quantity_reason", quantityDecision.Reason).
			Int("quota_current", data.Quota.Current).
			Int("quota_overflow", data.Quota.Overflow).
			Msg("quantity decision requested skip short-circuit")
		maafocus.Print(ctx, i18n.T("autostockpile.hit_but_skip", quantityDecision.Reason))
		if err := overrideSkipBranch(ctx); err != nil {
			log.Error().
				Err(err).
				Str("component", "autostockpile").
				Str("node", arg.CurrentTaskName).
				Msg("failed to enable quantity skip branch")
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

	setDecisionState(&DecisionState{
		Region:             region,
		EffectiveConfig:    cfg,
		RawRecognitionData: *data,
		CurrentDecision: currentDecision{
			Selection:        selection,
			QuantityDecision: quantityDecision,
		},
	})

	selectionMode := formatSelectionMode(selection, *data)
	quantityLog := log.Info().
		Str("component", "autostockpile").
		Str("selection_mode", selectionMode).
		Str("template", BuildTemplatePath(selection.ProductID)).
		Str("tier", selection.CanonicalName).
		Int("threshold", selection.Threshold).
		Int("price", selection.CurrentPrice).
		Int("score", selection.Score).
		Int("quota_current", data.Quota.Current).
		Int("quota_overflow", data.Quota.Overflow).
		Str("quantity_mode", string(quantityDecision.Mode)).
		Str("quantity_reason", quantityDecision.Reason).
		Bool("swipe_max_enabled", quantityDecision.Mode == quantityModeSwipeMax).
		Bool("swipe_specific_quantity_enabled", quantityDecision.Mode == quantityModeSwipeSpecificQuantity)
	if quantityDecision.Mode == quantityModeSwipeSpecificQuantity {
		quantityLog = quantityLog.Int("quantity_target", quantityDecision.Target)
	}
	quantityLog.Msg("product selected and pipeline overridden")
	maafocus.Print(ctx, i18n.T("autostockpile.product_selected", selectionMode, selection.ProductName, selection.CurrentPrice))

	return true
}

// SelectBestProduct 按阈值与利润分数选择当前应购买的最佳商品。
func SelectBestProduct(data RecognitionData, cfg SelectionConfig, bypassThresholdFilter bool) (SelectionResult, error) {
	if len(data.Goods) == 0 {
		return SelectionResult{Selected: false, Reason: i18n.T("autostockpile.no_goods_recognized")}, nil
	}

	candidates := make([]candidateGoods, 0, len(data.Goods))
	for _, goods := range data.Goods {
		threshold, err := resolveTierThreshold(goods.Tier, cfg)
		if err != nil {
			return SelectionResult{}, err
		}
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
		return SelectionResult{Selected: false, Reason: i18n.T("autostockpile.no_qualifying_goods")}, nil
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
	}, nil
}

func shouldRouteSkip(reason AbortReason) bool {
	return reason.isWarn() || reason.isSkip()
}

func shouldStopTask(reason AbortReason) bool {
	return reason.isFatal()
}

func lookupAbortReasonText(reason AbortReason) string {
	reasonText, err := LookupAbortReason(reason)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", "autostockpile").
			Str("abort_reason", string(reason)).
			Msg("failed to resolve abort reason message, fallback to reason key")
		return string(reason)
	}

	return reasonText
}

func routeSkipWithAbortReason(ctx *maa.Context, currentTaskName string, reason AbortReason, err error, focusPrefix string) bool {
	reasonText := lookupAbortReasonText(reason)

	logEvent := log.Info()
	if reason.isWarn() {
		logEvent = log.Warn()
	}
	logEvent = logEvent.
		Str("component", "autostockpile").
		Str("abort_reason", string(reason)).
		Str("abort_reason_text", reasonText)
	if err != nil {
		logEvent = logEvent.Err(err)
	}
	logEvent.Msg("routing current cycle to skip branch")

	if reason.isWarn() {
		maafocus.Print(ctx, i18n.RenderHTML("autostockpile.warning_skip", map[string]any{
			"Prefix": focusPrefix,
			"Reason": reasonText,
		}))
	} else {
		maafocus.Print(ctx, i18n.T("autostockpile.abort_info", focusPrefix, reasonText))
	}
	if err := overrideSkipBranch(ctx); err != nil {
		log.Error().
			Err(err).
			Str("component", "autostockpile").
			Str("node", currentTaskName).
			Msg("failed to enable abort skip branch")
		return false
	}

	return true
}

func stopTaskWithFocus(ctx *maa.Context, reason AbortReason, err error) bool {
	reasonText := lookupAbortReasonText(reason)

	logEvent := log.Error().
		Str("component", "autostockpile").
		Str("abort_reason", string(reason)).
		Str("abort_reason_text", reasonText)
	if err != nil {
		logEvent = logEvent.Err(err)
	}
	logEvent.Msg("stopping task due to fatal abort reason")

	maafocus.Print(ctx, i18n.RenderHTML("autostockpile.fatal_error", map[string]any{
		"Reason": reasonText,
	}))
	if ctx == nil || ctx.GetTasker() == nil {
		log.Error().
			Str("component", "autostockpile").
			Str("abort_reason", string(reason)).
			Msg("tasker is unavailable for fatal stop")
		return false
	}
	// TODO 由于SubTask中的子Task错误时不能全部停止，此处使用PostStop强制停止，代价是会抛出一部分报错
	ctx.GetTasker().PostStop()
	return false
}

func formatSelectionMode(selection SelectionResult, data RecognitionData) string {
	if selection.CurrentPrice < selection.Threshold {
		return i18n.T("autostockpile.mode_low_price")
	}
	if data.Quota.Overflow > 0 {
		return i18n.T("autostockpile.mode_overflow")
	}
	return i18n.T("autostockpile.mode_low_price")
}
