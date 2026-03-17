package autostockpile

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

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
	overflowNodeName              = "AutoStockpileCheckOverflow"
	overflowDetailNodeName        = "AutoStockpileGetOverflowDetail"
	locateGoodsNodeName           = "AutoStockpileLocateGoods"
	goodsPriceNodeName            = "AutoStockpileGetGoodsPrice"
	// MAX_DISTANCE 表示商品与价格框可接受的最大匹配距离。
	MAX_DISTANCE = 120
)

var (
	overflowCurrentMaxRe = regexp.MustCompile(`(\d+)\s*/\s*(\d+)`)
	overflowPlusRe       = regexp.MustCompile(`\+(\d+)`)
	priceRe              = regexp.MustCompile(`^[^\d]?(\d{3,4})$`)
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

type goodsTemplate struct {
	name         string
	tier         string
	templatePath string
}

// Run 执行 AutoStockpile 自定义识别，并返回包含商品与价格信息的结构化结果。
func (r *ItemValueChangeRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		log.Error().
			Str("component", autoStockpileComponent).
			Msg("custom recognition arg or image is nil")
		return nil, false
	}

	overflowDetected, err := runOverflowColorMatch(ctx, arg.Img)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "overflow_color_match").
			Msg("failed to run overflow color match")
	}

	overflowAmount := 0
	if overflowDetected {
		if cur, max, plus, ok := runOverflowDetailOCR(ctx, arg.Img); ok {
			overflowAmount = cur + plus - max
			log.Info().
				Str("component", autoStockpileComponent).
				Int("overflow_current", cur).
				Int("overflow_max", max).
				Int("overflow_plus", plus).
				Int("overflow_amount", overflowAmount).
				Msg("overflow detail parsed")

			if overflowAmount <= 0 {
				overflowDetected = false
			}

			if overflowAmount > 0 {
				if err := overrideSwipeSpecificQuantityTarget(ctx, overflowAmount); err != nil {
					log.Warn().
						Err(err).
						Str("component", autoStockpileComponent).
						Str("node", swipeSpecificQuantityNodeName).
						Int("overflow_amount", overflowAmount).
						Msg("failed to override swipe specific quantity target")
				}
			}
		}
	}

	region, anchor := resolveGoodsRegion(ctx)
	log.Info().
		Str("component", autoStockpileComponent).
		Str("anchor", anchor).
		Str("region", region).
		Msg("goods region resolved")

	goodsROI := resolveGoodsRecognitionROI(ctx, arg.Img)
	prices, ocrNames, err := runGoodsOCR(ctx, arg.Img, goodsROI)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "goods_ocr").
			Msg("failed to run goods ocr")
		prices = nil
		ocrNames = nil
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

	templates, goodsDir, err := listGoodsTemplates(region)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("region", region).
			Msg("failed to read goods templates")
		return nil, false
	}
	log.Info().
		Str("component", autoStockpileComponent).
		Str("region", region).
		Str("goods_dir", goodsDir).
		Int("template_count", len(templates)).
		Msg("goods templates loaded")

	itemMap := GetItemMap()
	goods := make([]goodsCandidate, 0, len(templates))
	for _, tpl := range templates {
		id := BuildIDFromTemplatePath(tpl.templatePath)
		if boundIDs[id] {
			continue
		}

		detail, recErr := runGoodsTemplateMatch(ctx, arg.Img, tpl.templatePath, goodsROI)
		if recErr != nil {
			log.Warn().
				Err(recErr).
				Str("component", autoStockpileComponent).
				Str("template", tpl.templatePath).
				Msg("template match failed")
			continue
		}

		box, hit := pickLowestTemplateHit(detail)
		if !hit {
			continue
		}

		itemName := tpl.name
		if name, ok := itemMap.IDToName[id]; ok {
			itemName = name
		}

		tier := ParseTierFromID(id)
		if tier == "" {
			tier = tpl.tier
		}

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

	resultPayload := RecognitionResult{
		Overflow:       overflowDetected,
		OverflowAmount: overflowAmount,
		Sunday:         time.Now().Weekday() == time.Sunday,
		Goods:          resultGoods,
	}

	resultDetail, err := json.Marshal(resultPayload)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", autoStockpileComponent).
			Msg("failed to marshal recognition result")
		return nil, false
	}

	log.Info().
		Str("component", autoStockpileComponent).
		Bool("overflow", resultPayload.Overflow).
		Bool("sunday", resultPayload.Sunday).
		Int("goods_count", len(resultPayload.Goods)).
		Msg("custom recognition finished")

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(resultDetail),
	}, len(resultGoods) > 0
}

// TODO: 当前 ColorMatch 溢出识别不够准确，需要改进。
func runOverflowColorMatch(ctx *maa.Context, img image.Image) (bool, error) {
	config := map[string]any{
		overflowNodeName: map[string]any{
			"recognition": "ColorMatch",
			"roi":         []int{250, 135, 325, 30},
			"method":      40,
			"lower":       [][]int{{0, 200, 200}},
			"upper":       [][]int{{20, 255, 255}},
			"count":       100,
		},
	}

	detail, err := ctx.RunRecognition(overflowNodeName, img, config)
	if err != nil {
		return false, err
	}

	hit := detail != nil && detail.Hit
	log.Info().
		Str("component", autoStockpileComponent).
		Bool("overflow_hit", hit).
		Msg("overflow color match completed")
	return hit, nil
}

func runOverflowDetailOCR(ctx *maa.Context, img image.Image) (current int, max int, plus int, ok bool) {
	config := map[string]any{
		overflowDetailNodeName: map[string]any{
			"recognition": "OCR",
			"roi":         []int{35, 125, 773, 47},
			"expected":    []string{".*"},
		},
	}

	detail, err := ctx.RunRecognition(overflowDetailNodeName, img, config)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "overflow_detail_ocr").
			Msg("failed to run overflow detail ocr")
		return 0, 0, 0, false
	}

	for _, text := range extractOCRTexts(detail) {
		if current == 0 || max == 0 {
			if match := overflowCurrentMaxRe.FindStringSubmatch(text); len(match) == 3 {
				cur, curErr := strconv.Atoi(match[1])
				maxValue, maxErr := strconv.Atoi(match[2])
				if curErr == nil && maxErr == nil {
					current = cur
					max = maxValue
				}
			}
		}
		if plus == 0 {
			if match := overflowPlusRe.FindStringSubmatch(text); len(match) == 2 {
				plusValue, parseErr := strconv.Atoi(match[1])
				if parseErr == nil {
					plus = plusValue
				}
			}
		}
	}

	return current, max, plus, current > 0 || max > 0 || plus > 0
}

func overrideSwipeSpecificQuantityTarget(ctx *maa.Context, overflowAmount int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	customActionParam, err := loadSwipeSpecificQuantityCustomActionParam(ctx)
	if err != nil {
		return err
	}

	return ctx.OverridePipeline(map[string]any{
		swipeSpecificQuantityNodeName: buildSwipeSpecificQuantityTargetOverride(customActionParam, overflowAmount),
	})
}

func loadSwipeSpecificQuantityCustomActionParam(ctx *maa.Context) (map[string]any, error) {
	node, err := ctx.GetNode(swipeSpecificQuantityNodeName)
	if err != nil {
		return nil, err
	}

	if node.Action == nil {
		return nil, fmt.Errorf("node %s missing action", swipeSpecificQuantityNodeName)
	}

	param, ok := node.Action.Param.(*maa.CustomActionParam)
	if !ok || param == nil {
		return nil, fmt.Errorf("node %s action param type %T is not *maa.CustomActionParam", swipeSpecificQuantityNodeName, node.Action.Param)
	}

	return normalizeCustomActionParam(param.CustomActionParam)
}

func buildSwipeSpecificQuantityTargetOverride(customActionParam map[string]any, overflowAmount int) map[string]any {
	clonedParam := make(map[string]any, len(customActionParam))
	for key, item := range customActionParam {
		clonedParam[key] = item
	}
	clonedParam["Target"] = overflowAmount

	return map[string]any{
		"action": map[string]any{
			"param": map[string]any{
				"custom_action_param": clonedParam,
			},
		},
	}
}

func normalizeCustomActionParam(raw any) (map[string]any, error) {
	switch value := raw.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(value))
		for key, item := range value {
			cloned[key] = item
		}
		return cloned, nil
	case string:
		var nested any
		if err := json.Unmarshal([]byte(value), &nested); err != nil {
			return nil, err
		}
		return normalizeCustomActionParam(nested)
	default:
		return nil, fmt.Errorf("unsupported custom_action_param type %T", raw)
	}
}

func resolveGoodsRegion(ctx *maa.Context) (region string, anchor string) {
	if ctx == nil {
		return "Wuling", ""
	}

	anchor, err := ctx.GetAnchor(anchorTargetRegionName)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("anchor_name", anchorTargetRegionName).
			Msg("failed to get anchor, fallback to Wuling")
		return "Wuling", ""
	}

	switch anchor {
	case "GoToValleyIV":
		return "ValleyIV", anchor
	case "GoToWuling":
		return "Wuling", anchor
	default:
		log.Warn().
			Str("component", autoStockpileComponent).
			Str("anchor", anchor).
			Msg("unexpected anchor value, fallback to Wuling")
		return "Wuling", anchor
	}
}

func listGoodsTemplates(region string) ([]goodsTemplate, string, error) {
	baseParts := []string{"assets", "resource", "image", "AutoStockpile", "Goods", region}
	pathCandidates := []string{
		filepath.Join(baseParts...),
		filepath.Join("..", "..", filepath.Join(baseParts...)),
		filepath.Join("..", filepath.Join(baseParts...)),
	}

	var lastErr error
	for _, goodsDir := range pathCandidates {
		entries, err := os.ReadDir(goodsDir)
		if err != nil {
			lastErr = err
			continue
		}

		templates := make([]goodsTemplate, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".png") {
				continue
			}

			name, tier, ok := parseGoodsFileName(entry.Name(), region)
			if !ok {
				log.Warn().
					Str("component", autoStockpileComponent).
					Str("filename", entry.Name()).
					Msg("invalid goods filename format, skip")
				continue
			}

			templates = append(templates, goodsTemplate{
				name:         name,
				tier:         tier,
				templatePath: filepath.ToSlash(filepath.Join("AutoStockpile", "Goods", region, entry.Name())),
			})
		}

		sort.Slice(templates, func(i, j int) bool {
			return templates[i].templatePath < templates[j].templatePath
		})

		return templates, goodsDir, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("no valid goods directory found for region %s", region)
	}
	return nil, "", lastErr
}

func parseGoodsFileName(filename, region string) (name string, tier string, ok bool) {
	parts := strings.Split(filename, ".")
	if len(parts) < 3 {
		return "", "", false
	}

	name = parts[0]
	tierPart := parts[1]
	tierLevel := strings.TrimPrefix(tierPart, "Tier")
	if tierLevel == "" || tierLevel == tierPart {
		return "", "", false
	}

	if _, err := strconv.Atoi(tierLevel); err != nil {
		return "", "", false
	}

	tier = region + "Tier" + tierLevel
	return name, tier, true
}

func runGoodsTemplateMatch(ctx *maa.Context, img image.Image, templatePath string, goodsROI []int) (*maa.RecognitionDetail, error) {
	config := map[string]any{
		locateGoodsNodeName: map[string]any{
			"recognition": "TemplateMatch",
			"template":    templatePath,
			"threshold":   0.8,
			"roi":         goodsROI,
		},
	}

	return ctx.RunRecognition(locateGoodsNodeName, img, config)
}

func pickLowestTemplateHit(detail *maa.RecognitionDetail) (maa.Rect, bool) {
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

		if !hit || tm.Box.Y() > selected.Y() {
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
		if overrideErr := overrideSelectedGoodsClickROIY(ctx, selectedGoodsClickResetY); overrideErr != nil {
			log.Warn().
				Err(overrideErr).
				Str("component", autoStockpileComponent).
				Str("node", selectedGoodsClickNodeName).
				Int("roi_y", selectedGoodsClickResetY).
				Msg("failed to reset selected goods click roi y")
		}
		return maa.Rect{}, false, nil
	}

	box, hit := pickTopmostTemplateHit(detail)
	if !hit {
		if overrideErr := overrideSelectedGoodsClickROIY(ctx, selectedGoodsClickResetY); overrideErr != nil {
			log.Warn().
				Err(overrideErr).
				Str("component", autoStockpileComponent).
				Str("node", selectedGoodsClickNodeName).
				Int("roi_y", selectedGoodsClickResetY).
				Msg("failed to reset selected goods click roi y")
		}
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

		if !hit || tm.Box.Y() < selected.Y() {
			selected = tm.Box
			hit = true
		}
	}

	return selected, hit
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

func runGoodsOCR(ctx *maa.Context, img image.Image, goodsROI []int) ([]priceCandidate, []ocrNameCandidate, error) {
	config := map[string]any{
		goodsPriceNodeName: map[string]any{
			"recognition": "OCR",
			"expected":    []string{".*"},
			"roi":         goodsROI,
		},
	}

	detail, err := ctx.RunRecognition(goodsPriceNodeName, img, config)
	if err != nil {
		return nil, nil, err
	}

	results := recognitionResults(detail)
	if len(results) == 0 {
		return nil, nil, nil
	}

	prices := make([]priceCandidate, 0, len(results))
	ocrNames := make([]ocrNameCandidate, 0, len(results))
	seenPrice := make(map[string]struct{}, len(results))
	seenName := make(map[string]struct{}, len(results))
	itemMap := GetItemMap()
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

	return prices, ocrNames, nil
}

func bindPriceToGoods(goods goodsCandidate, prices []priceCandidate, used []bool) (int, bool) {
	bestIdx := -1
	bestDistance := 0
	goodsBottomY := goods.box.Y() + goods.box.Height()

	for i, price := range prices {
		if i < len(used) && used[i] {
			continue
		}
		if price.box.Y() <= goods.box.Y() {
			continue
		}
		if price.box.X() <= (goods.box.X() - 50) {
			continue
		}

		distanceY := absInt(goodsBottomY - price.box.Y())
		distanceX := price.box.X() - goods.box.X()
		distance := int(math.Hypot(float64(distanceY), float64(distanceX)))
		if distance > MAX_DISTANCE {
			continue
		}

		if bestIdx < 0 || distance < bestDistance {
			bestIdx = i
			bestDistance = distance
		}
	}

	if bestIdx < 0 {
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
	bestIdx := -1
	bestDistance := 0

	for i, price := range prices {
		if i < len(used) && used[i] {
			continue
		}
		// 当前界面中，Y 轴从上到下依次是“商品图片 -> 商品价格 -> 商品名称”。
		// 这里的 goods.box 来自 OCR 识别到的商品名称区域，因此价格框应当位于名称框上方，
		// 即 price.box.Y() 必须小于 goods.box.Y()；否则说明不是该商品对应的价格。
		if price.box.Y() >= goods.box.Y() {
			continue
		}
		if price.box.X() <= goods.box.X() {
			continue
		}

		distanceY := absInt(goods.box.Y() - price.box.Y())
		distanceX := price.box.X() - goods.box.X()
		distance := int(math.Hypot(float64(distanceY), float64(distanceX)))
		if distance > MAX_DISTANCE {
			continue
		}

		if bestIdx < 0 || distance < bestDistance {
			bestIdx = i
			bestDistance = distance
		}
	}

	if bestIdx < 0 {
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

// BuildIDFromTemplatePath 根据模板路径构造商品 ID。
func BuildIDFromTemplatePath(templatePath string) string {
	trimmed := strings.TrimPrefix(templatePath, "AutoStockpile/Goods/")
	trimmed = strings.TrimSuffix(trimmed, ".png")
	return trimmed
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
