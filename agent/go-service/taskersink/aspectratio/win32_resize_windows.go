//go:build windows

package aspectratio

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                           = windows.NewLazySystemDLL("user32.dll")
	procGetWindowRect                = user32.NewProc("GetWindowRect")
	procAdjustWindowRectEx           = user32.NewProc("AdjustWindowRectEx")
	procAdjustWindowRectExForDpi     = user32.NewProc("AdjustWindowRectExForDpi")
	procGetWindowLongW               = user32.NewProc("GetWindowLongW")
	procGetWindowLongPtrW            = user32.NewProc("GetWindowLongPtrW")
	procSetWindowPos                 = user32.NewProc("SetWindowPos")
	procGetDpiForWindow              = user32.NewProc("GetDpiForWindow")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procPostMessageW                 = user32.NewProc("PostMessageW")
)

// GetWindowLong / GetWindowLongPtr index constants.
const (
	GWL_STYLE   = -16
	GWL_EXSTYLE = -20
)

// SetWindowPos flags.
const (
	SWP_NOZORDER   = 0x0004
	SWP_NOACTIVATE = 0x0010
)

// WM_SYS* messages used for synthesizing Alt+Enter at the HWND level.
const (
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
)

// Pre-computed lParam values for an Alt+Enter dispatch where the system-key
// message itself carries the modifier context (bit 29 = "ALT held"). The game
// only inspects the lParam of the WM_SYSKEYDOWN/UP it receives; it does not
// query GetKeyState(VK_MENU). Sending separate VK_MENU presses tends to drag
// the target into menu-activation mode and swallow the subsequent Enter, so
// we skip them entirely.
//
//	wmSysKeyAltEnterDown lParam decoded:
//	  bits 0..15   repeat count   = 0
//	  bits 16..23  scan code      = 0  (game ignores it for this purpose)
//	  bit  29      alt context    = 1
//	wmSysKeyAltEnterUp lParam decoded:
//	  bits 0..15   repeat count   = 1
//	  bit  29      alt context    = 1
//	  bit  30      prev key state = 1 (was down)
//	  bit  31      transition     = 1 (released)
const (
	wmSysKeyAltEnterDown = 0x20000000
	wmSysKeyAltEnterUp   = 0xE0000001
)

// VK_RETURN — the only virtual-key code we synthesize for the fullscreen
// toggle. VK_MENU is intentionally not sent (see lParam comment above).
const vkReturn = 0x0D

// DPI_AWARENESS_CONTEXT pseudo-handle values, matching Windows SDK macros.
const (
	dpiAwarenessContextPerMonitorAwareV2 = ^uintptr(0) - 3 // (HANDLE)-4
)

// RECT mirrors the Windows SDK RECT structure.
type RECT struct {
	Left, Top, Right, Bottom int32
}

// ResizeClientArea resizes the given window's client area to
// (targetClientW, targetClientH) while preserving its top-left position.
//
// Returns the original outer-frame x/y/width/height for later restoration
// via RestoreWindowRect. All coordinates are physical pixels — this routine
// switches the thread to PER_MONITOR_AWARE_V2 DPI context so SetWindowPos
// is not silently scaled by Windows on high-DPI displays.
func ResizeClientArea(hwnd uintptr, targetClientW, targetClientH int32) (int32, int32, int32, int32, error) {
	if hwnd == 0 {
		return 0, 0, 0, 0, fmt.Errorf("invalid HWND")
	}
	if err := ensureResizeAPIs(); err != nil {
		return 0, 0, 0, 0, err
	}

	restore := setThreadDpiAware()
	defer restore()

	var origRect RECT
	if ret, _, e := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&origRect))); ret == 0 {
		return 0, 0, 0, 0, fmt.Errorf("GetWindowRect failed: %w", e)
	}
	origX := origRect.Left
	origY := origRect.Top
	origW := origRect.Right - origRect.Left
	origH := origRect.Bottom - origRect.Top

	style := getWindowLong(hwnd, GWL_STYLE)
	exStyle := getWindowLong(hwnd, GWL_EXSTYLE)

	// Convert desired client size to outer frame size, accounting for the
	// window's actual DPI. AdjustWindowRectEx assumes 96 DPI and produces
	// undersized frames on high-DPI displays — use the *ForDpi variant when
	// available (Windows 10 1607+).
	target := RECT{Left: 0, Top: 0, Right: targetClientW, Bottom: targetClientH}
	const hasMenu = uintptr(0) // game windows generally have no menu bar
	dpi := getWindowDpi(hwnd)
	if err := adjustClientRectToOuter(&target, uint32(style), hasMenu, uint32(exStyle), dpi); err != nil {
		return 0, 0, 0, 0, err
	}
	outerW := target.Right - target.Left
	outerH := target.Bottom - target.Top

	ret, _, e := procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(origX),
		uintptr(origY),
		uintptr(outerW),
		uintptr(outerH),
		uintptr(SWP_NOZORDER|SWP_NOACTIVATE),
	)
	if ret == 0 {
		return 0, 0, 0, 0, fmt.Errorf("SetWindowPos failed: %w", e)
	}

	return origX, origY, origW, origH, nil
}

// RestoreWindowRect moves the window back to its original outer rect.
// Coordinates are interpreted as physical pixels (PER_MONITOR_AWARE_V2).
func RestoreWindowRect(hwnd uintptr, x, y, w, h int32) error {
	if hwnd == 0 {
		return fmt.Errorf("invalid HWND")
	}
	if err := procSetWindowPos.Find(); err != nil {
		return err
	}
	restore := setThreadDpiAware()
	defer restore()

	ret, _, e := procSetWindowPos.Call(
		hwnd,
		0,
		uintptr(x),
		uintptr(y),
		uintptr(w),
		uintptr(h),
		uintptr(SWP_NOZORDER|SWP_NOACTIVATE),
	)
	if ret == 0 {
		return fmt.Errorf("SetWindowPos restore failed: %w", e)
	}
	return nil
}

// SendAltEnter posts a minimal WM_SYSKEYDOWN/UP(VK_RETURN) pair to the given
// window, with the lParam bit-29 (Alt context) set on both messages. This
// triggers the game's built-in fullscreen toggle in both foreground and
// background controller modes — see the lParam constants above.
//
// We deliberately do NOT send any VK_MENU messages. Most game/engine input
// handlers detect Alt+Enter purely from the WM_SYSKEYDOWN(VK_RETURN) message
// with bit-29 set; sending a separate VK_MENU down/up can put the window into
// menu-activation mode and swallow the Enter.
func SendAltEnter(hwnd uintptr) error {
	if hwnd == 0 {
		return fmt.Errorf("invalid HWND")
	}
	if err := procPostMessageW.Find(); err != nil {
		return fmt.Errorf("PostMessageW unavailable: %w", err)
	}
	if ret, _, e := procPostMessageW.Call(hwnd, wmSysKeyDown, vkReturn, wmSysKeyAltEnterDown); ret == 0 {
		return fmt.Errorf("PostMessage SYSKEYDOWN failed: %w", e)
	}
	time.Sleep(50 * time.Millisecond)
	if ret, _, e := procPostMessageW.Call(hwnd, wmSysKeyUp, vkReturn, wmSysKeyAltEnterUp); ret == 0 {
		return fmt.Errorf("PostMessage SYSKEYUP failed: %w", e)
	}
	return nil
}

// ensureResizeAPIs verifies all user32 procs required for resize are present.
// All of these are core APIs that have existed since Windows 2000+, but the
// upfront check mirrors hdrcheck/hdr_windows.go and produces clearer errors
// than a late call-site failure.
func ensureResizeAPIs() error {
	for _, p := range []*windows.LazyProc{
		procGetWindowRect,
		procAdjustWindowRectEx,
		procSetWindowPos,
	} {
		if err := p.Find(); err != nil {
			return fmt.Errorf("user32 API unavailable: %w", err)
		}
	}
	return nil
}

// setThreadDpiAware switches this thread to PER_MONITOR_AWARE_V2 so that
// SetWindowPos coordinates are interpreted as physical pixels regardless of
// per-monitor DPI scaling. Returns a function to restore the previous context.
// Mirrors what MaaFramework's MessageInput does before any SetWindowPos call.
//
// On Windows < 10 1607 the proc is unavailable; in that case we leave the
// thread DPI context unchanged. The resize will then be subject to whatever
// awareness the process has, which on modern Windows defaults to system DPI.
func setThreadDpiAware() func() {
	if err := procSetThreadDpiAwarenessContext.Find(); err != nil {
		return func() {}
	}
	prev, _, _ := procSetThreadDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorAwareV2)
	return func() {
		if prev != 0 {
			procSetThreadDpiAwarenessContext.Call(prev)
		}
	}
}

// getWindowDpi returns the DPI of the monitor hosting the given window, or 96
// (the legacy default) when GetDpiForWindow is unavailable.
func getWindowDpi(hwnd uintptr) uint32 {
	if err := procGetDpiForWindow.Find(); err != nil {
		return 96
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 96
	}
	return uint32(dpi)
}

// adjustClientRectToOuter expands the given client-area rect to the
// corresponding outer-frame rect, using AdjustWindowRectExForDpi when
// available and falling back to AdjustWindowRectEx otherwise.
func adjustClientRectToOuter(r *RECT, style uint32, hasMenu uintptr, exStyle uint32, dpi uint32) error {
	if procAdjustWindowRectExForDpi.Find() == nil {
		ret, _, e := procAdjustWindowRectExForDpi.Call(
			uintptr(unsafe.Pointer(r)),
			uintptr(style),
			hasMenu,
			uintptr(exStyle),
			uintptr(dpi),
		)
		if ret == 0 {
			return fmt.Errorf("AdjustWindowRectExForDpi failed: %w", e)
		}
		return nil
	}
	ret, _, e := procAdjustWindowRectEx.Call(
		uintptr(unsafe.Pointer(r)),
		uintptr(style),
		hasMenu,
		uintptr(exStyle),
	)
	if ret == 0 {
		return fmt.Errorf("AdjustWindowRectEx failed: %w", e)
	}
	return nil
}

// getWindowLong reads a window's style/ex-style. Prefers GetWindowLongPtrW
// (only exists as a separate export on 64-bit Windows; on 32-bit it's a macro
// alias for GetWindowLongW), and falls back to the narrow GetWindowLongW.
func getWindowLong(hwnd uintptr, index int32) int64 {
	if procGetWindowLongPtrW.Find() == nil {
		ret, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(index))
		return int64(ret)
	}
	ret, _, _ := procGetWindowLongW.Call(hwnd, uintptr(index))
	return int64(int32(ret))
}
