package autostockpile

import (
	"fmt"
	"image"
	"math"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	maxGoodsPriceDistance = 120
	testPricesEnvVar      = "MAAEND_AUTOSTOCKPILE_RECOGNITION_TEST_PRICES"
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

func runGoodsTemplateMatch(ctx *maa.Context, img image.Image, templatePath string, goodsROI []int) (*maa.RecognitionDetail, error) {
	if err := overrideLocateGoodsRecognition(ctx, templatePath, goodsROI); err != nil {
		return nil, err
	}

	return ctx.RunRecognition(locateGoodsNodeName, img, nil)
}

// scanGoodsOnImage 对单帧截图执行 OCR 绑价与模板补全，返回本页识别到的商品列表。
func scanGoodsOnImage(ctx *maa.Context, img image.Image, region string, itemMap *ItemMap) ([]GoodsItem, AbortReason, error) {
	if ctx == nil || img == nil {
		return nil, AbortReasonGoodsOCRUnavailableWarn, fmt.Errorf("ctx or image is nil")
	}
	if err := validateItemMap(itemMap); err != nil {
		return nil, AbortReasonNone, err
	}

	goodsROI := resolveGoodsRecognitionROI(ctx, img)
	prices, ocrNames, goodsOCRAbortReason, goodsOCRErr := runGoodsOCR(ctx, img, goodsROI, itemMap)
	if goodsOCRAbortReason != AbortReasonNone {
		return nil, goodsOCRAbortReason, goodsOCRErr
	}
	log.Info().
		Str("component", autoStockpileComponent).
		Str("step", "scan_page").
		Str("region", region).
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

		detail, recErr := runGoodsTemplateMatch(ctx, img, templatePath, goodsROI)
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

	return resultGoods, AbortReasonNone, nil
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

	box, hit := bestTemplateHit(detail)
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

func runGoodsOCR(ctx *maa.Context, img image.Image, goodsROI []int, itemMap *ItemMap) ([]priceCandidate, []ocrNameCandidate, AbortReason, error) {
	if err := overrideGoodsPriceROI(ctx, goodsROI); err != nil {
		return nil, nil, AbortReasonGoodsOCRUnavailableWarn, err
	}

	detail, err := ctx.RunRecognition(goodsPriceNodeName, img, nil)
	if err != nil {
		return nil, nil, AbortReasonGoodsOCRUnavailableWarn, err
	}

	results := filteredOCRCandidates(detail)
	if len(results) == 0 {
		return nil, nil, AbortReasonGoodsOCRUnavailableWarn, nil
	}

	prices := make([]priceCandidate, 0, len(results))
	ocrNames := make([]ocrNameCandidate, 0, len(results))
	seenPrice := make(map[string]struct{}, len(results))
	seenName := make(map[string]struct{}, len(results))
	for _, result := range results {
		text := strings.TrimSpace(result.Text)
		if text == "" {
			continue
		}

		if match := priceRe.FindStringSubmatch(text); len(match) == 2 {
			priceText := match[1]
			price, parseErr := strconv.Atoi(priceText)
			if parseErr != nil {
				continue
			}

			key := fmt.Sprintf("%d:%d:%d:%d:%s", result.Box.X(), result.Box.Y(), result.Box.Width(), result.Box.Height(), priceText)
			if _, exists := seenPrice[key]; exists {
				continue
			}
			seenPrice[key] = struct{}{}

			prices = append(prices, priceCandidate{
				value: price,
				text:  priceText,
				box:   result.Box,
			})
			continue
		}

		id, name, matched := MatchGoodsName(text, itemMap, 2)
		if !matched {
			continue
		}

		nameKey := fmt.Sprintf("%d:%d:%d:%d:%s", result.Box.X(), result.Box.Y(), result.Box.Width(), result.Box.Height(), id)
		if _, exists := seenName[nameKey]; exists {
			continue
		}
		seenName[nameKey] = struct{}{}

		ocrNames = append(ocrNames, ocrNameCandidate{
			id:   id,
			name: name,
			tier: ParseTierFromID(id),
			box:  result.Box,
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

func applyTestPricesIfEnabled(goods []GoodsItem) {
	if os.Getenv(testPricesEnvVar) == "" {
		return
	}

	if len(goods) == 0 {
		return
	}

	if len(goods) == 1 {
		goods[0].Price = 200
		log.Info().
			Str("component", autoStockpileComponent).
			Str("goods_id", goods[0].ID).
			Str("goods_name", goods[0].Name).
			Int("new_price", 200).
			Msg("test price rewrite applied (1 item)")
		return
	}

	indices := make([]int, len(goods))
	for i := range indices {
		indices[i] = i
	}

	rand.Shuffle(len(indices), func(i, j int) {
		indices[i], indices[j] = indices[j], indices[i]
	})

	targetCount100 := 2
	targetCount200 := 1

	if len(goods) == 2 {
		targetCount100 = 1
		targetCount200 = 1
	} else if len(goods) >= 3 {
		targetCount100 = 2
		targetCount200 = 1
	}

	count100 := 0
	for i := 0; i < len(indices) && count100 < targetCount100; i++ {
		goods[indices[i]].Price = 100
		log.Info().
			Str("component", autoStockpileComponent).
			Str("goods_id", goods[indices[i]].ID).
			Str("goods_name", goods[indices[i]].Name).
			Int("new_price", 100).
			Msg("test price rewrite applied (100)")
		count100++
	}

	count200 := 0
	for i := targetCount100; i < len(indices) && count200 < targetCount200; i++ {
		goods[indices[i]].Price = 200
		log.Info().
			Str("component", autoStockpileComponent).
			Str("goods_id", goods[indices[i]].ID).
			Str("goods_name", goods[indices[i]].Name).
			Int("new_price", 200).
			Msg("test price rewrite applied (200)")
		count200++
	}

	log.Info().
		Str("component", autoStockpileComponent).
		Int("total_goods", len(goods)).
		Int("modified_count_100", count100).
		Int("modified_count_200", count200).
		Msg("test price rewrite finished")
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
		if distance > maxGoodsPriceDistance {
			return 0, false
		}

		return distance, true
	})
	if !ok {
		return 0, false
	}
	if bestIdx < len(used) {
		used[bestIdx] = true
	}

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
		if price.box.Y() >= goods.box.Y() {
			return 0, false
		}
		if price.box.X() <= goods.box.X() {
			return 0, false
		}

		distanceY := absInt(goods.box.Y() - price.box.Y())
		distanceX := price.box.X() - goods.box.X()
		distance := int(math.Hypot(float64(distanceY), float64(distanceX)))
		if distance > maxGoodsPriceDistance {
			return 0, false
		}

		return distance, true
	})
	if !ok {
		return 0, false
	}
	if bestIdx < len(used) {
		used[bestIdx] = true
	}

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
