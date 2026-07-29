package goods

import (
	"fmt"
	"image"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/internal/ocrmatch"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	stockCellAnchorNodeName       = "SellProductCheckGoodsCellAnchor"
	stockQuantityFragmentMaxGapX  = 16
	stockGridRowToleranceY        = 32
	stockAnchorDedupToleranceAxis = 8
)

var (
	stockQuantityPattern      = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*(万|萬|만|k|m)?`)
	stockQuantityContinuation = regexp.MustCompile(`(?i)^(?:\d+|[.,]\d+|万|萬|만|k|m)$`)
)

type stockPageItem struct {
	ItemID     string
	StockBox   maa.Rect
	ClickBox   maa.Rect
	Quantity   int64
	StockKnown bool
}

type stockPageScan struct {
	Items []stockPageItem
}

// stockCellOffsets 是 Pipeline 按平台提供的货品格子相对锚点区域。
// Go 只消费这些区域完成名称归属、库存 OCR 和安全点击，不维护平台布局坐标。
type stockCellOffsets struct {
	Name     maa.Rect
	Quantity maa.Rect
	Click    maa.Rect
}

type stockNameMatch struct {
	itemID string
	box    maa.Rect
}

type stockQuantityFallback func(maa.Rect) (string, error)

func recognizeStockPage(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
	groups []itemPriorityGroup,
	offsets stockCellOffsets,
) (*stockPageScan, error) {
	anchorBoxes, err := recognizeStockCellAnchors(ctx, arg.Img)
	if err != nil {
		return nil, err
	}
	if len(anchorBoxes) == 0 {
		return nil, fmt.Errorf("no goods cell anchors recognized")
	}

	ocrDetail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, maa.OCRParam{
		ROI: maa.NewTargetRect(arg.Roi),
	}, arg.Img)
	if err != nil || ocrDetail == nil {
		return nil, fmt.Errorf("recognize goods page text: %w", err)
	}
	items, err := buildStockPageItems(
		ocrmatch.CollectResults(ocrDetail),
		anchorBoxes,
		groups,
		offsets,
		arg.Img.Bounds(),
		func(stockBox maa.Rect) (string, error) {
			return recognizeStockQuantityOnlyRec(ctx, arg.Img, stockBox)
		},
	)
	if err != nil {
		return nil, err
	}
	return &stockPageScan{Items: items}, nil
}

// buildStockPageItems 将一次全页 OCR 的名称和库存文本按格子锚点归属到对应货品。
func buildStockPageItems(
	ocrItems []ocrmatch.Item,
	anchorBoxes []maa.Rect,
	groups []itemPriorityGroup,
	offsets stockCellOffsets,
	bounds image.Rectangle,
	fallback stockQuantityFallback,
) ([]stockPageItem, error) {
	nameMatches := matchStockPageNames(ocrItems, groups)
	if len(nameMatches) < len(anchorBoxes) {
		return nil, fmt.Errorf("item name count %d is smaller than anchor count %d", len(nameMatches), len(anchorBoxes))
	}

	items := make([]stockPageItem, 0, len(nameMatches))
	usedNames := make(map[string]struct{}, len(anchorBoxes))
	lowestCompleteNameBottom := 0
	for _, anchorBox := range anchorBoxes {
		nameBox := stockCellBox(anchorBox, offsets.Name, bounds)
		nameMatch, ok := findStockNameForAnchor(nameMatches, usedNames, nameBox)
		if !ok {
			return nil, fmt.Errorf("no item name found in %v for anchor %v", nameBox, anchorBox)
		}
		usedNames[nameMatch.itemID] = struct{}{}
		lowestCompleteNameBottom = max(lowestCompleteNameBottom, nameMatch.box.Y()+nameMatch.box.Height())
		stockBox := stockCellBox(anchorBox, offsets.Quantity, bounds)
		raw := collectStockQuantityText(ocrItems, stockBox)
		quantity, ok := parseStockQuantity(raw)
		source := "page_ocr"
		// 全页 OCR 的检测阶段可能漏掉较小的库存数字；只对缺失格子在同一帧精确识别一次。
		if !ok && fallback != nil {
			fallbackRaw, err := fallback(stockBox)
			if err != nil {
				return nil, fmt.Errorf("item %q stock fallback failed in %v: %w", nameMatch.itemID, stockBox, err)
			}
			raw = fallbackRaw
			quantity, ok = parseStockQuantity(raw)
			source = "only_rec_fallback"
		}
		if !ok {
			return nil, fmt.Errorf("item %q stock text %q is invalid in %v", nameMatch.itemID, raw, stockBox)
		}
		log.Debug().
			Str("component", priorityItemRecognitionName).
			Str("item_id", nameMatch.itemID).
			Str("stock_ocr", raw).
			Str("stock_ocr_source", source).
			Int64("stock_quantity", quantity).
			Interface("anchor_box", anchorBox).
			Interface("name_box", nameMatch.box).
			Interface("stock_box", stockBox).
			Msg("goods cell stock recognized")
		items = append(items, stockPageItem{
			ItemID:     nameMatch.itemID,
			StockBox:   stockBox,
			ClickBox:   stockCellBox(anchorBox, offsets.Click, bounds),
			Quantity:   quantity,
			StockKnown: true,
		})
	}

	// 列表底部可能只露出下一行名称。它没有完整格子锚点和库存区域，
	// 但稀有度、单价策略仍可使用名称框安全点击。
	for _, nameMatch := range nameMatches {
		if _, used := usedNames[nameMatch.itemID]; used {
			continue
		}
		if nameMatch.box.Y() < lowestCompleteNameBottom ||
			!stockNameWithinAnchorColumns(nameMatch.box, anchorBoxes, offsets.Name, bounds) {
			continue
		}
		items = append(items, stockPageItem{
			ItemID:     nameMatch.itemID,
			ClickBox:   nameMatch.box,
			StockKnown: false,
		})
	}
	return items, nil
}

func recognizeStockQuantityOnlyRec(ctx *maa.Context, img image.Image, roi maa.Rect) (string, error) {
	detail, err := ctx.RunRecognitionDirect(maa.RecognitionTypeOCR, maa.OCRParam{
		ROI:     maa.NewTargetRect(roi),
		OnlyRec: true,
	}, img)
	if err != nil {
		return "", fmt.Errorf("recognize stock quantity: %w", err)
	}
	if detail == nil {
		return "", fmt.Errorf("recognize stock quantity returned no detail")
	}
	for _, item := range ocrmatch.CollectResults(detail) {
		if _, ok := parseStockQuantity(item.Text); ok {
			return item.Text, nil
		}
	}
	return "", fmt.Errorf("no stock quantity recognized")
}

func stockNameWithinAnchorColumns(
	nameBox maa.Rect,
	anchorBoxes []maa.Rect,
	nameOffset maa.Rect,
	bounds image.Rectangle,
) bool {
	if len(anchorBoxes) == 0 {
		return false
	}
	left := bounds.Max.X
	right := bounds.Min.X
	for _, anchorBox := range anchorBoxes {
		box := stockCellBox(anchorBox, nameOffset, bounds)
		left = min(left, box.X())
		right = max(right, box.X()+box.Width())
	}
	return nameBox.X() < right && nameBox.X()+nameBox.Width() > left
}

func matchStockPageNames(ocrItems []ocrmatch.Item, groups []itemPriorityGroup) []stockNameMatch {
	matches := make([]stockNameMatch, 0, len(groups))
	for _, group := range groups {
		match := ocrmatch.FindBest(ocrItems, group.Candidates)
		if match == nil {
			continue
		}
		matches = append(matches, stockNameMatch{itemID: group.ItemID, box: match.Box})
	}
	sortStockNameMatchesByGrid(matches)
	return matches
}

func findStockNameForAnchor(
	matches []stockNameMatch,
	used map[string]struct{},
	nameBox maa.Rect,
) (stockNameMatch, bool) {
	for _, match := range matches {
		if _, exists := used[match.itemID]; exists {
			continue
		}
		if rectsOverlap(match.box, nameBox) {
			return match, true
		}
	}
	return stockNameMatch{}, false
}

func recognizeStockCellAnchors(ctx *maa.Context, img image.Image) ([]maa.Rect, error) {
	detail, err := ctx.RunRecognition(stockCellAnchorNodeName, img, nil)
	if err != nil {
		return nil, fmt.Errorf("recognize goods cell anchors: %w", err)
	}
	if detail == nil {
		return nil, fmt.Errorf("recognize goods cell anchors returned no detail")
	}
	results := templateMatchResults(detail)
	boxes := make([]maa.Rect, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		matched, ok := result.AsTemplateMatch()
		if !ok || matched == nil || rectEmpty(matched.Box) {
			continue
		}
		if containsNearbyRect(boxes, matched.Box) {
			continue
		}
		boxes = append(boxes, matched.Box)
	}
	sortRectsByGrid(boxes)
	return boxes, nil
}

func templateMatchResults(detail *maa.RecognitionDetail) []*maa.RecognitionResult {
	if detail == nil || detail.Algorithm != string(maa.RecognitionTypeTemplateMatch) || detail.Results == nil {
		return nil
	}
	return detail.Results.Filtered
}

func collectStockQuantityText(items []ocrmatch.Item, roi maa.Rect) string {
	matched := make([]ocrmatch.Item, 0, len(items))
	for _, item := range items {
		if rectContains(roi, item.Box) {
			matched = append(matched, item)
		}
	}
	// 库存位于单价格子的左侧，先找最左侧的数字结果。
	sort.SliceStable(matched, func(i, j int) bool {
		if matched[i].Box.X() != matched[j].Box.X() {
			return matched[i].Box.X() < matched[j].Box.X()
		}
		return matched[i].Box.Y() < matched[j].Box.Y()
	})
	for index, item := range matched {
		first := stockQuantityPattern.FindString(strings.ReplaceAll(strings.TrimSpace(item.Text), " ", ""))
		if first == "" {
			continue
		}
		parts := []string{first}
		previousBox := item.Box
		for _, next := range matched[index+1:] {
			gap := next.Box.X() - (previousBox.X() + previousBox.Width())
			if gap > stockQuantityFragmentMaxGapX {
				break
			}
			fragment := strings.ReplaceAll(strings.TrimSpace(next.Text), " ", "")
			if gap < 0 || !stockQuantityContinuation.MatchString(fragment) ||
				!stockQuantityFragmentsAligned(previousBox, next.Box) {
				continue
			}
			parts = append(parts, fragment)
			previousBox = next.Box
		}
		return strings.Join(parts, "")
	}
	return ""
}

func stockQuantityFragmentsAligned(left, right maa.Rect) bool {
	leftCenterY := left.Y() + left.Height()/2
	rightCenterY := right.Y() + right.Height()/2
	tolerance := max(left.Height(), right.Height()) / 2
	return absInt(leftCenterY-rightCenterY) <= tolerance
}

func parseStockQuantity(raw string) (int64, bool) {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), " ", "")
	match := stockQuantityPattern.FindStringSubmatch(normalized)
	if len(match) == 0 {
		return 0, false
	}
	numberText := match[1]
	suffix := strings.ToLower(match[2])
	if suffix == "" {
		numberText = strings.ReplaceAll(numberText, ",", "")
	} else {
		numberText = strings.ReplaceAll(numberText, ",", ".")
	}
	value, err := strconv.ParseFloat(numberText, 64)
	if err != nil || value < 0 {
		return 0, false
	}
	multiplier := float64(1)
	switch suffix {
	case "k":
		multiplier = 1_000
	case "万", "萬", "만":
		multiplier = 10_000
	case "m":
		multiplier = 1_000_000
	}
	return int64(math.Round(value * multiplier)), true
}

func parseStockCellOffsets(name, quantity, click []int) (stockCellOffsets, error) {
	nameRect, err := parseStockCellOffset("stock_name_offset", name)
	if err != nil {
		return stockCellOffsets{}, err
	}
	quantityRect, err := parseStockCellOffset("stock_quantity_offset", quantity)
	if err != nil {
		return stockCellOffsets{}, err
	}
	clickRect, err := parseStockCellOffset("stock_click_offset", click)
	if err != nil {
		return stockCellOffsets{}, err
	}
	return stockCellOffsets{Name: nameRect, Quantity: quantityRect, Click: clickRect}, nil
}

func parseStockCellOffset(field string, value []int) (maa.Rect, error) {
	if len(value) != 4 {
		return maa.Rect{}, fmt.Errorf("%s length is %d, expected 4", field, len(value))
	}
	if value[2] <= 0 || value[3] <= 0 {
		return maa.Rect{}, fmt.Errorf("%s width and height must be positive", field)
	}
	return maa.Rect{value[0], value[1], value[2], value[3]}, nil
}

func stockCellBox(anchor, offset maa.Rect, bounds image.Rectangle) maa.Rect {
	// 与 MaaFramework roi_offset 一致：x/y 平移基准框，width/height 在基准尺寸上增减。
	return clipMaaRect(maa.Rect{
		anchor.X() + offset.X(),
		anchor.Y() + offset.Y(),
		anchor.Width() + offset.Width(),
		anchor.Height() + offset.Height(),
	}, bounds)
}

func clipMaaRect(rect maa.Rect, bounds image.Rectangle) maa.Rect {
	clipped := image.Rect(
		rect.X(),
		rect.Y(),
		rect.X()+rect.Width(),
		rect.Y()+rect.Height(),
	).Intersect(bounds)
	return maa.Rect{clipped.Min.X, clipped.Min.Y, clipped.Dx(), clipped.Dy()}
}

func rectsOverlap(left, right maa.Rect) bool {
	return left.X() < right.X()+right.Width() &&
		right.X() < left.X()+left.Width() &&
		left.Y() < right.Y()+right.Height() &&
		right.Y() < left.Y()+left.Height()
}

func rectContains(outer, inner maa.Rect) bool {
	return inner.X() >= outer.X() &&
		inner.Y() >= outer.Y() &&
		inner.X()+inner.Width() <= outer.X()+outer.Width() &&
		inner.Y()+inner.Height() <= outer.Y()+outer.Height()
}

func sortStockNameMatchesByGrid(matches []stockNameMatch) {
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].box.Y() < matches[j].box.Y()
	})
	for start := 0; start < len(matches); {
		end := start + 1
		for end < len(matches) && matches[end].box.Y()-matches[start].box.Y() <= stockGridRowToleranceY {
			end++
		}
		sort.SliceStable(matches[start:end], func(i, j int) bool {
			return matches[start+i].box.X() < matches[start+j].box.X()
		})
		start = end
	}
}

func sortRectsByGrid(rects []maa.Rect) {
	sort.SliceStable(rects, func(i, j int) bool {
		return rects[i].Y() < rects[j].Y()
	})
	for start := 0; start < len(rects); {
		end := start + 1
		for end < len(rects) && rects[end].Y()-rects[start].Y() <= stockGridRowToleranceY {
			end++
		}
		sort.SliceStable(rects[start:end], func(i, j int) bool {
			return rects[start+i].X() < rects[start+j].X()
		})
		start = end
	}
}

func containsNearbyRect(rects []maa.Rect, candidate maa.Rect) bool {
	centerX := candidate.X() + candidate.Width()/2
	centerY := candidate.Y() + candidate.Height()/2
	for _, rect := range rects {
		if absInt(centerX-(rect.X()+rect.Width()/2)) <= stockAnchorDedupToleranceAxis &&
			absInt(centerY-(rect.Y()+rect.Height()/2)) <= stockAnchorDedupToleranceAxis {
			return true
		}
	}
	return false
}

func rectEmpty(rect maa.Rect) bool {
	return rect.Width() <= 0 || rect.Height() <= 0
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
