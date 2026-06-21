//go:build windows

package aspectratio

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unsafe"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const (
	win32WmSysKeyDown = 0x0104
	win32WmSysKeyUp   = 0x0105

	win32WmSysKeyAltEnterDown = 0x20000000
	win32WmSysKeyAltEnterUp   = 0xE0000001

	win32VkReturn = 0x0D

	win32SwRestore               = 9
	win32AttachThreadInputAttach = 1
	win32AttachThreadInputDetach = 0

	win32DPIAwarenessContextPerMonitorAwareV2 = ^uintptr(3)
)

var (
	win32User32                       = windows.NewLazySystemDLL("user32.dll")
	win32Kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	win32ProcIsWindow                 = win32User32.NewProc("IsWindow")
	win32ProcPostMessageW             = win32User32.NewProc("PostMessageW")
	win32ProcGetClientRect            = win32User32.NewProc("GetClientRect")
	win32ProcSetThreadDpiAwarenessCtx = win32User32.NewProc("SetThreadDpiAwarenessContext")
	win32ProcShowWindow               = win32User32.NewProc("ShowWindow")
	win32ProcBringWindowToTop         = win32User32.NewProc("BringWindowToTop")
	win32ProcSetForegroundWindow      = win32User32.NewProc("SetForegroundWindow")
	win32ProcGetForegroundWindow      = win32User32.NewProc("GetForegroundWindow")
	win32ProcGetWindowThreadProcessID = win32User32.NewProc("GetWindowThreadProcessId")
	win32ProcAttachThreadInput        = win32User32.NewProc("AttachThreadInput")
	win32ProcGetCurrentThreadID       = win32Kernel32.NewProc("GetCurrentThreadId")
)

type win32ControllerInfo struct {
	Type string `json:"type"`
	HWnd uint64 `json:"hwnd"`
}

type win32Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func init() {
	sendAltEnterWindowsImpl = sendAltEnterWin32
}

func sendAltEnterWin32(controller *maa.Controller) (resolutionReader, error) {
	hwnd, err := controllerHwndWin32(controller)
	if err != nil {
		return nil, err
	}
	if err := ensureAltEnterWin32APIs(); err != nil {
		return nil, err
	}
	if ret, _, _ := win32ProcIsWindow.Call(hwnd); ret == 0 {
		return nil, fmt.Errorf("invalid HWND: %d", hwnd)
	}

	focusConfirmed := focusWindowForAltEnterWin32(hwnd)
	if !focusConfirmed {
		log.Warn().
			Uint64("target_hwnd", uint64(hwnd)).
			Uint64("foreground_hwnd", uint64(foregroundWindowWin32())).
			Bool("focus_confirmed", false).
			Msg("Failed to confirm foreground window before Alt+Enter, continuing with PostMessage fallback")
	}

	if err := postMessageWin32(hwnd, win32WmSysKeyDown, win32VkReturn, win32WmSysKeyAltEnterDown, "SYSKEYDOWN"); err != nil {
		return nil, err
	}
	time.Sleep(50 * time.Millisecond)
	if err := postMessageWin32(hwnd, win32WmSysKeyUp, win32VkReturn, win32WmSysKeyAltEnterUp, "SYSKEYUP"); err != nil {
		return nil, err
	}

	log.Debug().
		Uint64("hwnd", uint64(hwnd)).
		Uint64("foreground_hwnd", uint64(foregroundWindowWin32())).
		Bool("focus_confirmed", focusConfirmed).
		Msg("Alt+Enter key sequence completed")
	return func() (int32, int32, error) {
		return getClientResolutionWin32(hwnd)
	}, nil
}

func ensureAltEnterWin32APIs() error {
	for _, p := range []*windows.LazyProc{
		win32ProcIsWindow,
		win32ProcPostMessageW,
		win32ProcGetClientRect,
		win32ProcShowWindow,
		win32ProcBringWindowToTop,
		win32ProcSetForegroundWindow,
		win32ProcGetForegroundWindow,
		win32ProcGetWindowThreadProcessID,
		win32ProcAttachThreadInput,
		win32ProcGetCurrentThreadID,
	} {
		if err := p.Find(); err != nil {
			return fmt.Errorf("win32 API unavailable: %w", err)
		}
	}
	return nil
}

func focusWindowForAltEnterWin32(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	if foregroundWindowWin32() == hwnd {
		log.Debug().
			Uint64("target_hwnd", uint64(hwnd)).
			Uint64("foreground_hwnd", uint64(hwnd)).
			Bool("focus_confirmed", true).
			Msg("Game window is already foreground before Alt+Enter")
		return true
	}

	requestForegroundWindowWin32(hwnd)
	time.Sleep(100 * time.Millisecond)
	if foregroundWindowWin32() == hwnd {
		log.Debug().
			Uint64("target_hwnd", uint64(hwnd)).
			Uint64("foreground_hwnd", uint64(hwnd)).
			Bool("focus_confirmed", true).
			Msg("Focused game window before Alt+Enter")
		return true
	}

	focusWindowWithAttachedInputWin32(hwnd)
	time.Sleep(100 * time.Millisecond)
	foregroundHwnd := foregroundWindowWin32()
	focusConfirmed := foregroundHwnd == hwnd
	if focusConfirmed {
		log.Debug().
			Uint64("target_hwnd", uint64(hwnd)).
			Uint64("foreground_hwnd", uint64(foregroundHwnd)).
			Bool("focus_confirmed", true).
			Msg("Focused game window before Alt+Enter")
	}
	return focusConfirmed
}

func requestForegroundWindowWin32(hwnd uintptr) {
	win32ProcShowWindow.Call(hwnd, win32SwRestore)
	win32ProcBringWindowToTop.Call(hwnd)
	win32ProcSetForegroundWindow.Call(hwnd)
}

func focusWindowWithAttachedInputWin32(hwnd uintptr) {
	currentThreadID := currentThreadIDWin32()
	targetThreadID := windowThreadIDWin32(hwnd)
	foregroundHwnd := foregroundWindowWin32()
	foregroundThreadID := windowThreadIDWin32(foregroundHwnd)

	detachForeground := attachThreadInputWin32(currentThreadID, foregroundThreadID)
	defer detachForeground()
	detachTarget := attachThreadInputWin32(currentThreadID, targetThreadID)
	defer detachTarget()

	requestForegroundWindowWin32(hwnd)
}

func attachThreadInputWin32(sourceThreadID, targetThreadID uintptr) func() {
	if sourceThreadID == 0 || targetThreadID == 0 || sourceThreadID == targetThreadID {
		return func() {}
	}
	ret, _, _ := win32ProcAttachThreadInput.Call(sourceThreadID, targetThreadID, win32AttachThreadInputAttach)
	if ret == 0 {
		log.Debug().
			Uint64("source_thread_id", uint64(sourceThreadID)).
			Uint64("target_thread_id", uint64(targetThreadID)).
			Msg("AttachThreadInput failed while focusing game window before Alt+Enter")
		return func() {}
	}
	return func() {
		win32ProcAttachThreadInput.Call(sourceThreadID, targetThreadID, win32AttachThreadInputDetach)
	}
}

func foregroundWindowWin32() uintptr {
	hwnd, _, _ := win32ProcGetForegroundWindow.Call()
	return hwnd
}

func windowThreadIDWin32(hwnd uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	threadID, _, _ := win32ProcGetWindowThreadProcessID.Call(hwnd, 0)
	return threadID
}

func currentThreadIDWin32() uintptr {
	threadID, _, _ := win32ProcGetCurrentThreadID.Call()
	return threadID
}

func postMessageWin32(hwnd, msg, wparam, lparam uintptr, op string) error {
	if ret, _, err := win32ProcPostMessageW.Call(hwnd, msg, wparam, lparam); ret == 0 {
		if err != nil && err != windows.ERROR_SUCCESS {
			return fmt.Errorf("PostMessage %s failed: %w", op, err)
		}
		return fmt.Errorf("PostMessage %s failed with ret=0", op)
	}
	return nil
}

func getClientResolutionWin32(hwnd uintptr) (int32, int32, error) {
	if err := ensureAltEnterWin32APIs(); err != nil {
		return 0, 0, err
	}
	if ret, _, _ := win32ProcIsWindow.Call(hwnd); ret == 0 {
		return 0, 0, fmt.Errorf("invalid HWND: %d", hwnd)
	}

	restoreDPIContext := setDPIAwareWin32()
	defer restoreDPIContext()

	var rect win32Rect
	if ret, _, _ := win32ProcGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect))); ret == 0 {
		return 0, 0, fmt.Errorf("GetClientRect failed for HWND: %d", hwnd)
	}

	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid client rect for HWND %d: %dx%d", hwnd, width, height)
	}
	return width, height, nil
}

func setDPIAwareWin32() func() {
	if err := win32ProcSetThreadDpiAwarenessCtx.Find(); err != nil {
		return func() {}
	}
	oldCtx, _, _ := win32ProcSetThreadDpiAwarenessCtx.Call(win32DPIAwarenessContextPerMonitorAwareV2)
	return func() {
		if oldCtx != 0 {
			win32ProcSetThreadDpiAwarenessCtx.Call(oldCtx)
		}
	}
}

func controllerHwndWin32(controller *maa.Controller) (uintptr, error) {
	if controller == nil {
		return 0, fmt.Errorf("nil controller")
	}

	infoStr, err := controller.GetInfo()
	if err != nil {
		return 0, fmt.Errorf("failed to get controller info: %w", err)
	}
	if strings.TrimSpace(infoStr) == "" {
		return 0, fmt.Errorf("empty controller info")
	}

	var info win32ControllerInfo
	if err := json.Unmarshal([]byte(infoStr), &info); err != nil {
		return 0, fmt.Errorf("failed to parse controller info: %w", err)
	}
	if info.Type != "" && !strings.EqualFold(info.Type, control.CONTROL_TYPE_WIN32) {
		return 0, fmt.Errorf("controller type is %q, not win32", info.Type)
	}
	if info.HWnd == 0 {
		return 0, fmt.Errorf("controller info has no hwnd")
	}
	return uintptr(info.HWnd), nil
}
