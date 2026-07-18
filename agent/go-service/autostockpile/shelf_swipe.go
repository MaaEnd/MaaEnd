package autostockpile

import (
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func runShelfSwipe(ctx *maa.Context, nodeName string) error {
	if ctx == nil {
		return fmt.Errorf("context is nil")
	}

	_, err := ctx.RunAction(nodeName, maa.Rect{0, 0, 0, 0}, "", nil)
	if err != nil {
		return err
	}
	return nil
}

func swipeShelfDown(ctx *maa.Context) error {
	return runShelfSwipe(ctx, swipeShelfDownNodeName)
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
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}
	return img, nil
}

// scanGoodsWithOptionalSecondPage 首屏扫描后下滑一次再扫次屏，按 ID 合并，并尽量滑回首屏。
func scanGoodsWithOptionalSecondPage(
	ctx *maa.Context,
	firstImg image.Image,
	region string,
	itemMap *ItemMap,
) (goods []GoodsItem, secondPageOnlyIDs []string, abortReason AbortReason, err error) {
	page0, abortReason, err := scanGoodsOnImage(ctx, firstImg, region, itemMap)
	if abortReason != AbortReasonNone || err != nil {
		return nil, nil, abortReason, err
	}

	if swipeErr := swipeShelfDown(ctx); swipeErr != nil {
		log.Warn().
			Err(swipeErr).
			Str("component", autoStockpileComponent).
			Str("step", "shelf_swipe_down").
			Int("page0_count", len(page0)).
			Msg("shelf swipe down failed, keep first page only")
		return page0, nil, AbortReasonNone, nil
	}

	restored := false
	defer func() {
		if restored {
			return
		}
		if upErr := swipeShelfUp(ctx); upErr != nil {
			log.Warn().
				Err(upErr).
				Str("component", autoStockpileComponent).
				Str("step", "shelf_swipe_up").
				Msg("failed to restore shelf to first page")
		}
	}()

	secondImg, capErr := screencapShelf(ctx)
	if capErr != nil {
		log.Warn().
			Err(capErr).
			Str("component", autoStockpileComponent).
			Str("step", "shelf_screencap").
			Int("page0_count", len(page0)).
			Msg("second page screencap failed, keep first page only")
		return page0, nil, AbortReasonNone, nil
	}

	page1, page1Abort, page1Err := scanGoodsOnImage(ctx, secondImg, region, itemMap)
	if page1Abort != AbortReasonNone || page1Err != nil {
		log.Warn().
			Err(page1Err).
			Str("component", autoStockpileComponent).
			Str("step", "scan_page1").
			Str("abort_reason", string(page1Abort)).
			Int("page0_count", len(page0)).
			Msg("second page scan failed, keep first page only")
		return page0, nil, AbortReasonNone, nil
	}

	goods, secondPageOnlyIDs = mergeGoodsByID(page0, page1)
	log.Info().
		Str("component", autoStockpileComponent).
		Str("step", "merge_pages").
		Int("page0_count", len(page0)).
		Int("page1_count", len(page1)).
		Int("merged_count", len(goods)).
		Int("second_page_only_count", len(secondPageOnlyIDs)).
		Strs("second_page_only_ids", secondPageOnlyIDs).
		Msg("dual-page goods scan merged")

	if upErr := swipeShelfUp(ctx); upErr != nil {
		log.Warn().
			Err(upErr).
			Str("component", autoStockpileComponent).
			Str("step", "shelf_swipe_up").
			Msg("failed to restore shelf to first page after merge")
	} else {
		restored = true
	}

	return goods, secondPageOnlyIDs, AbortReasonNone, nil
}
