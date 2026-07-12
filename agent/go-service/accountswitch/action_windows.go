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

	wmClose = 0x0010

	mouseEventFLeftDown = 0x0002
	mouseEventFLeftUp   = 0x0004

	awarenessContextPerMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
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
	procGetClientRect       = user32.NewProc("GetClientRect")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procClientToScreen      = user32.NewProc("ClientToScreen")
	procSetThreadDpiCtx     = user32.NewProc("SetThreadDpiAwarenessContext")
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
	if ctx == nil || ctx.GetTasker() == nil || ctx.GetTasker().GetController() == nil {
		log.Error().
			Str("component", componentName).
			Msg("nil controller for account switch window click")
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

	restoreDpiCtx := setDPIAware()
	defer restoreDpiCtx()

	sourceWidth, sourceHeight, err := sourceImageSize(ctx)
	if err != nil || sourceWidth <= 0 || sourceHeight <= 0 {
		log.Error().
			Err(err).
			Str("component", componentName).
			Int("source_width", sourceWidth).
			Int("source_height", sourceHeight).
			Msg("failed to get source image size for account switch click")
		return false
	}

	mainClientRect, ok := getClientRect(mainHwnd)
	if !ok {
		log.Error().
			Str("component", componentName).
			Uint64("main_hwnd", uint64(mainHwnd)).
			Msg("failed to get main window client rect")
		return false
	}
	mainClientOrigin := winPoint{}
	if !clientToScreen(mainHwnd, &mainClientOrigin) {
		log.Error().
			Str("component", componentName).
			Uint64("main_hwnd", uint64(mainHwnd)).
			Msg("failed to get main window client origin")
		return false
	}
	targetRect, ok := getWindowRect(targetHwnd)
	if !ok {
		log.Error().
			Str("component", componentName).
			Uint64("target_hwnd", uint64(targetHwnd)).
			Msg("failed to get target window rect")
		return false
	}

	mainWidth := mainClientRect.Right - mainClientRect.Left
	mainHeight := mainClientRect.Bottom - mainClientRect.Top
	if mainWidth <= 0 || mainHeight <= 0 {
		log.Error().
			Str("component", componentName).
			Int32("main_width", mainWidth).
			Int32("main_height", mainHeight).
			Msg("invalid main client rect for account switch click")
		return false
	}

	boxCenterX := box.X() + box.Width()/2
	boxCenterY := box.Y() + box.Height()/2
	screenX := mainClientOrigin.X + scaleCoord(boxCenterX, sourceWidth, mainWidth)
	screenY := mainClientOrigin.Y + scaleCoord(boxCenterY, sourceHeight, mainHeight)
	if screenX < targetRect.Left || screenY < targetRect.Top || screenX >= targetRect.Right || screenY >= targetRect.Bottom {
		log.Warn().
			Str("component", componentName).
			Str("window", param.Window).
			Uint64("target_hwnd", uint64(targetHwnd)).
			Int32("screen_x", screenX).
			Int32("screen_y", screenY).
			Int32("target_left", targetRect.Left).
			Int32("target_top", targetRect.Top).
			Int32("target_right", targetRect.Right).
			Int32("target_bottom", targetRect.Bottom).
			Msg("mapped click point is outside target window rect")
	}

	if !cursorClick(screenX, screenY) {
		return false
	}

	log.Debug().
		Str("component", componentName).
		Str("window", param.Window).
		Uint64("main_hwnd", uint64(mainHwnd)).
		Uint64("target_hwnd", uint64(targetHwnd)).
		Int("box_x", box.X()).
		Int("box_y", box.Y()).
		Int("box_w", box.Width()).
		Int("box_h", box.Height()).
		Int("source_width", sourceWidth).
		Int("source_height", sourceHeight).
		Int32("main_client_left", mainClientOrigin.X).
		Int32("main_client_top", mainClientOrigin.Y).
		Int32("main_client_width", mainWidth).
		Int32("main_client_height", mainHeight).
		Int32("screen_x", screenX).
		Int32("screen_y", screenY).
		Msg("clicked account switch window by physical cursor")
	return true
}

func sourceImageSize(ctx *maa.Context) (int, int, error) {
	if ctx == nil || ctx.GetTasker() == nil || ctx.GetTasker().GetController() == nil {
		return 0, 0, fmt.Errorf("nil controller")
	}
	ctrl := ctx.GetTasker().GetController()
	if img, err := ctrl.CacheImage(); err == nil && img != nil {
		bounds := img.Bounds()
		if bounds.Dx() > 0 && bounds.Dy() > 0 {
			return bounds.Dx(), bounds.Dy(), nil
		}
	}
	width, height, err := ctrl.GetResolution()
	if err != nil {
		return 0, 0, err
	}
	return int(width), int(height), nil
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

func sendMessage(hwnd, msg, wparam, lparam uintptr) uintptr {
	ret, _, _ := procSendMessageW.Call(hwnd, msg, wparam, lparam)
	return ret
}

func getClientRect(hwnd uintptr) (winRect, bool) {
	var rect winRect
	ret, _, _ := procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	return rect, ret != 0
}

func getWindowRect(hwnd uintptr) (winRect, bool) {
	var rect winRect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	return rect, ret != 0
}

func clientToScreen(hwnd uintptr, point *winPoint) bool {
	ret, _, _ := procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(point)))
	return ret != 0
}

func setDPIAware() func() {
	if err := procSetThreadDpiCtx.Find(); err != nil {
		return func() {}
	}
	oldCtx, _, _ := procSetThreadDpiCtx.Call(awarenessContextPerMonitorAwareV2)
	return func() {
		if oldCtx != 0 {
			procSetThreadDpiCtx.Call(oldCtx)
		}
	}
}

func scaleCoord(value int, sourceSize int, targetSize int32) int32 {
	if sourceSize <= 0 || targetSize <= 0 {
		return 0
	}
	return int32((int64(value)*int64(targetSize) + int64(sourceSize)/2) / int64(sourceSize))
}

func cursorClick(screenX, screenY int32) bool {
	if !setCursorPos(screenX, screenY) {
		log.Error().
			Str("component", componentName).
			Int32("screen_x", screenX).
			Int32("screen_y", screenY).
			Msg("failed to move cursor for account switch click")
		return false
	}
	time.Sleep(30 * time.Millisecond)
	mouseEvent(mouseEventFLeftDown, 0, 0, 0, 0)
	time.Sleep(50 * time.Millisecond)
	mouseEvent(mouseEventFLeftUp, 0, 0, 0, 0)
	return true
}

func setCursorPos(x, y int32) bool {
	ret, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	return ret != 0
}

func mouseEvent(flags, dx, dy, data, extraInfo uintptr) {
	procMouseEvent.Call(flags, dx, dy, data, extraInfo)
}

func describeWindowSelector(param windowActionParam) string {
	selector, ok := defaultWindowSelectors[param.Window]
	if !ok {
		return fmt.Sprintf("window=%q", param.Window)
	}
	return fmt.Sprintf("window=%q class_name=%q window_name=%q", param.Window, selector.ClassName, selector.WindowName)
}
