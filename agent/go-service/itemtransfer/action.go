package itemtransfer

import (
	"encoding/json"
	"image"
	"math/bits"
	"sort"
	"strconv"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ItemTransferFallbackAction is a custom action that searches for a target item
// on the current visible page using hover + OCR + binary search when NND fails.
type ItemTransferFallbackAction struct{}

var _ maa.CustomActionRunner = &ItemTransferFallbackAction{}

func (a *ItemTransferFallbackAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params fallbackParams
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

	side := inferSide(params.Side, arg.CurrentTaskName)

	nndNode := repoNNDNode
	if side == "bag" {
		nndNode = bagNNDNode
	}

	log.Info().
		Str("component", componentName).
		Str("target_name", itemInfo.Name).
		Int("target_class", params.TargetClass).
		Int("target_idx", targetIdx).
		Str("category", itemInfo.Category).
		Str("side", side).
		Bool("descending", params.Descending).
		Msg("starting fallback search on current page")

	tasker := ctx.GetTasker()
	ctrl := tasker.GetController()

	if tasker.Stopping() {
		return false
	}

	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to cache image")
		return false
	}

	rawItems := detectAllItems(ctx, img, nndNode)
	if len(rawItems) == 0 {
		log.Warn().Str("component", componentName).Msg("no items detected on current page")
		return false
	}

	cols := repoCols
	if side == "bag" {
		cols = bagCols
	}
	items := buildFullGrid(rawItems, cols, side)

	// Case 2.1: target class detected with low score → snap to grid and verify
	if found := findByLowScoreTarget(rawItems, params.TargetClass); found != nil {
		gx, gy := snapToGrid(found.CenterX, found.CenterY, items)
		log.Info().
			Str("component", componentName).
			Float64("score", found.Score).
			Int("grid_x", gx).Int("grid_y", gy).
			Msg("target class found with low score, verifying via OCR")

		results := hoverAndOCR(ctx, tasker, ctrl, gx, gy)
		if matchesAnyTarget(ocrTexts(results), itemInfo.Name) {
			log.Info().Str("component", componentName).Strs("ocr_names", ocrTexts(results)).Msg("OCR verified target")
			return ctrlClick(ctrl, gx, gy)
		}
		log.Info().
			Str("component", componentName).
			Strs("ocr_names", ocrTexts(results)).
			Str("expected", itemInfo.Name).
			Msg("OCR name mismatch, proceeding to binary search")
	}

	// Case 2.2 + Step 3: binary search among visible grid cells
	if targetIdx >= 0 && len(categoryOrder) > 0 {
		result := binarySearchOnPage(ctx, tasker, ctrl, items, categoryOrder, targetIdx, itemInfo.Name)
		if result != nil {
			return ctrlClick(ctrl, result.CenterX, result.CenterY)
		}
		log.Info().Str("component", componentName).Msg("binary search exhausted all grid cells, item not found")
		moveMouseSafe(ctrl)
		return false
	}

	// No category_order data: linear scan
	result := linearScanOnPage(ctx, tasker, ctrl, items, itemInfo.Name)
	if result != nil {
		return ctrlClick(ctrl, result.CenterX, result.CenterY)
	}

	log.Info().Str("component", componentName).Msg("linear scan found nothing, item not found")
	moveMouseSafe(ctrl)
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
		if len(items) == 0 {
			log.Debug().
				Str("component", componentName).
				Int("box_w", box.Width()).
				Int("box_h", box.Height()).
				Msg("NND detection box size (first item)")
		}
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
	if len(items) <= 1 {
		return
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CenterY < items[j].CenterY
	})
	const rowGap = 20
	rowStarts := []int{0}
	for i := 1; i < len(items); i++ {
		if items[i].CenterY-items[i-1].CenterY > rowGap {
			rowStarts = append(rowStarts, i)
		}
	}
	for r := 0; r < len(rowStarts); r++ {
		start := rowStarts[r]
		end := len(items)
		if r+1 < len(rowStarts) {
			end = rowStarts[r+1]
		}
		row := items[start:end]
		sort.Slice(row, func(i, j int) bool {
			return row[i].CenterX < row[j].CenterX
		})
	}
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

// runTooltipOCR runs OCR in the tooltip region computed from hoverX/hoverY.
// It returns all recognized texts with bounding boxes and scores.
func runTooltipOCR(ctx *maa.Context, img image.Image, hoverX, hoverY int) []tooltipOCRResult {
	ocrROI := computeTooltipROI(hoverX, hoverY)
	override := map[string]any{
		tooltipOCRNode: map[string]any{
			"roi": ocrROI,
		},
	}

	detail, err := ctx.RunRecognition(tooltipOCRNode, img, override)
	if err != nil || detail == nil || !detail.Hit {
		log.Warn().
			Str("component", componentName).
			Int("hover_x", hoverX).
			Int("hover_y", hoverY).
			Ints("ocr_roi", ocrROI).
			Msg("tooltip OCR failed")
		return nil
	}

	results := extractAllOCRResults(detail)
	log.Info().
		Str("component", componentName).
		Strs("ocr_texts", ocrTexts(results)).
		Int("hover_x", hoverX).
		Int("hover_y", hoverY).
		Ints("ocr_roi", ocrROI).
		Msg("tooltip OCR result")

	return results
}

// hoverAndOCR moves the cursor to (x, y), waits for the tooltip to appear,
// takes a screenshot, then delegates to runTooltipOCR.
func hoverAndOCR(ctx *maa.Context, tasker *maa.Tasker, ctrl *maa.Controller, x, y int) []tooltipOCRResult {
	if tasker.Stopping() {
		return nil
	}

	ctrl.PostTouchMove(0, int32(x), int32(y), 0).Wait()
	time.Sleep(1500 * time.Millisecond)

	if tasker.Stopping() {
		return nil
	}

	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("failed to cache image after hover")
		return nil
	}

	results := runTooltipOCR(ctx, img, x, y)

	var filtered []tooltipOCRResult
	for _, r := range results {
		if !strings.Contains(r.Text, "已盛装") {
			filtered = append(filtered, r)
		}
	}
	return filtered
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

// extractAllOCRResults collects all OCR hits from a recognition detail,
// preserving text, box, and score. Prefers Filtered over All.
func extractAllOCRResults(detail *maa.RecognitionDetail) []tooltipOCRResult {
	if detail == nil || detail.Results == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []tooltipOCRResult
	for _, results := range [][]*maa.RecognitionResult{
		detail.Results.Filtered,
		detail.Results.All,
	} {
		for _, r := range results {
			if r == nil {
				continue
			}
			if ocr, ok := r.AsOCR(); ok {
				text := strings.TrimSpace(ocr.Text)
				if text != "" && !seen[text] {
					seen[text] = true
					out = append(out, tooltipOCRResult{
						Text:  text,
						Box:   ocr.Box,
						Score: ocr.Score,
					})
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return out
}

// ocrTexts extracts a plain text slice from structured OCR results.
func ocrTexts(results []tooltipOCRResult) []string {
	if len(results) == 0 {
		return nil
	}
	texts := make([]string, len(results))
	for i, r := range results {
		texts[i] = r.Text
	}
	return texts
}

func matchesTarget(ocrName, targetName string) bool {
	if ocrName == "" {
		return false
	}
	ocrName = strings.TrimSpace(ocrName)
	if ocrName == targetName {
		return true
	}
	cleaned := cleanOCRNoise(ocrName)
	return cleaned != "" && cleaned == targetName
}

func matchesAnyTarget(names []string, targetName string) bool {
	for _, name := range names {
		if matchesTarget(name, targetName) {
			return true
		}
	}
	return false
}

// resolveOCRIndex finds the first OCR text that exists in categoryOrder
// and returns its name and index. Falls back to fuzzy matching.
func resolveOCRIndex(names []string, categoryOrder []string) (string, int) {
	for _, n := range names {
		if i := indexOf(categoryOrder, n); i >= 0 {
			return n, i
		}
	}
	for _, n := range names {
		if i := fuzzyIndexOf(categoryOrder, n); i >= 0 {
			return n, i
		}
	}
	if len(names) > 0 {
		return names[0], -1
	}
	return "", -1
}

func cleanOCRNoise(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == ' ' || r == '·' || r == '.' || r == ',' || r == '、' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// maxLinearSteps is the hard cap on how many grid cells the linear approach
// may visit. Even when the binary search interval is large enough to permit
// more steps, we never exceed this limit.
const maxLinearSteps = 3

// binarySearchSteps returns the worst-case number of OCR calls binary search
// would need for a remaining interval of n elements: ⌈log₂(n+1)⌉.
func binarySearchSteps(n int) int {
	if n <= 0 {
		return 0
	}
	return bits.Len(uint(n))
}

// adaptiveThreshold returns the effective close-index threshold given the
// current binary search interval size. Linear approach is only started when
// its max steps (= returned threshold) ≤ the remaining binary search steps,
// guaranteeing no extra OCR calls compared to continuing binary search.
func adaptiveThreshold(remainingN int) int {
	bs := binarySearchSteps(remainingN)
	if bs > maxLinearSteps {
		return maxLinearSteps
	}
	return bs
}

func shouldSwitchToLinear(ocrIdx, targetIdx, threshold int) bool {
	if ocrIdx < 0 || threshold <= 0 {
		return false
	}
	return abs(ocrIdx-targetIdx) <= threshold && ocrIdx != targetIdx
}

// linearApproachFromGrid walks from startGridIdx along the grid toward targetIdx:
// grid index +1 per step when knownOcrIdx < targetIdx, -1 when knownOcrIdx > targetIdx.
// maxSteps limits the number of cells to visit (set by adaptiveThreshold).
// If OCR resolves past the target in the approach direction, returns nil (overshoot).
func linearApproachFromGrid(ctx *maa.Context, tasker *maa.Tasker, ctrl *maa.Controller, items []gridItem, categoryOrder []string, targetIdx int, targetName string, startGridIdx, knownOcrIdx, maxSteps int) *gridItem {
	if maxSteps <= 0 || knownOcrIdx < 0 || knownOcrIdx == targetIdx {
		return nil
	}

	dir := 1
	if knownOcrIdx > targetIdx {
		dir = -1
	}

	for step := 1; step <= maxSteps; step++ {
		g := startGridIdx + dir*step
		if g < 0 || g >= len(items) {
			log.Info().
				Str("component", componentName).
				Int("start_grid", startGridIdx).
				Int("dir", dir).
				Int("step", step).
				Msg("linear approach reached grid boundary without target")
			return nil
		}
		if tasker.Stopping() {
			return nil
		}

		results := hoverAndOCR(ctx, tasker, ctrl, items[g].CenterX, items[g].CenterY)
		names := ocrTexts(results)
		if matchesAnyTarget(names, targetName) {
			log.Info().
				Str("component", componentName).
				Strs("ocr_names", names).
				Int("grid_idx", g).
				Msg("linear approach found target")
			return &items[g]
		}
		if len(results) == 0 {
			continue
		}
		_, ocrIdx := resolveOCRIndex(names, categoryOrder)
		if ocrIdx >= 0 && dir > 0 && ocrIdx > targetIdx {
			log.Info().
				Str("component", componentName).
				Int("ocr_idx", ocrIdx).
				Int("target_idx", targetIdx).
				Int("grid_idx", g).
				Msg("linear approach overshot past target (forward)")
			return nil
		}
		if ocrIdx >= 0 && dir < 0 && ocrIdx < targetIdx {
			log.Info().
				Str("component", componentName).
				Int("ocr_idx", ocrIdx).
				Int("target_idx", targetIdx).
				Int("grid_idx", g).
				Msg("linear approach overshot past target (backward)")
			return nil
		}
	}

	log.Info().
		Str("component", componentName).
		Int("start_grid", startGridIdx).
		Int("target_idx", targetIdx).
		Msg("linear approach exhausted without finding target")
	return nil
}

// binarySearchOnPage searches among visible grid cells.
// Always starts from cell 0 (top-left) to establish a baseline, then
// converges forward via binary search on the remaining range.
// When OCR resolves to a category index close to the target and the
// adaptive threshold (min(maxLinearSteps, ⌈log₂(remaining)⌉)) is met,
// binary search stops and cells are visited linearly toward the target;
// this guarantees the linear phase never costs more OCR calls than
// continuing binary search would.
// Returns the target item if found, or nil when the range is exhausted.
func binarySearchOnPage(ctx *maa.Context, tasker *maa.Tasker, ctrl *maa.Controller, items []gridItem, categoryOrder []string, targetIdx int, targetName string) *gridItem {
	if len(items) == 0 {
		return nil
	}

	if tasker.Stopping() {
		return nil
	}

	const maxConsecutiveCategoryMisses = 3
	consecutiveMisses := 0

	first := &items[0]
	results := hoverAndOCR(ctx, tasker, ctrl, first.CenterX, first.CenterY)
	names := ocrTexts(results)

	if matchesAnyTarget(names, targetName) {
		log.Info().Str("component", componentName).Strs("ocr_names", names).Int("grid_idx", 0).Msg("found target at first cell")
		return first
	}

	if len(results) > 0 {
		name, ocrIdx := resolveOCRIndex(names, categoryOrder)
		firstThreshold := adaptiveThreshold(len(items) - 1)
		if ocrIdx >= 0 && ocrIdx > targetIdx && abs(ocrIdx-targetIdx) > firstThreshold {
			log.Info().Str("component", componentName).
				Str("ocr_name", name).Int("ocr_idx", ocrIdx).Int("target_idx", targetIdx).
				Msg("first cell already past target, item not on this page")
			return nil
		}
		if shouldSwitchToLinear(ocrIdx, targetIdx, firstThreshold) {
			log.Info().Str("component", componentName).
				Str("ocr_name", name).Int("ocr_idx", ocrIdx).Int("target_idx", targetIdx).
				Int("threshold", firstThreshold).
				Msg("first cell within close index range, switching to linear approach")
			return linearApproachFromGrid(ctx, tasker, ctrl, items, categoryOrder, targetIdx, targetName, 0, ocrIdx, firstThreshold)
		}
		if ocrIdx < 0 {
			consecutiveMisses++
		}
	}

	lo, hi := 1, len(items)-1

	for lo <= hi {
		if tasker.Stopping() {
			return nil
		}

		mid := (lo + hi) / 2
		item := &items[mid]

		results = hoverAndOCR(ctx, tasker, ctrl, item.CenterX, item.CenterY)
		names = ocrTexts(results)
		if len(results) == 0 {
			lo = mid + 1
			continue
		}

		if matchesAnyTarget(names, targetName) {
			log.Info().Str("component", componentName).
				Strs("ocr_names", names).Int("grid_idx", mid).
				Msg("binary search found target")
			return item
		}

		name, ocrIdx := resolveOCRIndex(names, categoryOrder)
		if ocrIdx < 0 {
			consecutiveMisses++
			if consecutiveMisses >= maxConsecutiveCategoryMisses {
				log.Warn().Str("component", componentName).
					Strs("ocr_names", names).Int("consecutive_misses", consecutiveMisses).
					Msg("too many consecutive category misses, likely wrong category page")
				return nil
			}
			lo = mid + 1
			log.Warn().Str("component", componentName).
				Strs("ocr_names", names).Int("mid", mid).
				Msg("OCR'd item not in category order, advancing forward")
			continue
		}

		consecutiveMisses = 0

		log.Info().Str("component", componentName).
			Str("ocr_name", name).Int("ocr_idx", ocrIdx).Int("target_idx", targetIdx).
			Int("lo", lo).Int("hi", hi).Int("mid", mid).
			Msg("binary search narrowing grid range")

		var remainingN int
		if ocrIdx < targetIdx {
			remainingN = hi - mid
		} else {
			remainingN = mid - lo
		}
		midThreshold := adaptiveThreshold(remainingN)
		if shouldSwitchToLinear(ocrIdx, targetIdx, midThreshold) {
			log.Info().Str("component", componentName).
				Str("ocr_name", name).Int("ocr_idx", ocrIdx).Int("target_idx", targetIdx).
				Int("mid", mid).Int("remaining_n", remainingN).Int("threshold", midThreshold).
				Msg("within close index range, switching to linear approach")
			return linearApproachFromGrid(ctx, tasker, ctrl, items, categoryOrder, targetIdx, targetName, mid, ocrIdx, midThreshold)
		}

		if ocrIdx < targetIdx {
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}

	log.Info().Str("component", componentName).
		Int("target_idx", targetIdx).
		Msg("binary search exhausted, target not found")
	return nil
}

func linearScanOnPage(ctx *maa.Context, tasker *maa.Tasker, ctrl *maa.Controller, items []gridItem, targetName string) *gridItem {
	for i := range items {
		if tasker.Stopping() {
			return nil
		}
		results := hoverAndOCR(ctx, tasker, ctrl, items[i].CenterX, items[i].CenterY)
		if matchesAnyTarget(ocrTexts(results), targetName) {
			return &items[i]
		}
	}
	return nil
}

func ctrlClick(ctrl *maa.Controller, x, y int) bool {
	ctrl.PostKeyDown(17).Wait()
	time.Sleep(50 * time.Millisecond)

	ctrl.PostClick(int32(x), int32(y)).Wait()

	time.Sleep(50 * time.Millisecond)
	ctrl.PostKeyUp(17).Wait()

	log.Info().
		Str("component", componentName).
		Int("x", x).
		Int("y", y).
		Msg("Ctrl+Click performed")

	moveMouseSafe(ctrl)
	return true
}

func snapToGrid(x, y int, grid []gridItem) (int, int) {
	bestX, bestY := x, y
	bestDist := 1<<31 - 1
	for _, g := range grid {
		dx := g.CenterX - x
		dy := g.CenterY - y
		d := dx*dx + dy*dy
		if d < bestDist {
			bestDist = d
			bestX, bestY = g.CenterX, g.CenterY
		}
	}
	return bestX, bestY
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
