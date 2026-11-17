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

// shelfResetSwipes 是复位时向上滑动的次数。列表顶部会被夹紧，多滑一次不会越界，
// 因此固定滑两次，确保从货架任意位置都能回到整页顶部。
const shelfResetSwipes = 2

// resetShelf 把货架复位到「市场段顶部」这个固定起点：先向上滑到整页顶部
// （列表顶部夹紧，多滑无害），再点击市场入口让 UI 跳到市场段顶部。
// 只有列表位于整页顶部时点击市场入口才会触发跳转，因此上滑必须真正回到顶部。
// 固定起点让 SwipeCountToReveal 的「从起点下滑 N 次」语义稳定；
// 复位失败只记录日志，不阻断本次识别结果（后续点击可能因此偏位）。
func resetShelf(ctx *maa.Context) {
	for i := 0; i < shelfResetSwipes; i++ {
		if err := swipeShelfUp(ctx); err != nil {
			log.Warn().
				Err(err).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_reset_swipe").
				Str("node", swipeShelfUpNodeName).
				Int("swipe_index", i).
				Msg("failed to swipe up during shelf reset")
			return
		}
	}

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
	if detail == nil || !detail.Hit {
		log.Warn().
			Str("component", autoStockpileComponent).
			Str("step", "shelf_reset_market").
			Str("node", shelfResetNodeName).
			Msg("market entry not visible after scrolling to top, skip the reset click")
		return
	}

	if _, clickErr := ctx.RunAction(shelfResetNodeName, detail.Box, "", nil); clickErr != nil {
		log.Warn().
			Err(clickErr).
			Str("component", autoStockpileComponent).
			Str("step", "shelf_reset_click").
			Str("node", shelfResetNodeName).
			Msg("failed to click market entry during shelf reset")
		return
	}

	log.Info().
		Str("component", autoStockpileComponent).
		Str("step", "shelf_reset_click").
		Str("node", shelfResetNodeName).
		Msg("shelf reset to market section top")
}

// samePageGoods 判断两屏识别到的商品集合是否完全一致（按商品 ID）。
// 单帧像素比较会被 UI 微动画破坏，而商品 ID 集合不受动画影响：
// 集合相同说明这次下滑没有让货架前进，继续扫描只会重复识别同一屏。
func samePageGoods(previous, current []GoodsItem) bool {
	if len(previous) != len(current) {
		return false
	}

	previousIDs := make(map[string]struct{}, len(previous))
	for _, item := range previous {
		previousIDs[item.ID] = struct{}{}
	}
	for _, item := range current {
		if _, ok := previousIDs[item.ID]; !ok {
			return false
		}
	}

	return true
}

func logShelfScanSkip(reason string, scannedPages int) {
	log.Info().
		Str("component", autoStockpileComponent).
		Str("step", "shelf_swipe_skip").
		Str("reason", reason).
		Int("scanned_pages", scannedPages).
		Msg("shelf did not advance after swipe, stop scanning further pages")
}

// scanGoodsAcrossShelfPages 固定扫描最多 3 帧（进入 1 帧 + 两次下滑各 1 帧），
// 按商品 ID 合并所有帧结果，返回合并后的商品与「露出该商品需要下滑的次数」，并把货架送回扫描起点。
// 是否继续下滑只看货架有没有前进，与识别到的商品数量无关。
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

	// 下游要按「市场段顶部」这个固定起点点击商品，因此无论后面是否降级，都要复位货架。
	defer func() {
		if swiped > 0 {
			resetShelf(ctx)
		}
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
			logShelfScanSkip("frame_unchanged", len(pages))
			break
		}
		// 画面有变化说明这次下滑可能移动了货架，先记入待复原次数。
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

		// 像素比较会被 UI 微动画破坏（谷地常见），因此再用商品集合确认货架是否真的前进。
		if samePageGoods(pages[len(pages)-1], page) {
			logShelfScanSkip("goods_unchanged", len(pages))
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

	return goods, swipeCountToReveal, AbortReasonNone, nil
}

func pageGoodsCounts(pages [][]GoodsItem) []int {
	counts := make([]int, 0, len(pages))
	for _, page := range pages {
		counts = append(counts, len(page))
	}
	return counts
}
