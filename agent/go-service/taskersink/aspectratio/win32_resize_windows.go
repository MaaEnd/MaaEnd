//go:build windows

package aspectratio

import (
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	procPostMessageW = user32.NewProc("PostMessageW")
)

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

const vkReturn = 0x0D

// SendAltEnter posts a minimal WM_SYSKEYDOWN/UP(VK_RETURN) pair to the given
// window, with the lParam bit-29 (Alt context) set on both messages. This
// triggers the game's built-in fullscreen toggle in both foreground and
// background controller modes.
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
