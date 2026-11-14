package autostockpile

import (
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// shelfFrameCompareStep 是判断「货架有没有动」时的采样步长（像素）。
// 逐像素比较整屏开销过大，而滚动一屏必然改变大量采样点，隔点采样已足够。
const shelfFrameCompareStep = 4

// shelfScanSwipeTimes 是逐屏扫描固定下滑的次数：进入时 1 帧 + 每次滑动各 1 帧。
// 与识别到的商品数量无关，多出来的帧由商品 ID 去重消化；
// 画面没有变化时提前结束，避免在不需要滑动的控制器上重复识别同一屏。
const shelfScanSwipeTimes = 2

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

// shelfResetMaxSwipes 是重置货架时最多向上滑动的次数。
// 循环在「市场入口重新可见」时提前结束，该上限只用于兜底，
// 避免入口一直不可见时无限上滑；实机若滑出列表顶部，优先减小该值。
const shelfResetMaxSwipes = 4

// resetShelf 把货架重置回顶部：先向上滑动到市场入口重新可见，再点击入口让列表重新渲染。
// 位置由 UI 重置保证，不依赖下滑次数与上滑次数的精确抵消。
// 重置失败只记录日志，不阻断本次识别结果（后续点击可能因此偏位）。
func resetShelf(ctx *maa.Context) {
	for i := 0; i < shelfResetMaxSwipes; i++ {
		img, capErr := screencapShelf(ctx)
		if capErr != nil {
			log.Warn().
				Err(capErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_reset_screencap").
				Msg("failed to screencap during shelf reset")
			return
		}

		detail, recErr := ctx.RunRecognition(shelfResetNodeName, img, nil)
		if recErr != nil {
			log.Warn().
				Err(recErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_reset_market").
				Str("node", shelfResetNodeName).
				Msg("failed to locate market entry during shelf reset")
			return
		}
		// 市场入口重新可见后不再继续上滑，避免越过列表顶部滑进「当前拥有」区域。
		if detail != nil && detail.Hit {
			break
		}

		if swipeErr := swipeShelfUp(ctx); swipeErr != nil {
			log.Warn().
				Err(swipeErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_reset_swipe").
				Int("swipe_index", i).
				Msg("failed to swipe up during shelf reset")
			return
		}
	}

	detail, err := ctx.RunTask(shelfResetNodeName)
	if err != nil || detail == nil || !detail.Status.Success() {
		log.Warn().
			Err(err).
			Str("component", autoStockpileComponent).
			Str("step", "shelf_reset_click").
			Str("node", shelfResetNodeName).
			Msg("failed to re-enter market view after shelf reset")
	}
}

// scanGoodsAcrossShelfPages 固定扫描最多 3 帧（进入 1 帧 + 两次下滑各 1 帧），
// 按商品 ID 合并所有帧结果，返回合并后的商品与「露出该商品需要下滑的次数」，并把货架重置回顶部。
// 是否继续下滑只看画面有没有变化，与识别到的商品数量无关。
func scanGoodsAcrossShelfPages(
	ctx *maa.Context,
	firstImg image.Image,
	region string,
	itemMap *ItemMap,
) (goods []GoodsItem, swipeCountToReveal map[string]int, abortReason AbortReason, err error) {
	if ctx == nil || firstImg == nil {
		return nil, nil, AbortReasonGoodsOCRUnavailableWarn, fmt.Errorf("ctx or image is nil")
	}

	firstPage, abortReason, err := scanGoodsOnImage(ctx, firstImg, region, itemMap)
	if abortReason != AbortReasonNone || err != nil {
		return nil, nil, abortReason, err
	}

	pages := [][]GoodsItem{firstPage}
	swiped := 0
	resetDone := false

	// 下游要按首屏结果点击商品，因此无论后面是否降级，都要把货架重置回顶部。
	defer func() {
		if resetDone || swiped == 0 {
			return
		}
		resetShelf(ctx)
	}()

	previousImg := firstImg
	for scroll := 0; scroll < shelfScanSwipeTimes; scroll++ {
		if swipeErr := runShelfSwipe(ctx, swipeShelfDownNodeName); swipeErr != nil {
			log.Warn().
				Err(swipeErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_swipe_down").
				Str("node", swipeShelfDownNodeName).
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
				Int("scanned_pages", len(pages)).
				Msg("shelf screencap failed, keep scanned pages only")
			break
		}

		// 画面没有变化说明已经到底（或当前控制器本来就不需要滑动），继续识别只会重复扫同一屏。
		if shelfFramesLookIdentical(previousImg, currentImg) {
			log.Info().
				Str("component", autoStockpileComponent).
				Str("step", "shelf_swipe_skip").
				Int("scanned_pages", len(pages)).
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

	resetShelf(ctx)
	resetDone = true

	return goods, swipeCountToReveal, AbortReasonNone, nil
}

func pageGoodsCounts(pages [][]GoodsItem) []int {
	counts := make([]int, 0, len(pages))
	for _, page := range pages {
		counts = append(counts, len(page))
	}
	return counts
}
