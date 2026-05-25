//go:build windows

package accountswitch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unsafe"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const (
	swRestore = 9

	wmClose       = 0x0010
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202

	mkLButton           = 0x0001
	mouseEventFLeftDown = 0x0002
	mouseEventFLeftUp   = 0x0004
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")

	procFindWindowW         = user32.NewProc("FindWindowW")
	procIsWindow            = user32.NewProc("IsWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procBringWindowToTop    = user32.NewProc("BringWindowToTop")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSetCursorPos        = user32.NewProc("SetCursorPos")
	procMouseEvent          = user32.NewProc("mouse_event")
	procSendMessageW        = user32.NewProc("SendMessageW")
	procPostMessageW        = user32.NewProc("PostMessageW")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procScreenToClient      = user32.NewProc("ScreenToClient")
	procGetClientRect       = user32.NewProc("GetClientRect")
)

type WindowAction struct{}

type controllerInfo struct {
	Type string `json:"type"`
	HWnd uint64 `json:"hwnd"`
}

type winPoint struct {
	X int32
	Y int32
}

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func (a *WindowAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().
			Str("component", componentName).
			Msg("nil context or custom action arg")
		return false
	}

	param, err := parseWindowActionParam(arg.CustomActionParam)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("custom_action_param", arg.CustomActionParam).
			Msg("failed to parse account switch window action param")
		return false
	}

	hwnd, err := resolveWindow(ctx, param)
	if err != nil {
		if param.Optional {
			log.Warn().
				Err(err).
				Str("component", componentName).
				Str("op", param.Op).
				Str("window", param.Window).
				Msg("account switch window action skipped optional missing window")
			return true
		}
		log.Error().
			Err(err).
			Str("component", componentName).
			Str("op", param.Op).
			Str("window", param.Window).
			Msg("failed to resolve account switch window")
		return false
	}

	switch param.Op {
	case opFocus:
		focusWindow(hwnd)
		return true
	case opClose:
		sendMessage(hwnd, wmClose, 0, 0)
		return true
	case opClick:
		return clickWindow(ctx, arg, hwnd, param)
	default:
		log.Error().
			Str("component", componentName).
			Str("op", param.Op).
			Msg("unsupported account switch window action op")
		return false
	}
}

func resolveWindow(ctx *maa.Context, param windowActionParam) (uintptr, error) {
	if param.Window == windowMain {
		if hwnd := controllerHwnd(ctx); hwnd != 0 && isWindow(hwnd) {
			return hwnd, nil
		}
	}

	selector, ok := defaultWindowSelectors[param.Window]
	if !ok {
		return 0, fmt.Errorf("unknown window alias: %s", param.Window)
	}
	hwnd, err := findWindow(selector.ClassName, selector.WindowName)
	if err != nil {
		return 0, err
	}
	if hwnd == 0 || !isWindow(hwnd) {
		return 0, fmt.Errorf("window not found: %s", describeWindowSelector(param))
	}
	return hwnd, nil
}

func clickWindow(ctx *maa.Context, arg *maa.CustomActionArg, targetHwnd uintptr, param windowActionParam) bool {
	box, ok := recognitionBox(arg)
	if !ok {
		log.Error().
			Str("component", componentName).
			Str("window", param.Window).
			Msg("missing recognition box for account switch window click")
		return false
	}

	mainHwnd := controllerHwnd(ctx)
	if mainHwnd == 0 || !isWindow(mainHwnd) {
		selector := defaultWindowSelectors[windowMain]
		fallback, err := findWindow(selector.ClassName, selector.WindowName)
		if err != nil {
			log.Error().
				Err(err).
				Str("component", componentName).
				Msg("failed to find fallback main window")
			return false
		}
		mainHwnd = fallback
	}
	if mainHwnd == 0 || !isWindow(mainHwnd) {
		log.Error().
			Str("component", componentName).
			Msg("main window hwnd is unavailable for coordinate mapping")
		return false
	}

	ctrlWidth, ctrlHeight, err := controllerResolution(ctx)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", componentName).
			Msg("failed to get controller resolution")
		return false
	}

	mainRect, ok := getClientRect(mainHwnd)
	if !ok {
		log.Error().
			Str("component", componentName).
			Uint64("main_hwnd", uint64(mainHwnd)).
			Msg("failed to get main window client rect")
		return false
	}
	targetRect, ok := getClientRect(targetHwnd)
	if !ok {
		log.Error().
			Str("component", componentName).
			Uint64("target_hwnd", uint64(targetHwnd)).
			Msg("failed to get target window client rect")
		return false
	}

	mainWidth := mainRect.Right - mainRect.Left
	mainHeight := mainRect.Bottom - mainRect.Top
	targetWidth := targetRect.Right - targetRect.Left
	targetHeight := targetRect.Bottom - targetRect.Top
	if mainWidth <= 0 || mainHeight <= 0 || targetWidth <= 0 || targetHeight <= 0 {
		log.Error().
			Str("component", componentName).
			Int32("main_width", mainWidth).
			Int32("main_height", mainHeight).
			Int32("target_width", targetWidth).
			Int32("target_height", targetHeight).
			Msg("invalid client rect for account switch window click")
		return false
	}

	boxCenterX := box.X() + box.Width()/2
	boxCenterY := box.Y() + box.Height()/2
	mainClient := winPoint{
		X: scaleCoord(boxCenterX, int(ctrlWidth), mainWidth),
		Y: scaleCoord(boxCenterY, int(ctrlHeight), mainHeight),
	}
	screenPoint := mainClient
	if !clientToScreen(mainHwnd, &screenPoint) {
		log.Error().
			Str("component", componentName).
			Uint64("main_hwnd", uint64(mainHwnd)).
			Msg("failed to convert main client point to screen point")
		return false
	}
	targetClient := screenPoint
	if !screenToClient(targetHwnd, &targetClient) {
		log.Error().
			Str("component", componentName).
			Uint64("target_hwnd", uint64(targetHwnd)).
			Msg("failed to convert screen point to target client point")
		return false
	}

	if targetClient.X < 0 || targetClient.Y < 0 || targetClient.X >= targetWidth || targetClient.Y >= targetHeight {
		log.Error().
			Str("component", componentName).
			Str("window", param.Window).
			Uint64("target_hwnd", uint64(targetHwnd)).
			Int32("target_x", targetClient.X).
			Int32("target_y", targetClient.Y).
			Int32("target_width", targetWidth).
			Int32("target_height", targetHeight).
			Msg("mapped click point is outside target window client rect")
		return false
	}

	clickMode := "message"
	if param.Window == windowCombo {
		clickMode = "cursor"
		if !cursorClick(screenPoint) {
			return false
		}
	} else if !messageClick(targetHwnd, targetClient, screenPoint) {
		return false
	}

	log.Debug().
		Str("component", componentName).
		Str("window", param.Window).
		Str("click_method", clickMode).
		Uint64("target_hwnd", uint64(targetHwnd)).
		Int("box_x", box.X()).
		Int("box_y", box.Y()).
		Int("box_w", box.Width()).
		Int("box_h", box.Height()).
		Int32("target_x", targetClient.X).
		Int32("target_y", targetClient.Y).
		Msg("posted account switch window click")
	return true
}

func messageClick(hwnd uintptr, clientPoint winPoint, screenPoint winPoint) bool {
	focusWindow(hwnd)
	// Keep the real cursor position in sync with Qt's cursor-position based hit testing.
	if !setCursorPos(screenPoint.X, screenPoint.Y) {
		log.Warn().
			Str("component", componentName).
			Int32("screen_x", screenPoint.X).
			Int32("screen_y", screenPoint.Y).
			Msg("failed to sync cursor position before post message click")
	}

	lparam := makeLParam(clientPoint.X, clientPoint.Y)
	if !postMessage(hwnd, wmMouseMove, 0, lparam) {
		return logPostMessageFailure(hwnd, wmMouseMove)
	}
	if !postMessage(hwnd, wmLButtonDown, mkLButton, lparam) {
		return logPostMessageFailure(hwnd, wmLButtonDown)
	}
	time.Sleep(50 * time.Millisecond)
	if !postMessage(hwnd, wmLButtonUp, 0, lparam) {
		return logPostMessageFailure(hwnd, wmLButtonUp)
	}
	return true
}

func cursorClick(screenPoint winPoint) bool {
	if !setCursorPos(screenPoint.X, screenPoint.Y) {
		log.Error().
			Str("component", componentName).
			Int32("screen_x", screenPoint.X).
			Int32("screen_y", screenPoint.Y).
			Msg("failed to move cursor for no-focus click")
		return false
	}
	time.Sleep(30 * time.Millisecond)
	mouseEvent(mouseEventFLeftDown, 0, 0, 0, 0)
	time.Sleep(50 * time.Millisecond)
	mouseEvent(mouseEventFLeftUp, 0, 0, 0, 0)
	return true
}

func recognitionBox(arg *maa.CustomActionArg) (maa.Rect, bool) {
	if arg == nil {
		return maa.Rect{}, false
	}
	if arg.Box.Width() > 0 && arg.Box.Height() > 0 {
		return arg.Box, true
	}
	if arg.RecognitionDetail != nil && arg.RecognitionDetail.Box.Width() > 0 && arg.RecognitionDetail.Box.Height() > 0 {
		return arg.RecognitionDetail.Box, true
	}
	return maa.Rect{}, false
}

func controllerHwnd(ctx *maa.Context) uintptr {
	if ctx == nil || ctx.GetTasker() == nil || ctx.GetTasker().GetController() == nil {
		return 0
	}
	infoStr, err := ctx.GetTasker().GetController().GetInfo()
	if err != nil || strings.TrimSpace(infoStr) == "" {
		return 0
	}
	var info controllerInfo
	if err := json.Unmarshal([]byte(infoStr), &info); err != nil {
		return 0
	}
	return uintptr(info.HWnd)
}

func controllerResolution(ctx *maa.Context) (int32, int32, error) {
	if ctx == nil || ctx.GetTasker() == nil || ctx.GetTasker().GetController() == nil {
		return 0, 0, fmt.Errorf("nil controller")
	}
	width, height, err := ctx.GetTasker().GetController().GetResolution()
	if err != nil {
		return 0, 0, err
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid controller resolution: %dx%d", width, height)
	}
	return width, height, nil
}

func findWindow(className, windowName string) (uintptr, error) {
	if className == "" && windowName == "" {
		return 0, fmt.Errorf("class_name and window_name cannot both be empty")
	}

	classPtr, err := optionalUTF16Ptr(className)
	if err != nil {
		return 0, fmt.Errorf("invalid class_name: %w", err)
	}
	windowPtr, err := optionalUTF16Ptr(windowName)
	if err != nil {
		return 0, fmt.Errorf("invalid window_name: %w", err)
	}

	hwnd, _, _ := procFindWindowW.Call(
		uintptr(unsafe.Pointer(classPtr)),
		uintptr(unsafe.Pointer(windowPtr)),
	)
	return hwnd, nil
}

func optionalUTF16Ptr(value string) (*uint16, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	return windows.UTF16PtrFromString(value)
}

func isWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	ret, _, _ := procIsWindow.Call(hwnd)
	return ret != 0
}

func focusWindow(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	procShowWindow.Call(hwnd, swRestore)
	procBringWindowToTop.Call(hwnd)
	ret, _, _ := procSetForegroundWindow.Call(hwnd)
	return ret != 0
}

func setCursorPos(x, y int32) bool {
	ret, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	return ret != 0
}

func mouseEvent(flags, dx, dy, data, extraInfo uintptr) {
	procMouseEvent.Call(flags, dx, dy, data, extraInfo)
}

func sendMessage(hwnd, msg, wparam, lparam uintptr) uintptr {
	ret, _, _ := procSendMessageW.Call(hwnd, msg, wparam, lparam)
	return ret
}

func postMessage(hwnd, msg, wparam, lparam uintptr) bool {
	ret, _, _ := procPostMessageW.Call(hwnd, msg, wparam, lparam)
	return ret != 0
}

func getClientRect(hwnd uintptr) (winRect, bool) {
	var rect winRect
	ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	return rect, ret != 0
}

func clientToScreen(hwnd uintptr, point *winPoint) bool {
	ret, _, _ := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(point)))
	return ret != 0
}

func screenToClient(hwnd uintptr, point *winPoint) bool {
	ret, _, _ := procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(point)))
	return ret != 0
}

func scaleCoord(value int, sourceSize int, targetSize int32) int32 {
	if sourceSize <= 0 || targetSize <= 0 {
		return 0
	}
	return int32((int64(value)*int64(targetSize) + int64(sourceSize)/2) / int64(sourceSize))
}

func makeLParam(x, y int32) uintptr {
	return uintptr(uint32(uint16(x)) | uint32(uint16(y))<<16)
}

func describeWindowSelector(param windowActionParam) string {
	selector, ok := defaultWindowSelectors[param.Window]
	if !ok {
		return fmt.Sprintf("window=%q", param.Window)
	}
	return fmt.Sprintf("window=%q class_name=%q window_name=%q", param.Window, selector.ClassName, selector.WindowName)
}

func logPostMessageFailure(hwnd uintptr, msg uintptr) bool {
	log.Error().
		Str("component", componentName).
		Uint64("target_hwnd", uint64(hwnd)).
		Uint("message", uint(msg)).
		Msg("failed to post account switch mouse message")
	return false
}
