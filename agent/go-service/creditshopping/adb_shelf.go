package creditshopping

import (
	"fmt"
	"image"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 货架滑动工具节点：小幅上滑，使第一排名称+折扣与第二排名称+折扣分属两次截图。
// 见第一排完整时看不到第二排名字；见第二排完整时看不到第一排折扣——故必须分两次记录后按 slot 合并。
// base 为 Win32（一屏可见，DoNothing），实际滑动参数与等待由 resource_adb 覆盖决定。
const (
	shelfSwipeDownNode = "CreditShoppingSwipeShelfDown"
	shelfSwipeUpNode   = "CreditShoppingSwipeShelfUp"
)

// isADBController 判断当前控制器是否走触控 + 移动端 UI（ADB / PlayCover / Android 原生）。
// ctrl 由回调入口传入；PI_CONTROLLER 缺失时用它兜底，否则 ADB 会被误当成桌面端。
func isADBController(ctrl *maa.Controller) bool {
	return pienv.IsAdbLikeControllerType(pienv.ResolveControllerKind(ctrl))
}

// runShelfSwipe 调一次货架滑动工具节点；滑动参数与稳定等待都在 Pipeline 侧，这里不做平台判断。
func runShelfSwipe(ctx *maa.Context, nodeName string) bool {
	if ctx == nil {
		log.Warn().Str("component", component).Str("node", nodeName).Msg("record shelf adb: nil context")
		return false
	}
	if _, err := ctx.RunAction(nodeName, maa.Rect{0, 0, 0, 0}, "", nil); err != nil {
		log.Warn().Err(err).Str("component", component).Str("node", nodeName).Msg("record shelf adb: swipe node failed")
		return false
	}
	return true
}

// scanShelfSlotsADB 首屏录 slot 0–5（第一排名称+折扣），滑动后录 slot 6–9（第二排）；两屏合并为一条快照。
func scanShelfSlotsADB(ctx *maa.Context, ctrl *maa.Controller, first image.Image) []SlotRecord {
	slotsTop := buildSlotRecords(ctx, first, scanShelfNameHits(ctx, first), slotAssignADBTop)

	// 先小幅上滑采集第二屏；defer 保证任意返回路径都会滑回第一屏，避免后续购买节点仍停留在第二屏。
	swiped := runShelfSwipe(ctx, shelfSwipeDownNode)
	if swiped {
		defer func() {
			if !runShelfSwipe(ctx, shelfSwipeUpNode) {
				log.Warn().Str("component", component).Msg("record shelf adb: failed to swipe back after shelf scan")
			}
		}()
	}
	second, err := screencap(ctrl)
	if err != nil {
		log.Warn().Err(err).Str("component", component).Int("top_slots", len(slotsTop)).Msg("record shelf adb: second screencap failed, keep first row only")
		return slotsTop
	}
	slotsBottom := buildSlotRecords(ctx, second, scanShelfNameHits(ctx, second), slotAssignADBBottom)

	merged := mergeSlotRecordsByPosition(slotsTop, slotsBottom)
	log.Info().
		Str("component", component).
		Int("slots_row1", len(slotsTop)).
		Int("slots_row2", len(slotsBottom)).
		Int("slots_merged", len(merged)).
		Bool("restored", swiped).
		Msg("record shelf adb: row1 screen + swipe + row2 screen merged by slot")
	return merged
}

func screencap(ctrl *maa.Controller) (image.Image, error) {
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
