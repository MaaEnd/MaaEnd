package creditshopping

import (
	"fmt"
	"image"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/rs/zerolog/log"
)

// 与 Shopping.json ADBSpecial 一致：向上滑动以露出下半货架。
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

// scanShelfSlotsADB 第一次只录上排槽位 0–5，滑动后第二次只录下排槽位 6–9。
func scanShelfSlotsADB(ctx *maa.Context, ctrl *maa.Controller, first image.Image) []SlotRecord {
	hitsTop := scanShelfNameHits(ctx, first)
	slotsTop := buildSlotRecords(ctx, first, hitsTop, slotAssignADBTop)

	swipeShelfForADB(ctx, ctrl)
	second, err := screencap(ctrl)
	if err != nil {
		log.Warn().Err(err).Str("component", component).Int("top_slots", len(slotsTop)).Msg("record shelf adb: second screencap failed, keep top half only")
		return slotsTop
	}
	hitsBottom := scanShelfNameHits(ctx, second)
	slotsBottom := buildSlotRecords(ctx, second, hitsBottom, slotAssignADBBottom)

	merged := mergeSlotRecordsByPosition(slotsTop, slotsBottom)
	log.Info().
		Str("component", component).
		Int("slots_top", len(slotsTop)).
		Int("slots_bottom", len(slotsBottom)).
		Int("slots_merged", len(merged)).
		Msg("record shelf adb: top row + bottom row by slot position")
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
