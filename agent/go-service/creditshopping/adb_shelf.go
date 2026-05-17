package creditshopping

import (
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/rs/zerolog/log"
)

// 与 Shopping.json ADBSpecial 一致：小幅上滑，使第一排名称+折扣与第二排名称+折扣分属两次截图。
// 见第一排完整时看不到第二排名字；见第二排完整时看不到第一排折扣——故必须分两次记录后按 slot 合并。
const (
	adbShelfSwipeBeginX = 640
	adbShelfSwipeBeginY = 500
	adbShelfSwipeEndY   = 300
	adbShelfSwipeDurMs  = 500
	adbShelfSwipeWaitMs = 400
)

func isADBController(ctrl *maa.Controller) bool {
	t, err := control.GetControlType(ctrl)
	return err == nil && t == control.CONTROL_TYPE_ADB
}

func swipeShelfForADB(ctx *maa.Context, ctrl *maa.Controller) bool {
	ca, err := control.NewControlAdaptor(ctx, ctrl, 1280, 720)
	if err != nil {
		log.Warn().Err(err).Str("component", component).Msg("record shelf adb: swipe adaptor failed")
		return false
	}
	dy := adbShelfSwipeEndY - adbShelfSwipeBeginY
	ca.Swipe(0, adbShelfSwipeBeginX, adbShelfSwipeBeginY, 0, dy, adbShelfSwipeDurMs, adbShelfSwipeWaitMs)
	return true
}

// scanShelfSlotsADB 首屏录 slot 0–5（第一排名称+折扣），滑动后录 slot 6–9（第二排）；两屏合并为一条快照。
func scanShelfSlotsADB(ctx *maa.Context, ctrl *maa.Controller, first image.Image) []SlotRecord {
	slotsTop := buildSlotRecords(ctx, first, scanShelfNameHits(ctx, first), slotAssignADBTop)

	swipeShelfForADB(ctx, ctrl)
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
