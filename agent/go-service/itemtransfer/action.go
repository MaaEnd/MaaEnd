package itemtransfer

import (
	"encoding/json"
	"image"
	"sort"
	"strconv"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type ItemTransferFallbackAction struct{}

func (a *ItemTransferFallbackAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params FallbackParams
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to parse custom action param")
		return false
	}

	data, err := loadItemOrderData()
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to load item order data")
		return false
	}

	classKey := strconv.Itoa(params.TargetClass)
	itemInfo, ok := data.Items[classKey]
	if !ok {
		log.Error().
			Str("component", componentName).
			Int("target_class", params.TargetClass).
			Msg("target class not found in item_order.json")
		return false
	}

	categoryOrder, hasCategoryOrder := data.CategoryOrder[itemInfo.Category]
	if !hasCategoryOrder || len(categoryOrder) == 0 {
		log.Warn().
			Str("component", componentName).
			Str("category", itemInfo.Category).
			Msg("category_order empty or not found, falling back to linear scan")
	}

	if params.Descending {
		categoryOrder = reversed(categoryOrder)
	}

	targetIdx := indexOf(categoryOrder, itemInfo.Name)

	side := params.Side
	if side == "" {
		side = "repo"
	}

	nndNode := repoNNDNode
	scrollX, scrollY := repoScrollTargetX, repoScrollTargetY
	if side == "bag" {
		nndNode = bagNNDNode
		scrollX, scrollY = bagScrollTargetX, bagScrollTargetY
	}

	log.Info().
		Str("component", componentName).
		Str("target_name", itemInfo.Name).
		Int("target_class", params.TargetClass).
		Int("target_idx", targetIdx).
		Str("category", itemInfo.Category).
		Str("side", side).
		Bool("descending", params.Descending).
		Msg("starting fallback search")

	ctrl := ctx.GetTasker().GetController()

	for scroll := 0; scroll < maxScrollAttempts; scroll++ {
		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil {
			log.Error().Err(err).Str("component", componentName).Msg("failed to cache image")
			return false
		}

		items := detectAllItems(ctx, img, nndNode)
		if len(items) == 0 {
			log.Warn().Str("component", componentName).Int("scroll", scroll).Msg("no items detected, scrolling down")
			doScroll(ctx, scrollX, scrollY, scrollDY)
			continue
		}

		sortByGridPosition(items)

		if found := findByLowScoreTarget(items, params.TargetClass); found != nil {
			log.Info().
				Str("component", componentName).
				Float64("score", found.Score).
				Msg("target class found with low score, verifying via OCR")

			name := hoverAndOCR(ctx, ctrl, found.CenterX, found.CenterY)
			if matchesTarget(name, itemInfo.Name) {
				log.Info().Str("component", componentName).Str("ocr_name", name).Msg("OCR verified target, performing Ctrl+Click")
				return ctrlClick(ctrl, found.CenterX, found.CenterY)
			}
			log.Info().
				Str("component", componentName).
				Str("ocr_name", name).
				Str("expected", itemInfo.Name).
				Msg("OCR name mismatch, proceeding to binary search")
		}

		if targetIdx >= 0 && len(categoryOrder) > 0 {
			result := binarySearchOnPage(ctx, ctrl, items, categoryOrder, targetIdx, itemInfo.Name)
			if result != nil {
				return ctrlClick(ctrl, result.CenterX, result.CenterY)
			}

			direction := determineScrollDirection(ctx, ctrl, items, categoryOrder, targetIdx)
			if direction == 0 {
				log.Info().Str("component", componentName).Msg("cannot determine scroll direction, item likely not present")
				return false
			}
			dy := scrollDY
			if direction < 0 {
				dy = -scrollDY
			}
			doScroll(ctx, scrollX, scrollY, dy)
		} else {
			result := linearScanOnPage(ctx, ctrl, items, itemInfo.Name)
			if result != nil {
				return ctrlClick(ctrl, result.CenterX, result.CenterY)
			}
			doScroll(ctx, scrollX, scrollY, scrollDY)
		}
	}

	log.Warn().Str("component", componentName).Msg("fallback search exhausted all scroll attempts")
	return false
}

func detectAllItems(ctx *maa.Context, img image.Image, nndNode string) []gridItem {
	detail, err := ctx.RunRecognition(nndNode, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		return nil
	}

	results := recognitionResults(detail)
	items := make([]gridItem, 0, len(results))
	for _, r := range results {
		nnd, ok := r.AsNeuralNetworkDetect()
		if !ok {
			continue
		}
		box := nnd.Box
		items = append(items, gridItem{
			Box:     [4]int{box.X(), box.Y(), box.Width(), box.Height()},
			ClassID: nnd.ClsIndex,
			Score:   nnd.Score,
			CenterX: box.X() + box.Width()/2,
			CenterY: box.Y() + box.Height()/2,
		})
	}
	return items
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

func sortByGridPosition(items []gridItem) {
	sort.Slice(items, func(i, j int) bool {
		dy := items[i].CenterY - items[j].CenterY
		if dy < -20 {
			return true
		}
		if dy > 20 {
			return false
		}
		return items[i].CenterX < items[j].CenterX
	})
}

func findByLowScoreTarget(items []gridItem, targetClass int) *gridItem {
	var best *gridItem
	for i := range items {
		if int(items[i].ClassID) == targetClass {
			if best == nil || items[i].Score > best.Score {
				best = &items[i]
			}
		}
	}
	return best
}

func hoverAndOCR(ctx *maa.Context, ctrl *maa.Controller, x, y int) string {
	ctrl.PostTouchMove(0, int32(x), int32(y), 0).Wait()
	time.Sleep(1 * time.Second)

	ctrl.PostScreencap().Wait()
	newImg, err := ctrl.CacheImage()
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to cache image after hover")
		return ""
	}

	ocrROI := computeTooltipROI(x, y)
	override := map[string]any{
		tooltipOCRNode: map[string]any{
			"roi": ocrROI,
		},
	}

	detail, err := ctx.RunRecognition(tooltipOCRNode, newImg, override)
	if err != nil || detail == nil || !detail.Hit {
		log.Warn().
			Str("component", componentName).
			Int("hover_x", x).
			Int("hover_y", y).
			Ints("ocr_roi", ocrROI).
			Msg("tooltip OCR failed")
		return ""
	}

	text := extractOCRText(detail)
	log.Info().
		Str("component", componentName).
		Str("ocr_text", text).
		Int("hover_x", x).
		Int("hover_y", y).
		Msg("tooltip OCR result")

	moveMouseSafe(ctrl)
	return strings.TrimSpace(text)
}

func computeTooltipROI(hoverX, hoverY int) []int {
	roiX := hoverX + tooltipOffsetX
	roiY := hoverY + tooltipOffsetY
	if roiX+tooltipWidth > 1280 {
		roiX = hoverX - tooltipOffsetX - tooltipWidth
	}
	if roiY+tooltipHeight > 720 {
		roiY = 720 - tooltipHeight
	}
	if roiX < 0 {
		roiX = 0
	}
	if roiY < 0 {
		roiY = 0
	}
	return []int{roiX, roiY, tooltipWidth, tooltipHeight}
}

func extractOCRText(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.Results == nil {
		return ""
	}
	for _, results := range [][]*maa.RecognitionResult{
		{detail.Results.Best},
		detail.Results.Filtered,
		detail.Results.All,
	} {
		for _, r := range results {
			if r == nil {
				continue
			}
			if ocrResult, ok := r.AsOCR(); ok && ocrResult.Text != "" {
				return ocrResult.Text
			}
		}
	}
	return ""
}

func matchesTarget(ocrName, targetName string) bool {
	if ocrName == "" {
		return false
	}
	ocrName = strings.TrimSpace(ocrName)
	return ocrName == targetName || strings.Contains(ocrName, targetName) || strings.Contains(targetName, ocrName)
}

func binarySearchOnPage(ctx *maa.Context, ctrl *maa.Controller, items []gridItem, categoryOrder []string, targetIdx int, targetName string) *gridItem {
	lo, hi := 0, len(items)-1
	attempts := 0

	for lo <= hi && attempts < maxBinaryRetries {
		mid := (lo + hi) / 2
		item := &items[mid]
		attempts++

		name := hoverAndOCR(ctx, ctrl, item.CenterX, item.CenterY)
		if name == "" {
			lo = mid + 1
			continue
		}

		if matchesTarget(name, targetName) {
			log.Info().
				Str("component", componentName).
				Str("ocr_name", name).
				Int("mid", mid).
				Msg("binary search found target")
			return item
		}

		ocrIdx := indexOf(categoryOrder, name)
		if ocrIdx < 0 {
			ocrIdx = fuzzyIndexOf(categoryOrder, name)
		}
		if ocrIdx < 0 {
			log.Warn().
				Str("component", componentName).
				Str("ocr_name", name).
				Msg("OCR'd item not found in category order, skipping")
			lo = mid + 1
			continue
		}

		log.Info().
			Str("component", componentName).
			Str("ocr_name", name).
			Int("ocr_idx", ocrIdx).
			Int("target_idx", targetIdx).
			Int("lo", lo).
			Int("hi", hi).
			Int("mid", mid).
			Msg("binary search narrowing")

		if ocrIdx < targetIdx {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	return nil
}

func determineScrollDirection(ctx *maa.Context, ctrl *maa.Controller, items []gridItem, categoryOrder []string, targetIdx int) int {
	if len(items) == 0 {
		return 1
	}

	first := &items[0]
	last := &items[len(items)-1]

	firstName := hoverAndOCR(ctx, ctrl, first.CenterX, first.CenterY)
	firstIdx := indexOf(categoryOrder, firstName)
	if firstIdx < 0 {
		firstIdx = fuzzyIndexOf(categoryOrder, firstName)
	}

	lastName := hoverAndOCR(ctx, ctrl, last.CenterX, last.CenterY)
	lastIdx := indexOf(categoryOrder, lastName)
	if lastIdx < 0 {
		lastIdx = fuzzyIndexOf(categoryOrder, lastName)
	}

	if firstIdx >= 0 && targetIdx < firstIdx {
		return -1 // scroll up
	}
	if lastIdx >= 0 && targetIdx > lastIdx {
		return 1 // scroll down
	}

	return 0
}

func linearScanOnPage(ctx *maa.Context, ctrl *maa.Controller, items []gridItem, targetName string) *gridItem {
	for i := range items {
		name := hoverAndOCR(ctx, ctrl, items[i].CenterX, items[i].CenterY)
		if matchesTarget(name, targetName) {
			return &items[i]
		}
	}
	return nil
}

func ctrlClick(ctrl *maa.Controller, x, y int) bool {
	ctrl.PostKeyDown(17).Wait()
	time.Sleep(50 * time.Millisecond)

	ctrl.PostTouchDown(0, int32(x), int32(y), 1).Wait()
	time.Sleep(100 * time.Millisecond)
	ctrl.PostTouchUp(0).Wait()

	time.Sleep(50 * time.Millisecond)
	ctrl.PostKeyUp(17).Wait()

	log.Info().
		Str("component", componentName).
		Int("x", x).
		Int("y", y).
		Msg("Ctrl+Click performed")
	return true
}

func doScroll(ctx *maa.Context, targetX, targetY, dy int) {
	override := map[string]any{
		"__ItemTransferFallbackScroll": map[string]any{
			"target": []int{targetX, targetY},
			"dy":     dy,
		},
	}
	ctx.RunAction(
		"__ItemTransferFallbackScroll",
		maa.Rect{0, 0, 0, 0}, "", override,
	)
	time.Sleep(500 * time.Millisecond)
}

func moveMouseSafe(ctrl *maa.Controller) {
	ctrl.PostTouchMove(0, 10, 10, 0).Wait()
	time.Sleep(50 * time.Millisecond)
}

func indexOf(order []string, name string) int {
	for i, n := range order {
		if n == name {
			return i
		}
	}
	return -1
}

func fuzzyIndexOf(order []string, name string) int {
	name = strings.TrimSpace(name)
	if name == "" {
		return -1
	}
	bestIdx := -1
	bestDist := len(name) + 1
	for i, n := range order {
		if strings.Contains(n, name) || strings.Contains(name, n) {
			d := abs(len(n) - len(name))
			if d < bestDist {
				bestDist = d
				bestIdx = i
			}
		}
	}
	return bestIdx
}

func reversed(s []string) []string {
	if len(s) == 0 {
		return s
	}
	out := make([]string, len(s))
	copy(out, s)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
