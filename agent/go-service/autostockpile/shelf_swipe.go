package autostockpile

import (
	"fmt"
	"image"
	"strings"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// shelfFrameCompareStep 是判断「货架有没有动」时的采样步长（像素）。
// 逐像素比较整屏开销过大，而滚动一屏必然改变大量采样点，隔点采样已足够。
const shelfFrameCompareStep = 4

const (
	// shelfRestoreTolerance 是复原后「市场」标题允许的纵向误差（像素）。
	// 点击识别 ROI 覆盖整个货架区，几十像素的偏差不影响点击，因此容差放宽以免微调空转。
	shelfRestoreTolerance = 40
	// shelfRestoreMaxSteps 是复原阶段最多执行的微调次数，防止识别异常时死循环。
	shelfRestoreMaxSteps = 8
	// shelfRestoreMaxDrag 是单次微调的最大拖动距离（像素）。
	shelfRestoreMaxDrag = 260
	// shelfRestoreMinDrag 是单次微调的最小拖动距离（像素）；更小的拖动会被触控阈值吞掉。
	shelfRestoreMinDrag = 40
)

// shelfSwipeDownNodes 是货架逐屏下滑的节点顺序：先小幅下滑露出次屏，再一直滑到底。
// 下标即「露出该屏需要下滑的次数」，与 mergeGoodsPages 产出的 SwipeCountToReveal 一致。
var shelfSwipeDownNodes = []string{swipeShelfDownNodeName, swipeShelfToBottomNodeName}

func runShelfSwipe(ctx *maa.Context, nodeName string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	_, err := ctx.RunAction(nodeName, maa.Rect{0, 0, 0, 0}, "", nil)
	return err
}

func swipeShelfUp(ctx *maa.Context) error {
	return runShelfSwipe(ctx, swipeShelfUpNodeName)
}

func screencapShelf(ctx *maa.Context) (image.Image, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker is nil")
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return nil, fmt.Errorf("controller is nil")
	}

	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		return nil, err
	}
	return img, nil
}

// shelfFramesLookIdentical 判断两帧的商品区域是否完全一致。
// 当前控制器不需要滑动（如 Win32 一屏即可看全）或货架已到尽头时，
// 下滑不会改变画面，此时再识别只会重复扫描同一屏，可以直接结束翻页。
func shelfFramesLookIdentical(previous, current image.Image) bool {
	if previous == nil || current == nil {
		return false
	}

	rect := image.Rect(
		goodsRecognitionBaseROI[0],
		goodsRecognitionBaseROI[1],
		goodsRecognitionBaseROI[0]+goodsRecognitionBaseROI[2],
		goodsRecognitionBaseROI[1]+goodsRecognitionBaseROI[3],
	).Intersect(previous.Bounds()).Intersect(current.Bounds())
	if rect.Empty() {
		return false
	}

	for y := rect.Min.Y; y < rect.Max.Y; y += shelfFrameCompareStep {
		for x := rect.Min.X; x < rect.Max.X; x += shelfFrameCompareStep {
			if !samePixel(previous, current, x, y) {
				return false
			}
		}
	}

	return true
}

// samePixel 比较两个画面上同一坐标的像素，避免直接比较 color.Color 接口值。
func samePixel(a, b image.Image, x, y int) bool {
	ar, ag, ab, aa := a.At(x, y).RGBA()
	br, bg, bb, ba := b.At(x, y).RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

// swipeShelfBy 用一次慢速拖动把货架内容纵向移动 dy 像素（正数向下、负数向上），并等待画面稳定。
// 慢速拖动惯性小，适合按像素微调；节点里的快速滑动会带惯性，实际滚动量远大于手势距离。
func swipeShelfBy(ctx *maa.Context, dy int) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return fmt.Errorf("tasker is nil")
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		return fmt.Errorf("controller is nil")
	}

	if dy > shelfRestoreMaxDrag {
		dy = shelfRestoreMaxDrag
	}
	if dy < -shelfRestoreMaxDrag {
		dy = -shelfRestoreMaxDrag
	}

	startY := int32(400)
	endY := startY + int32(dy)
	if endY < 140 {
		endY = 140
	}
	if endY > 660 {
		endY = 660
	}
	distance := absInt(int(endY - startY))
	duration := time.Duration(500+distance*2) * time.Millisecond

	job := ctrl.PostSwipe(640, startY, 640, endY, duration)
	job.Wait()
	if jobErr := job.Error(); jobErr != nil {
		return jobErr
	}

	roi := maa.Rect{
		goodsRecognitionBaseROI[0],
		goodsRecognitionBaseROI[1],
		goodsRecognitionBaseROI[2],
		goodsRecognitionBaseROI[3],
	}
	if freezeErr := ctx.WaitFreezes(300*time.Millisecond, &roi, nil); freezeErr != nil {
		log.Warn().
			Err(freezeErr).
			Str("component", autoStockpileComponent).
			Str("step", "shelf_wait_freezes").
			Int("drag", dy).
			Msg("shelf did not stabilize after fine correction")
	}
	return nil
}

// restoreShelf 把货架滑回本次扫描开始时的位置。
// 先按实际下滑次数用节点回滑，再以「市场」标题为锚点做慢速微调：
// 快速滑动带惯性，回滑次数无法精确抵消滑动距离，直接用微调收敛到起始位置。
// 该位置是后续点击商品的前提，因此失败只记录日志，不阻断本次识别结果。
func restoreShelf(ctx *maa.Context, times int, initialTitleY int, hasInitialTitle bool) {
	log.Info().
		Str("component", autoStockpileComponent).
		Str("step", "shelf_restore_start").
		Int("swiped", times).
		Int("initial_title_y", initialTitleY).
		Bool("has_initial_title", hasInitialTitle).
		Msg("restoring shelf")
	for i := 0; i < times; i++ {
		if err := swipeShelfUp(ctx); err != nil {
			log.Warn().
				Err(err).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_swipe_up").
				Int("restore_index", i).
				Int("restore_times", times).
				Msg("failed to restore shelf to start position")
			return
		}
	}

	if !hasInitialTitle {
		return
	}

	for step := 0; step < shelfRestoreMaxSteps; step++ {
		img, capErr := screencapShelf(ctx)
		if capErr != nil {
			log.Warn().
				Err(capErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_restore_screencap").
				Msg("failed to screencap during shelf restore")
			return
		}

		box, found, markErr := runFindMarketTitle(ctx, img)
		if markErr != nil {
			log.Warn().
				Err(markErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_restore_mark").
				Msg("failed to locate market title during shelf restore")
			return
		}
		if !found {
			// 「市场」标题不可见说明仍停在起始位置下方，继续上滑一屏。
			if err := swipeShelfUp(ctx); err != nil {
				log.Warn().
					Err(err).
					Str("component", autoStockpileComponent).
					Str("step", "shelf_swipe_up").
					Msg("failed to swipe up during shelf restore")
				return
			}
			continue
		}

		delta := box.Y() - initialTitleY
		log.Debug().
			Str("component", autoStockpileComponent).
			Str("step", "shelf_restore_step").
			Int("step", step).
			Int("title_y", box.Y()).
			Int("delta", delta).
			Msg("shelf restore measured")
		if absInt(delta) <= shelfRestoreTolerance {
			return
		}

		drag := -delta
		if drag > 0 && drag < shelfRestoreMinDrag {
			drag = shelfRestoreMinDrag
		}
		if drag < 0 && drag > -shelfRestoreMinDrag {
			drag = -shelfRestoreMinDrag
		}
		if err := swipeShelfBy(ctx, drag); err != nil {
			log.Warn().
				Err(err).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_restore_correction").
				Int("delta", delta).
				Msg("failed to correct shelf position")
			return
		}
	}

	log.Warn().
		Str("component", autoStockpileComponent).
		Str("step", "shelf_restore_unconverged").
		Int("initial_title_y", initialTitleY).
		Msg("shelf restore did not converge within step limit")
}

// scanGoodsAcrossShelfPages 从首屏开始逐屏下滑扫描，按商品 ID 合并所有屏结果，
// 返回合并后的商品与「露出该商品需要下滑的次数」，并尽量把货架滑回首屏。
func scanGoodsAcrossShelfPages(
	ctx *maa.Context,
	firstImg image.Image,
	region string,
	itemMap *ItemMap,
) (goods []GoodsItem, swipeCountToReveal map[string]int, abortReason AbortReason, err error) {
	if ctx == nil || firstImg == nil {
		return nil, nil, AbortReasonGoodsOCRUnavailableWarn, fmt.Errorf("ctx or image is nil")
	}

	// 以扫描起始画面上「市场」标题的纵坐标作为复原锚点：扫描结束后要回到同一位置，
	// 后续点击才与 SwipeCountToReveal 记录的下滑次数对应。
	initialTitleY, hasInitialTitle := resolveShelfMarketTitleY(ctx, firstImg)

	firstPage, abortReason, err := scanGoodsOnImage(ctx, firstImg, region, itemMap)
	if abortReason != AbortReasonNone || err != nil {
		return nil, nil, abortReason, err
	}

	pages := [][]GoodsItem{firstPage}
	swiped := 0
	restored := false

	// 下游要按首屏结果点击商品，因此无论后面是否降级，都要尽量把货架滑回起始位置。
	defer func() {
		if restored || swiped == 0 {
			return
		}
		restoreShelf(ctx, swiped, initialTitleY, hasInitialTitle)
	}()

	previousImg := firstImg
	for index, nodeName := range shelfSwipeDownNodes {
		pageIndex := index + 1

		if swipeErr := runShelfSwipe(ctx, nodeName); swipeErr != nil {
			log.Warn().
				Err(swipeErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_swipe_down").
				Str("node", nodeName).
				Int("page", pageIndex).
				Int("scanned_pages", len(pages)).
				Msg("shelf swipe down failed, keep scanned pages only")
			break
		}

		currentImg, capErr := screencapShelf(ctx)
		if capErr != nil {
			log.Warn().
				Err(capErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_screencap").
				Int("page", pageIndex).
				Int("scanned_pages", len(pages)).
				Msg("shelf screencap failed, keep scanned pages only")
			break
		}

		if shelfFramesLookIdentical(previousImg, currentImg) {
			log.Info().
				Str("component", autoStockpileComponent).
				Str("step", "shelf_swipe_skip").
				Str("node", nodeName).
				Int("page", pageIndex).
				Msg("shelf did not move after swipe, stop scanning further pages")
			break
		}
		swiped++

		page, pageAbort, pageErr := scanGoodsOnImage(ctx, currentImg, region, itemMap)
		if pageAbort != AbortReasonNone || pageErr != nil {
			log.Warn().
				Err(pageErr).
				Str("component", autoStockpileComponent).
				Str("step", "scan_page").
				Str("abort_reason", string(pageAbort)).
				Int("page", pageIndex).
				Int("scanned_pages", len(pages)).
				Msg("page scan failed, keep scanned pages only")
			break
		}

		pages = append(pages, page)
		previousImg = currentImg
	}

	goods, swipeCountToReveal = mergeGoodsPages(pages)

	log.Info().
		Str("component", autoStockpileComponent).
		Str("step", "merge_pages").
		Int("page_count", len(pages)).
		Ints("page_goods_counts", pageGoodsCounts(pages)).
		Int("merged_count", len(goods)).
		Int("swipe_count_to_reveal", len(swipeCountToReveal)).
		Msg("shelf goods scan merged")

	restoreShelf(ctx, swiped, initialTitleY, hasInitialTitle)
	restored = true

	return goods, swipeCountToReveal, AbortReasonNone, nil
}

// marketTitleTexts 是各语言下市场标题的候选文本，与 AutoStockpileFindMarketTitle 的 expected 保持一致。
var marketTitleTexts = []string{"市场", "市場", "Market", "マーケット", "시장", "마켓"}

// isMarketTitle 判断 OCR 文本是否为市场标题。
func isMarketTitle(text string) bool {
	for _, title := range marketTitleTexts {
		if strings.Contains(text, title) {
			return true
		}
	}
	return false
}

// runFindMarketTitle 在当前画面上定位「市场」标题，返回其文本框。
// 模板图 MarketMark.png 在 ADB 侧长期匹配不到，而标题文字稳定存在，因此用 OCR 作为滚动位置锚点。
func runFindMarketTitle(ctx *maa.Context, img image.Image) (maa.Rect, bool, error) {
	detail, err := ctx.RunRecognition(findMarketTitleNodeName, img, nil)
	if err != nil {
		return maa.Rect{}, false, err
	}
	if detail == nil || !detail.Hit || detail.Results == nil {
		return maa.Rect{}, false, nil
	}

	boxOf := func(result *maa.RecognitionResult) (maa.Rect, bool) {
		if result == nil {
			return maa.Rect{}, false
		}
		ocr, ok := result.AsOCR()
		if !ok || ocr == nil || !isMarketTitle(ocr.Text) {
			return maa.Rect{}, false
		}
		return ocr.Box, true
	}

	if box, ok := boxOf(detail.Results.Best); ok {
		return box, true, nil
	}
	for _, result := range detail.Results.Filtered {
		if box, ok := boxOf(result); ok {
			return box, true, nil
		}
	}
	for _, result := range detail.Results.All {
		if box, ok := boxOf(result); ok {
			return box, true, nil
		}
	}
	return maa.Rect{}, false, nil
}

// resolveShelfMarketTitleY 返回当前画面上「市场」标题的纵坐标；找不到时返回 false。
func resolveShelfMarketTitleY(ctx *maa.Context, img image.Image) (int, bool) {
	box, found, err := runFindMarketTitle(ctx, img)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "shelf_title_anchor").
			Msg("failed to resolve market title anchor")
		return 0, false
	}
	if !found {
		return 0, false
	}
	return box.Y(), true
}

func pageGoodsCounts(pages [][]GoodsItem) []int {
	counts := make([]int, 0, len(pages))
	for _, page := range pages {
		counts = append(counts, len(page))
	}
	return counts
}
