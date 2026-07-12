//go:build windows

package gfnwindow

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const (
	win32SwRestore               = 9
	win32SwpNoZOrder             = 0x0004
	win32SwpNoActivate           = 0x0010
	win32MonitorDefaultToNearest = 0x00000002

	win32DPIAwarenessContextPerMonitorAwareV2 = ^uintptr(3)

	// resizePollAttempts x resizePollInterval bounds how long we wait for the
	// window manager to apply the new size before declaring failure.
	resizePollAttempts = 20
	resizePollInterval = 50 * time.Millisecond
	restoreSettleDelay = 100 * time.Millisecond
)

var (
	win32User32                       = windows.NewLazySystemDLL("user32.dll")
	win32ProcIsWindow                 = win32User32.NewProc("IsWindow")
	win32ProcIsIconic                 = win32User32.NewProc("IsIconic")
	win32ProcIsZoomed                 = win32User32.NewProc("IsZoomed")
	win32ProcShowWindow               = win32User32.NewProc("ShowWindow")
	win32ProcGetClientRect            = win32User32.NewProc("GetClientRect")
	win32ProcGetWindowRect            = win32User32.NewProc("GetWindowRect")
	win32ProcSetWindowPos             = win32User32.NewProc("SetWindowPos")
	win32ProcMonitorFromWindow        = win32User32.NewProc("MonitorFromWindow")
	win32ProcGetMonitorInfoW          = win32User32.NewProc("GetMonitorInfoW")
	win32ProcSetThreadDpiAwarenessCtx = win32User32.NewProc("SetThreadDpiAwarenessContext")
)

type win32Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type win32MonitorInfo struct {
	CbSize    uint32
	RcMonitor win32Rect
	RcWork    win32Rect
	DwFlags   uint32
}

// ensureGFNWindowResolution resizes the GFN stream window so its client area
// matches the 1280x720 baseline, mirroring MaaNTE's resize_client_area with
// manage_title_bar disabled: the GFN CEF window is borderless, so WS_CAPTION
// is never forced and the frame extent is taken from the measured
// window/client rect delta instead of AdjustWindowRectEx.
func ensureGFNWindowResolution(controller *maa.Controller) (*resizeResult, error) {
	hwnd, err := control.GetWin32Hwnd(controller)
	if err != nil {
		return nil, err
	}
	if err := ensureResizeWin32APIs(); err != nil {
		return nil, err
	}
	if ret, _, _ := win32ProcIsWindow.Call(hwnd); ret == 0 {
		return nil, fmt.Errorf("invalid HWND: %d", hwnd)
	}

	// The DPI awareness context is per-thread; pin the goroutine so the
	// restore below acts on the same OS thread that measured the window.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restoreDPIContext := setDPIAwareWin32()
	defer restoreDPIContext()

	// A minimized or maximized window reports geometry that cannot be resized
	// in place; restore it first like MaaNTE does.
	if isIconicWin32(hwnd) || isZoomedWin32(hwnd) {
		log.Debug().
			Uint64("hwnd", uint64(hwnd)).
			Msg("GFN window is minimized or maximized, restoring before resize")
		win32ProcShowWindow.Call(hwnd, win32SwRestore)
		time.Sleep(restoreSettleDelay)
	}

	clientWidth, clientHeight, err := clientSizeWin32(hwnd)
	if err != nil {
		return &resizeResult{Reason: reasonClientSizeUnavailable}, nil
	}

	result := &resizeResult{
		BeforeWidth:  clientWidth,
		BeforeHeight: clientHeight,
		AfterWidth:   clientWidth,
		AfterHeight:  clientHeight,
	}

	if withinTolerance(clientWidth, clientHeight) {
		result.Reason = reasonAlreadyMatched
		return result, nil
	}

	var windowRect win32Rect
	if ret, _, _ := win32ProcGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&windowRect))); ret == 0 {
		return nil, fmt.Errorf("GetWindowRect failed for HWND: %d", hwnd)
	}

	// Frame extent from the measured delta: zero for the borderless GFN
	// stream window, non-zero if the client ever gains a frame.
	frameWidth := max(windowRect.Right-windowRect.Left-clientWidth, 0)
	frameHeight := max(windowRect.Bottom-windowRect.Top-clientHeight, 0)
	outerWidth := int32(targetWidth) + frameWidth
	outerHeight := int32(targetHeight) + frameHeight

	workArea, err := monitorWorkAreaWin32(hwnd)
	if err != nil {
		return nil, err
	}
	workWidth := workArea.Right - workArea.Left
	workHeight := workArea.Bottom - workArea.Top
	if workWidth < outerWidth || workHeight < outerHeight {
		log.Debug().
			Uint64("hwnd", uint64(hwnd)).
			Int32("work_width", workWidth).
			Int32("work_height", workHeight).
			Int32("outer_width", outerWidth).
			Int32("outer_height", outerHeight).
			Msg("Monitor work area is smaller than the resized GFN window")
		result.Reason = reasonWorkAreaTooSmall
		return result, nil
	}

	// Center within the work area so the resized window never ends up
	// partially off-screen.
	posX := workArea.Left + (workWidth-outerWidth)/2
	posY := workArea.Top + (workHeight-outerHeight)/2
	if ret, _, callErr := win32ProcSetWindowPos.Call(
		hwnd,
		0,
		uintptr(posX),
		uintptr(posY),
		uintptr(outerWidth),
		uintptr(outerHeight),
		win32SwpNoZOrder|win32SwpNoActivate,
	); ret == 0 {
		if callErr != nil && callErr != windows.ERROR_SUCCESS {
			return nil, fmt.Errorf("SetWindowPos failed for HWND %d: %w", hwnd, callErr)
		}
		return nil, fmt.Errorf("SetWindowPos failed for HWND %d with ret=0", hwnd)
	}

	// The window manager applies the resize asynchronously; poll until the
	// client area settles on the baseline.
	for range resizePollAttempts {
		afterWidth, afterHeight, err := clientSizeWin32(hwnd)
		if err == nil {
			result.AfterWidth = afterWidth
			result.AfterHeight = afterHeight
			if withinTolerance(afterWidth, afterHeight) {
				result.Reason = reasonResized
				return result, nil
			}
		}
		time.Sleep(resizePollInterval)
	}

	result.Reason = reasonResizeFailed
	return result, nil
}

func ensureResizeWin32APIs() error {
	for _, p := range []*windows.LazyProc{
		win32ProcIsWindow,
		win32ProcIsIconic,
		win32ProcIsZoomed,
		win32ProcShowWindow,
		win32ProcGetClientRect,
		win32ProcGetWindowRect,
		win32ProcSetWindowPos,
		win32ProcMonitorFromWindow,
		win32ProcGetMonitorInfoW,
	} {
		if err := p.Find(); err != nil {
			return fmt.Errorf("win32 API unavailable: %w", err)
		}
	}
	return nil
}

func withinTolerance(width, height int32) bool {
	return absInt32(width-targetWidth) <= sizeTolerance && absInt32(height-targetHeight) <= sizeTolerance
}

func clientSizeWin32(hwnd uintptr) (int32, int32, error) {
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

func monitorWorkAreaWin32(hwnd uintptr) (*win32Rect, error) {
	monitor, _, _ := win32ProcMonitorFromWindow.Call(hwnd, win32MonitorDefaultToNearest)
	if monitor == 0 {
		return nil, fmt.Errorf("MonitorFromWindow failed for HWND: %d", hwnd)
	}
	info := win32MonitorInfo{CbSize: uint32(unsafe.Sizeof(win32MonitorInfo{}))}
	if ret, _, _ := win32ProcGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ret == 0 {
		return nil, fmt.Errorf("GetMonitorInfoW failed for HWND: %d", hwnd)
	}
	return &info.RcWork, nil
}

func isIconicWin32(hwnd uintptr) bool {
	ret, _, _ := win32ProcIsIconic.Call(hwnd)
	return ret != 0
}

func isZoomedWin32(hwnd uintptr) bool {
	ret, _, _ := win32ProcIsZoomed.Call(hwnd)
	return ret != 0
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

func absInt32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
