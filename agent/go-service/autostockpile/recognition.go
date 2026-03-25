package autostockpile

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	autoStockpileComponent = "autostockpile"
	anchorTargetRegionName = "AutoStockpileGotoTargetRegion"

	selectedGoodsClickNodeName    = "AutoStockpileSelectedGoodsClick"
	swipeSpecificQuantityNodeName = "AutoStockpileSwipeSpecificQuantity"
	selectedGoodsClickResetY      = 180
	findMarketMarkNodeName        = "AutoStockpileFindMarketMark"
	overflowQuotaNodeName         = "AutoStockpileGetQuota"
	overflowQuotaAdditionNodeName = "AutoStockpileGetQuotaAddition"
	locateGoodsNodeName           = "AutoStockpileLocateGoods"
	goodsPriceNodeName            = "AutoStockpileGetGoods"
	// MAX_DISTANCE 表示商品与价格框可接受的最大匹配距离。
	MAX_DISTANCE = 120
)

var (
	overflowCurrentMaxRe = regexp.MustCompile(`(\d+)/(\d+)`)
	overflowPlusRe       = regexp.MustCompile(`\+(\d+)`)
	priceRe              = regexp.MustCompile(`^(\d{3,4})$`)
)

type goodsCandidate struct {
	item GoodsItem
	box  maa.Rect
}

type priceCandidate struct {
	value int
	text  string
	box   maa.Rect
}

type ocrNameCandidate struct {
	id   string
	name string
	tier string
	box  maa.Rect
}

// Run 执行 AutoStockpile 自定义识别，并返回包含商品与价格信息的结构化结果。
func (r *ItemValueChangeRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().
			Str("component", autoStockpileComponent).
			Msg("custom recognition arg or image is nil")
		return nil, false
	}

	region, anchor, err := resolveGoodsRegion(ctx)
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
		Str("anchor", anchor).
		Str("region", region).
		Msg("goods region resolved")

	cfg, abortReason, err := getSelectionConfigFromNode(ctx, arg.CurrentTaskName, region)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("node", arg.CurrentTaskName).
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
			Msg("overflow detail unavailable")
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
			Str("abort_reason", string(AbortReasonInsufficientFunds)).
			Msg("stock bill below reserve threshold, aborting recognition before goods scan")

		return buildAbortedRecognitionResult(arg, AbortReasonInsufficientFunds)
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

		box, hit := pickLowestTemplateHit(detail)
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

func validateItemMap(itemMap *ItemMap) error {
	if itemMap == nil {
		return fmt.Errorf("item_map is nil")
	}
	if len(itemMap.NameToID) == 0 {
		return fmt.Errorf("item_map name_to_id is empty")
	}
	if len(itemMap.IDToName) == 0 {
		return fmt.Errorf("item_map id_to_name is empty")
	}
	return nil
}

func itemMapCounts(itemMap *ItemMap) (nameCount int, idCount int) {
	if itemMap == nil {
		return 0, 0
	}
	return len(itemMap.NameToID), len(itemMap.IDToName)
}

func listUnboundRegionItemIDs(itemMap *ItemMap, region string, boundIDs map[string]bool) []string {
	if itemMap == nil || len(itemMap.IDToName) == 0 {
		return nil
	}

	prefix := region + "/"
	ids := make([]string, 0, len(itemMap.IDToName))
	for id := range itemMap.IDToName {
		if !strings.HasPrefix(id, prefix) {
			continue
		}
		if boundIDs[id] {
			continue
		}
		ids = append(ids, id)
	}

	sort.Strings(ids)
	return ids
}

func runOverflowDetailOCR(ctx *maa.Context, img image.Image) (current int, max int, plus int, ok bool) {
	detail, err := ctx.RunRecognition(overflowQuotaNodeName, img, nil)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "overflow_quota_ocr").
			Msg("failed to run overflow quota ocr")
		return 0, 0, 0, false
	}

	current, max, ok = parseOverflowCurrentMax(extractOCRTexts(detail))
	if !ok {
		return 0, 0, 0, false
	}

	plus = runOverflowQuotaAdditionOCR(ctx, img)
	return current, max, plus, true
}

func parseOverflowCurrentMax(texts []string) (current int, max int, ok bool) {
	for _, text := range texts {
		if match := overflowCurrentMaxRe.FindStringSubmatch(text); len(match) == 3 {
			cur, curErr := strconv.Atoi(match[1])
			maxValue, maxErr := strconv.Atoi(match[2])
			if curErr == nil && maxErr == nil {
				return cur, maxValue, true
			}
		}
	}

	return 0, 0, false
}

func runOverflowQuotaAdditionOCR(ctx *maa.Context, img image.Image) int {
	detail, err := ctx.RunRecognition(overflowQuotaAdditionNodeName, img, nil)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "overflow_quota_addition_ocr").
			Msg("failed to run overflow quota addition ocr")
		return 0
	}

	return parseOverflowPlus(extractOCRTexts(detail))
}

func parseOverflowPlus(texts []string) int {
	for _, text := range texts {
		if match := overflowPlusRe.FindStringSubmatch(text); len(match) == 2 {
			plusValue, parseErr := strconv.Atoi(match[1])
			if parseErr == nil {
				return plusValue
			}
		}
	}

	return 0
}

func resolveOverflow(current int, max int, plus int) (overflowDetected bool, overflowAmount int) {
	overflowAmount = current + plus - max
	return overflowAmount > 0, overflowAmount
}

func resolveAbortReasonFromOverflowCurrent(current int) AbortReason {
	if current == 0 {
		return AbortReasonQuotaZero
	}

	return AbortReasonNone
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

func resolveGoodsRegion(ctx *maa.Context) (region string, anchor string, err error) {
	if ctx == nil {
		return "", "", fmt.Errorf("context is nil")
	}

	anchor, err = ctx.GetAnchor(anchorTargetRegionName)
	if err != nil {
		return "", "", fmt.Errorf("get anchor %s: %w", anchorTargetRegionName, err)
	}

	switch anchor {
	case "SceneEnterMenuRegionalDevelopmentValleyIVStockRedistribution":
		return "ValleyIV", anchor, nil
	case "SceneEnterMenuRegionalDevelopmentWulingStockRedistribution":
		return "Wuling", anchor, nil
	default:
		return "", anchor, fmt.Errorf("unexpected anchor value %q", anchor)
	}
}

func runGoodsTemplateMatch(ctx *maa.Context, img image.Image, templatePath string, goodsROI []int) (*maa.RecognitionDetail, error) {
	if err := overrideLocateGoodsRecognition(ctx, templatePath, goodsROI); err != nil {
		return nil, err
	}

	return ctx.RunRecognition(locateGoodsNodeName, img, nil)
}

func overrideLocateGoodsRecognition(ctx *maa.Context, templatePath string, goodsROI []int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	return ctx.OverridePipeline(map[string]any{
		locateGoodsNodeName: map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"template": []string{templatePath},
					"roi":      append([]int(nil), goodsROI...),
				},
			},
		},
	})
}

func pickLowestTemplateHit(detail *maa.RecognitionDetail) (maa.Rect, bool) {
	return pickTemplateHit(detail, func(candidate maa.Rect, current maa.Rect) bool {
		return candidate.Y() > current.Y()
	})
}

func pickTemplateHit(detail *maa.RecognitionDetail, shouldReplace func(candidate maa.Rect, current maa.Rect) bool) (maa.Rect, bool) {
	results := recognitionResults(detail)
	if len(results) == 0 {
		return maa.Rect{}, false
	}

	hit := false
	var selected maa.Rect
	for _, result := range results {
		tm, ok := result.AsTemplateMatch()
		if !ok {
			continue
		}

		if !hit || shouldReplace(tm.Box, selected) {
			selected = tm.Box
			hit = true
		}
	}

	return selected, hit
}

func resolveGoodsRecognitionROI(ctx *maa.Context, img image.Image) []int {
	baseROI := []int{63, 162, 1177, 553}
	marketMarkBox, found, err := runFindMarketMark(ctx, img)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "find_market_mark").
			Msg("failed to locate market mark, use default goods roi")
		return baseROI
	}
	if !found {
		return baseROI
	}

	baseTop := baseROI[1]
	baseBottom := baseTop + baseROI[3]
	adjustedTop := marketMarkBox.Y()
	if adjustedTop <= baseTop || adjustedTop >= baseBottom {
		return baseROI
	}

	adjustedROI := []int{baseROI[0], adjustedTop, baseROI[2], baseBottom - adjustedTop}
	log.Info().
		Str("component", autoStockpileComponent).
		Int("market_mark_y", marketMarkBox.Y()).
		Int("market_mark_height", marketMarkBox.Height()).
		Ints("goods_roi", adjustedROI).
		Msg("goods recognition roi adjusted")
	return adjustedROI
}

func runFindMarketMark(ctx *maa.Context, img image.Image) (maa.Rect, bool, error) {
	detail, err := ctx.RunRecognition(findMarketMarkNodeName, img, nil)
	if err != nil {
		return maa.Rect{}, false, err
	}
	if detail == nil || !detail.Hit {
		resetSelectedGoodsClickROIY(ctx)
		return maa.Rect{}, false, nil
	}

	box, hit := pickTopmostTemplateHit(detail)
	if !hit {
		resetSelectedGoodsClickROIY(ctx)
		return maa.Rect{}, false, nil
	}
	if overrideErr := overrideSelectedGoodsClickROIY(ctx, box.Y()); overrideErr != nil {
		log.Warn().
			Err(overrideErr).
			Str("component", autoStockpileComponent).
			Str("node", selectedGoodsClickNodeName).
			Int("roi_y", box.Y()).
			Msg("failed to override selected goods click roi y")
	}
	return box, hit, nil
}

func pickTopmostTemplateHit(detail *maa.RecognitionDetail) (maa.Rect, bool) {
	return pickTemplateHit(detail, func(candidate maa.Rect, current maa.Rect) bool {
		return candidate.Y() < current.Y()
	})
}

func resetSelectedGoodsClickROIY(ctx *maa.Context) {
	if overrideErr := overrideSelectedGoodsClickROIY(ctx, selectedGoodsClickResetY); overrideErr != nil {
		log.Warn().
			Err(overrideErr).
			Str("component", autoStockpileComponent).
			Str("node", selectedGoodsClickNodeName).
			Int("roi_y", selectedGoodsClickResetY).
			Msg("failed to reset selected goods click roi y")
	}
}

func overrideSelectedGoodsClickROIY(ctx *maa.Context, y int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	node, err := ctx.GetNode(selectedGoodsClickNodeName)
	if err != nil {
		return err
	}

	roi, err := recognitionParamROI(node)
	if err != nil {
		return err
	}
	if len(roi) != 4 {
		return fmt.Errorf("invalid roi length %d", len(roi))
	}

	roi = append([]int(nil), roi...)
	roi[1] = y

	return ctx.OverridePipeline(map[string]any{
		selectedGoodsClickNodeName: map[string]any{
			"roi": roi,
		},
	})
}

func recognitionParamROI(node *maa.Node) ([]int, error) {
	if node == nil || node.Recognition == nil || node.Recognition.Param == nil {
		return nil, fmt.Errorf("node %s missing recognition param", selectedGoodsClickNodeName)
	}

	var target maa.Target
	switch param := node.Recognition.Param.(type) {
	case *maa.TemplateMatchParam:
		target = param.ROI
	case *maa.FeatureMatchParam:
		target = param.ROI
	case *maa.ColorMatchParam:
		target = param.ROI
	case *maa.OCRParam:
		target = param.ROI
	case *maa.NeuralNetworkClassifyParam:
		target = param.ROI
	case *maa.NeuralNetworkDetectParam:
		target = param.ROI
	case *maa.CustomRecognitionParam:
		target = param.ROI
	default:
		return nil, fmt.Errorf("node %s has unsupported recognition param type %T", selectedGoodsClickNodeName, node.Recognition.Param)
	}

	rect, err := target.AsRect()
	if err != nil {
		return nil, fmt.Errorf("node %s roi: %w", selectedGoodsClickNodeName, err)
	}

	return []int{rect[0], rect[1], rect[2], rect[3]}, nil
}

func runGoodsOCR(ctx *maa.Context, img image.Image, goodsROI []int, itemMap *ItemMap) ([]priceCandidate, []ocrNameCandidate, AbortReason, error) {
	if err := overrideGoodsPriceROI(ctx, goodsROI); err != nil {
		return nil, nil, AbortReasonGoodsOCRUnavailableWarn, err
	}

	detail, err := ctx.RunRecognition(goodsPriceNodeName, img, nil)
	if err != nil {
		return nil, nil, AbortReasonGoodsOCRUnavailableWarn, err
	}

	results := filteredOCRResults(detail)
	if len(results) == 0 {
		return nil, nil, AbortReasonGoodsOCRUnavailableWarn, nil
	}

	prices := make([]priceCandidate, 0, len(results))
	ocrNames := make([]ocrNameCandidate, 0, len(results))
	seenPrice := make(map[string]struct{}, len(results))
	seenName := make(map[string]struct{}, len(results))
	for _, result := range results {
		ocrResult, ok := result.AsOCR()
		if !ok {
			continue
		}

		text := strings.TrimSpace(ocrResult.Text)
		if text == "" {
			continue
		}

		if match := priceRe.FindStringSubmatch(text); len(match) == 2 {
			priceText := match[1]
			price, parseErr := strconv.Atoi(priceText)
			if parseErr != nil {
				continue
			}

			key := fmt.Sprintf("%d:%d:%d:%d:%s", ocrResult.Box.X(), ocrResult.Box.Y(), ocrResult.Box.Width(), ocrResult.Box.Height(), priceText)
			if _, exists := seenPrice[key]; exists {
				continue
			}
			seenPrice[key] = struct{}{}

			prices = append(prices, priceCandidate{
				value: price,
				text:  priceText,
				box:   ocrResult.Box,
			})
			continue
		}

		id, name, matched := MatchGoodsName(text, itemMap, 2)
		if !matched {
			continue
		}

		nameKey := fmt.Sprintf("%d:%d:%d:%d:%s", ocrResult.Box.X(), ocrResult.Box.Y(), ocrResult.Box.Width(), ocrResult.Box.Height(), id)
		if _, exists := seenName[nameKey]; exists {
			continue
		}
		seenName[nameKey] = struct{}{}

		ocrNames = append(ocrNames, ocrNameCandidate{
			id:   id,
			name: name,
			tier: ParseTierFromID(id),
			box:  ocrResult.Box,
		})
	}

	sort.Slice(prices, func(i, j int) bool {
		if prices[i].box.Y() == prices[j].box.Y() {
			return prices[i].box.X() < prices[j].box.X()
		}
		return prices[i].box.Y() < prices[j].box.Y()
	})

	return prices, ocrNames, AbortReasonNone, nil
}

func validateRecognizedGoodsTiers(goods []GoodsItem) error {
	for _, item := range goods {
		if item.Tier == "" {
			return fmt.Errorf("goods %s (%s) has empty tier", item.Name, item.ID)
		}
	}

	return nil
}

func overrideGoodsPriceROI(ctx *maa.Context, goodsROI []int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	return ctx.OverridePipeline(map[string]any{
		goodsPriceNodeName: map[string]any{
			"recognition": map[string]any{
				"param": map[string]any{
					"roi": append([]int(nil), goodsROI...),
				},
			},
		},
	})
}

func bindPriceToGoods(goods goodsCandidate, prices []priceCandidate, used []bool) (int, bool) {
	goodsBottomY := goods.box.Y() + goods.box.Height()

	bestIdx, bestDistance, ok := findBestPriceCandidate(prices, used, func(price priceCandidate) (int, bool) {
		if price.box.Y() <= goods.box.Y() {
			return 0, false
		}
		if price.box.X() <= (goods.box.X() - 50) {
			return 0, false
		}

		distanceY := absInt(goodsBottomY - price.box.Y())
		distanceX := price.box.X() - goods.box.X()
		distance := int(math.Hypot(float64(distanceY), float64(distanceX)))
		if distance > MAX_DISTANCE {
			return 0, false
		}

		return distance, true
	})
	if !ok {
		return 0, false
	}
	markPriceCandidateUsed(used, bestIdx)

	log.Info().
		Str("component", autoStockpileComponent).
		Str("bind_pass", "template").
		Str("goods_id", goods.item.ID).
		Str("goods_name", goods.item.Name).
		Str("tier", goods.item.Tier).
		Int("price", prices[bestIdx].value).
		Int("goods_bottom_y", goodsBottomY).
		Int("price_y", prices[bestIdx].box.Y()).
		Int("distance", bestDistance).
		Msg("price bound to goods")

	return prices[bestIdx].value, true
}

func bindPriceToOCRGoods(goods ocrNameCandidate, prices []priceCandidate, used []bool) (int, bool) {
	bestIdx, bestDistance, ok := findBestPriceCandidate(prices, used, func(price priceCandidate) (int, bool) {
		// 当前界面中，Y 轴从上到下依次是“商品图片 -> 商品价格 -> 商品名称”。
		// 这里的 goods.box 来自 OCR 识别到的商品名称区域，因此价格框应当位于名称框上方，
		// 即 price.box.Y() 必须小于 goods.box.Y()；否则说明不是该商品对应的价格。
		if price.box.Y() >= goods.box.Y() {
			return 0, false
		}
		if price.box.X() <= goods.box.X() {
			return 0, false
		}

		distanceY := absInt(goods.box.Y() - price.box.Y())
		distanceX := price.box.X() - goods.box.X()
		distance := int(math.Hypot(float64(distanceY), float64(distanceX)))
		if distance > MAX_DISTANCE {
			return 0, false
		}

		return distance, true
	})
	if !ok {
		return 0, false
	}
	markPriceCandidateUsed(used, bestIdx)

	log.Info().
		Str("component", autoStockpileComponent).
		Str("bind_pass", "ocr").
		Str("goods_id", goods.id).
		Str("goods_name", goods.name).
		Str("tier", goods.tier).
		Int("price", prices[bestIdx].value).
		Int("goods_y", goods.box.Y()).
		Int("price_y", prices[bestIdx].box.Y()).
		Int("distance", bestDistance).
		Msg("price bound to goods")

	return prices[bestIdx].value, true
}

func findBestPriceCandidate(prices []priceCandidate, used []bool, candidateDistance func(price priceCandidate) (int, bool)) (int, int, bool) {
	bestIdx := -1
	bestDistance := 0

	for i, price := range prices {
		if i < len(used) && used[i] {
			continue
		}

		distance, ok := candidateDistance(price)
		if !ok {
			continue
		}

		if bestIdx < 0 || distance < bestDistance {
			bestIdx = i
			bestDistance = distance
		}
	}

	if bestIdx < 0 {
		return 0, 0, false
	}

	return bestIdx, bestDistance, true
}

func markPriceCandidateUsed(used []bool, idx int) {
	if idx < len(used) {
		used[idx] = true
	}
}

func extractOCRTexts(detail *maa.RecognitionDetail) []string {
	results := recognitionResults(detail)
	if len(results) == 0 {
		return nil
	}

	texts := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for _, result := range results {
		ocrResult, ok := result.AsOCR()
		if !ok {
			continue
		}
		text := strings.TrimSpace(ocrResult.Text)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		texts = append(texts, text)
	}

	return texts
}

func recognitionResults(detail *maa.RecognitionDetail) []*maa.RecognitionResult {
	if detail == nil || detail.Results == nil {
		return nil
	}
	if len(detail.Results.Filtered) > 0 {
		return detail.Results.Filtered
	}
	if len(detail.Results.All) > 0 {
		return detail.Results.All
	}
	if detail.Results.Best != nil {
		return []*maa.RecognitionResult{detail.Results.Best}
	}
	return nil
}

// filteredOCRResults 仅返回 OCR filtered 结果，不进行任何回退。
// 该函数专门用于商品 OCR 链路，确保只使用颜色过滤后的高置信度结果。
func filteredOCRResults(detail *maa.RecognitionDetail) []*maa.RecognitionResult {
	if detail == nil || detail.Results == nil {
		return nil
	}
	if len(detail.Results.Filtered) > 0 {
		return detail.Results.Filtered
	}
	return nil
}
