// Copyright (c) 2026 Harry Huang
package maptracker

import (
	"math"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

/* ******** Big Map Viewport ******** */

// BigMapViewport represents a big-map viewport mapping between map coordinates and screen coordinates.
// Left/Top/Right/Bottom are viewport bounds in screen space.
// OriginMapX/OriginMapY are the map coordinates corresponding to the viewport's top-left corner.
// Scale is the screen-pixel-per-map-pixel ratio at current zoom.
type BigMapViewport struct {
	Left       float64 `json:"left"`
	Top        float64 `json:"top"`
	Right      float64 `json:"right"`
	Bottom     float64 `json:"bottom"`
	OriginMapX float64 `json:"originMapX"`
	OriginMapY float64 `json:"originMapY"`
	Scale      float64 `json:"scale"`
}

// NewBigMapViewport creates a viewport using the fixed big-map screen bounds and current inferred map origin/scale.
func NewBigMapViewport(originMapX, originMapY, scale float64) *BigMapViewport {
	left, top, right, bottom := bigMapViewBounds(WORK_W, WORK_H)
	return &BigMapViewport{
		Left:       float64(left),
		Top:        float64(top),
		Right:      float64(right),
		Bottom:     float64(bottom),
		OriginMapX: originMapX,
		OriginMapY: originMapY,
		Scale:      scale,
	}
}

// GetScreenCoordOf converts map coordinates to screen coordinates based on the current viewport
func (bmv *BigMapViewport) GetScreenCoordOf(mapX, mapY float64) (float64, float64) {
	viewX := bmv.Left + (mapX-bmv.OriginMapX)*bmv.Scale
	viewY := bmv.Top + (mapY-bmv.OriginMapY)*bmv.Scale
	return viewX, viewY
}

// GetMapCoordOf converts screen coordinates to map coordinates based on the current viewport.
func (bmv *BigMapViewport) GetMapCoordOf(viewX, viewY float64) (float64, float64) {
	mapX := bmv.OriginMapX + (viewX-bmv.Left)/bmv.Scale
	mapY := bmv.OriginMapY + (viewY-bmv.Top)/bmv.Scale
	return mapX, mapY
}

// IsMapCoordInView reports whether a map coordinate is currently inside the viewport.
func (bmv *BigMapViewport) IsMapCoordInView(mapX, mapY float64) bool {
	viewX, viewY := bmv.GetScreenCoordOf(mapX, mapY)
	return bmv.IsViewCoordInView(viewX, viewY)
}

// IsViewCoordInView reports whether a screen coordinate is inside the viewport bounds.
func (bmv *BigMapViewport) IsViewCoordInView(viewX, viewY float64) bool {
	return viewX >= bmv.Left && viewX <= bmv.Right && viewY >= bmv.Top && viewY <= bmv.Bottom
}

/* ******** Actions ******** */

// ActionWrapper provides synchronized touch/key operations with built-in delays
type ActionWrapper struct {
	ctx  *maa.Context
	ctrl *maa.Controller
}

// NewActionWrapper creates a new ActionWrapper from a context
func NewActionWrapper(ctx *maa.Context, ctrl *maa.Controller) *ActionWrapper {
	return &ActionWrapper{ctx, ctrl}
}

// ClickSync performs a touch down and up at (x, y)
func (aw *ActionWrapper) ClickSync(contact, x, y int, delayMillis int) {
	aw.ctrl.PostTouchDown(int32(contact), int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
	aw.ctrl.PostTouchUp(int32(contact)).Wait()
}

// SwipeSync performs an actual swipe from (x, y) to (x+dx, y+dy)
func (aw *ActionWrapper) SwipeSync(x, y, dx, dy int, durationMillis, delayMillis int) {
	stepDurationMillis := durationMillis / 2
	aw.ctrl.PostTouchDown(0, int32(x), int32(y), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	aw.ctrl.PostTouchMove(0, int32(x+dx), int32(y+dy), 1).Wait()
	time.Sleep(time.Duration(stepDurationMillis) * time.Millisecond)
	aw.ctrl.PostTouchUp(0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// SwipeHoverSync performs an only-hover swipe from (x, y) to (x+dx, y+dy)
func (aw *ActionWrapper) SwipeHoverSync(x, y, dx, dy int, durationMillis, delayMillis int) {
	aw.ctrl.PostTouchMove(0, int32(x), int32(y), 0).Wait()
	time.Sleep(time.Duration(durationMillis) * time.Millisecond)
	aw.ctrl.PostTouchMove(0, int32(x+dx), int32(y+dy), 0).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyDownSync sends a key press
func (aw *ActionWrapper) KeyDownSync(keyCode int, delayMillis int) {
	aw.ctrl.PostKeyDown(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyUpSync sends a key release
func (aw *ActionWrapper) KeyUpSync(keyCode int, delayMillis int) {
	aw.ctrl.PostKeyUp(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// KeyTypeSync sends a key press-release and waits
func (aw *ActionWrapper) KeyTypeSync(keyCode int, delayMillis int) {
	aw.ctrl.PostClickKey(int32(keyCode)).Wait()
	time.Sleep(time.Duration(delayMillis) * time.Millisecond)
}

// RotateCamera performs a camera rotation via series of mouse-keyboard operations
func (aw *ActionWrapper) RotateCamera(dx int, durationMillis, delayMillis int) {
	cx, cy := WORK_W/2, WORK_H/2
	aw.SwipeHoverSync(cx, cy, dx, 0, durationMillis, delayMillis)
}

func (aw *ActionWrapper) ResetCamera(delayMillis int) {
	cx, cy := WORK_W/2, WORK_H/2
	stepDelayMillis := delayMillis / 3
	aw.KeyDownSync(KEY_ALT, stepDelayMillis)
	aw.ClickSync(0, cx, cy, stepDelayMillis)
	aw.KeyUpSync(KEY_ALT, stepDelayMillis)
}

func bigMapViewBounds(screenW, screenH int) (left int, top int, right int, bottom int) {
	padLR := int(math.Round(PADDING_LR))
	padTB := int(math.Round(PADDING_TB))
	left = max(0, min(screenW, padLR))
	right = max(0, min(screenW, screenW-padLR))
	top = max(0, min(screenH, padTB))
	bottom = max(0, min(screenH, screenH-padTB))
	return left, top, right, bottom
}
