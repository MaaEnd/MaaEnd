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
