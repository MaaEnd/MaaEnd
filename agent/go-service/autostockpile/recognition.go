package autostockpile

import (
	"encoding/json"
	"sort"

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

	cfg, abortReason, err := getSelectionConfigFromNode(ctx, decisionAttachNodeName, region)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("node", decisionAttachNodeName).
			Str("region", region).
			Str("abort_reason", string(abortReason)).
			Msg("failed to load selection config for recognition")
		return buildAbortedRecognitionResult(arg, abortReason)
	}

	sunday := isServerSundayNow()

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

	stockBillAmount := 0
	stockBillAvailable := false
	if cfg.ReserveStockBill > 0 {
		if amount, ok := runStockBillOCR(ctx, arg.Img); ok {
			stockBillAmount = amount
			stockBillAvailable = true
		} else {
			log.Warn().
				Str("component", autoStockpileComponent).
				Str("step", "stock_bill_ocr").
				Str("abort_reason", string(AbortReasonStockBillUnavailableWarn)).
				Msg("stock bill ocr unavailable")
			return buildAbortedRecognitionResult(arg, AbortReasonStockBillUnavailableWarn)
		}
	} else {
		log.Info().
			Str("component", autoStockpileComponent).
			Int("reserve_stock_bill", cfg.ReserveStockBill).
			Msg("stock bill ocr skipped because reserve threshold is disabled")
	}

	if shouldAbortForInsufficientFunds(stockBillAvailable, stockBillAmount, cfg.ReserveStockBill) {
		log.Info().
			Str("component", autoStockpileComponent).
			Int("overflow_amount", overflowAmount).
			Int("stock_bill_amount", stockBillAmount).
			Int("reserve_stock_bill", cfg.ReserveStockBill).
			Str("abort_reason", string(AbortReasonInsufficientFundsSkip)).
			Msg("stock bill below reserve threshold, aborting recognition before goods scan")

		return buildAbortedRecognitionResult(arg, AbortReasonInsufficientFundsSkip)
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

	goodsROI := resolveGoodsRecognitionROI(ctx, arg.Img)
	prices, ocrNames, goodsOCRAbortReason, goodsOCRErr := runGoodsOCR(ctx, arg.Img, goodsROI, itemMap)
	if goodsOCRAbortReason != AbortReasonNone {
		log.Warn().
			Err(goodsOCRErr).
			Str("component", autoStockpileComponent).
			Str("step", "goods_ocr").
			Str("abort_reason", string(goodsOCRAbortReason)).
			Msg("goods ocr unavailable")
		return buildAbortedRecognitionResult(arg, goodsOCRAbortReason)
	}
	log.Info().
		Str("component", autoStockpileComponent).
		Int("price_count", len(prices)).
		Int("ocr_name_count", len(ocrNames)).
		Msg("goods ocr finished")

	boundIDs := make(map[string]bool)
	usedPrice := make([]bool, len(prices))
	pass1Goods := make([]GoodsItem, 0, len(ocrNames))
	pass1Success := 0
	pass1Failed := 0

	sort.Slice(ocrNames, func(i, j int) bool {
		if ocrNames[i].box.Y() != ocrNames[j].box.Y() {
			return ocrNames[i].box.Y() < ocrNames[j].box.Y()
		}
		return ocrNames[i].box.X() < ocrNames[j].box.X()
	})

	for _, name := range ocrNames {
		boundPrice, ok := bindPriceToOCRGoods(name, prices, usedPrice)
		if !ok {
			pass1Failed++
			log.Warn().
				Str("component", autoStockpileComponent).
				Str("bind_pass", "ocr").
				Str("goods_id", name.id).
				Str("goods_name", name.name).
				Str("tier", name.tier).
				Int("goods_x", name.box.X()).
				Int("goods_y", name.box.Y()).
				Msg("failed to bind price for goods")
			continue
		}

		pass1Goods = append(pass1Goods, GoodsItem{
			ID:    name.id,
			Name:  name.name,
			Tier:  name.tier,
			Price: boundPrice,
		})
		boundIDs[name.id] = true
		pass1Success++
	}

	log.Info().
		Str("component", autoStockpileComponent).
		Str("bind_pass", "ocr").
		Int("bind_success", pass1Success).
		Int("bind_failed", pass1Failed).
		Msg("goods-price binding finished")

	candidateIDs := listUnboundRegionItemIDs(itemMap, region, boundIDs)
	log.Info().
		Str("component", autoStockpileComponent).
		Str("region", region).
		Str("template_source", "item_map").
		Int("template_count", len(candidateIDs)).
		Msg("goods template candidates loaded")

	goods := make([]goodsCandidate, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		templatePath := BuildTemplatePath(id)

		detail, recErr := runGoodsTemplateMatch(ctx, arg.Img, templatePath, goodsROI)
		if recErr != nil {
			log.Warn().
				Err(recErr).
				Str("component", autoStockpileComponent).
				Str("template", templatePath).
				Msg("template match failed")
			continue
		}

		box, hit := bestTemplateHit(detail)
		if !hit {
			continue
		}

		itemName := itemMap.IDToName[id]
		tier := ParseTierFromID(id)

		goods = append(goods, goodsCandidate{
			item: GoodsItem{
				ID:    id,
				Name:  itemName,
				Tier:  tier,
				Price: 0,
			},
			box: box,
		})
	}

	log.Info().
		Str("component", autoStockpileComponent).
		Int("template_hits", len(goods)).
		Msg("template matching finished")

	sort.Slice(goods, func(i, j int) bool {
		if goods[i].box.Y() == goods[j].box.Y() {
			return goods[i].box.X() < goods[j].box.X()
		}
		return goods[i].box.Y() < goods[j].box.Y()
	})

	resultGoods := make([]GoodsItem, 0, len(pass1Goods)+len(goods))
	resultGoods = append(resultGoods, pass1Goods...)
	bindingSuccess := 0
	bindingFailed := 0

	for _, g := range goods {
		boundPrice, ok := bindPriceToGoods(g, prices, usedPrice)
		item := g.item
		if ok {
			item.Price = boundPrice
			bindingSuccess++
		} else {
			bindingFailed++
			log.Warn().
				Str("component", autoStockpileComponent).
				Str("bind_pass", "template").
				Str("goods_id", g.item.ID).
				Str("goods_name", g.item.Name).
				Str("tier", g.item.Tier).
				Int("price", item.Price).
				Int("goods_x", g.box.X()).
				Int("goods_y", g.box.Y()).
				Msg("failed to bind price for goods, skipping")
			continue
		}
		resultGoods = append(resultGoods, item)
	}

	log.Info().
		Str("component", autoStockpileComponent).
		Str("bind_pass", "template").
		Int("bind_success", bindingSuccess).
		Int("bind_failed", bindingFailed).
		Msg("goods-price binding finished")

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
			Sunday:             sunday,
			StockBillAmount:    stockBillAmount,
			StockBillAvailable: stockBillAvailable,
			Goods:              resultGoods,
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
		Bool("sunday", resultPayload.Data.Sunday).
		Int("stock_bill_amount", resultPayload.Data.StockBillAmount).
		Bool("stock_bill_available", resultPayload.Data.StockBillAvailable).
		Str("abort_reason", string(resultPayload.AbortReason)).
		Int("goods_count", len(resultPayload.Data.Goods)).
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
