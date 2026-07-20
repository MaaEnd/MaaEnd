package autostockpile

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &ItemValueChangeRecognition{}

// ItemValueChangeRecognition 负责识别商品及其价格信息。
type ItemValueChangeRecognition struct{}

// Run 执行 AutoStockpile 自定义识别，并返回包含商品与价格信息的结构化结果。
func (r *ItemValueChangeRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().
			Str("component", autoStockpileComponent).
			Msg("custom recognition arg or image is nil")
		return nil, false
	}

	region, err := resolveGoodsRegionFromTaskNode(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "resolve_goods_region").
			Str("abort_reason", string(AbortReasonRegionResolveFailedFatal)).
			Msg("failed to resolve goods region")
		return buildAbortedRecognitionResult(arg, AbortReasonRegionResolveFailedFatal)
	}
	log.Info().
		Str("component", autoStockpileComponent).
		Str("node", arg.CurrentTaskName).
		Str("region", region).
		Msg("goods region resolved")

	overflowDetected := false
	overflowAmount := 0
	overflowCurrent := 0
	overflowAbortReason := AbortReasonNone
	if cur, max, plus, ok := runOverflowDetailOCR(ctx, arg.Img); ok {
		overflowCurrent = cur
		overflowDetected, overflowAmount = resolveOverflow(cur, max, plus)

		log.Info().
			Str("component", autoStockpileComponent).
			Int("overflow_current", cur).
			Int("overflow_max", max).
			Int("overflow_plus", plus).
			Int("overflow_amount", overflowAmount).
			Bool("overflow_detected", overflowDetected).
			Msg("overflow detail parsed")

		overflowAbortReason = resolveAbortReasonFromOverflowCurrent(cur)
	} else {
		log.Warn().
			Str("component", autoStockpileComponent).
			Str("abort_reason", string(AbortReasonQuotaUnavailableWarn)).
			Msg("overflow detail unavailable, aborting with warning")
		return buildAbortedRecognitionResult(arg, AbortReasonQuotaUnavailableWarn)
	}

	if overflowAbortReason != AbortReasonNone {
		log.Info().
			Str("component", autoStockpileComponent).
			Int("overflow_current", overflowCurrent).
			Int("overflow_amount", overflowAmount).
			Str("abort_reason", string(overflowAbortReason)).
			Msg("quota exhausted, aborting recognition before goods scan")

		return buildAbortedRecognitionResult(arg, overflowAbortReason)
	}

	itemMap := GetItemMap()
	if err := validateItemMap(itemMap); err != nil {
		nameCount, idCount := itemMapCounts(itemMap)
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "load_item_map").
			Int("name_count", nameCount).
			Int("id_count", idCount).
			Msg("item_map is unavailable")
		return nil, false
	}

	resultGoods, secondPageOnlyIDs, goodsAbortReason, scanErr := scanGoodsWithOptionalSecondPage(ctx, arg.Img, region, itemMap)
	if goodsAbortReason != AbortReasonNone {
		log.Warn().
			Err(scanErr).
			Str("component", autoStockpileComponent).
			Str("step", "scan_goods").
			Str("abort_reason", string(goodsAbortReason)).
			Msg("goods scan unavailable")
		return buildAbortedRecognitionResult(arg, goodsAbortReason)
	}
	if scanErr != nil {
		log.Error().
			Err(scanErr).
			Str("component", autoStockpileComponent).
			Str("step", "scan_goods").
			Msg("goods scan failed")
		return nil, false
	}

	if err := validateRecognizedGoodsTiers(resultGoods); err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("abort_reason", string(AbortReasonGoodsTierInvalidFatal)).
			Msg("recognized goods contains invalid tier")
		return buildAbortedRecognitionResult(arg, AbortReasonGoodsTierInvalidFatal)
	}

	applyTestPricesIfEnabled(resultGoods)

	resultPayload := RecognitionResult{
		Data: &RecognitionData{
			Quota: QuotaInfo{
				Current:  overflowCurrent,
				Overflow: overflowAmount,
			},
			Goods:             resultGoods,
			SecondPageOnlyIDs: secondPageOnlyIDs,
		},
		AbortReason: AbortReasonNone,
	}

	result, err := buildCustomRecognitionResult(arg, resultPayload)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Msg("failed to marshal recognition result")
		return nil, false
	}

	log.Info().
		Str("component", autoStockpileComponent).
		Int("quota_current", resultPayload.Data.Quota.Current).
		Int("quota_overflow", resultPayload.Data.Quota.Overflow).
		Bool("overflow", resultPayload.hasOverflow()).
		Str("abort_reason", string(resultPayload.AbortReason)).
		Int("goods_count", len(resultPayload.Data.Goods)).
		Int("second_page_only_count", len(resultPayload.Data.SecondPageOnlyIDs)).
		Msg("custom recognition finished")
	maafocus.Print(ctx, i18n.T("autostockpile.recognition_done", len(resultPayload.Data.Goods)))

	return result, true
}

func buildAbortedRecognitionResult(arg *maa.CustomRecognitionArg, reason AbortReason) (*maa.CustomRecognitionResult, bool) {
	resultPayload := RecognitionResult{
		Data:        nil,
		AbortReason: reason,
	}

	result, err := buildCustomRecognitionResult(arg, resultPayload)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("abort_reason", string(reason)).
			Msg("failed to marshal aborted recognition result")
		return nil, false
	}

	return result, true
}

func buildCustomRecognitionResult(arg *maa.CustomRecognitionArg, payload RecognitionResult) (*maa.CustomRecognitionResult, error) {
	resultDetail, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(resultDetail),
	}, nil
}
