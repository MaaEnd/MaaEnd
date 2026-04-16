//go:build windows

package foregroundwindow

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	foregroundWindowUser32 = windows.NewLazySystemDLL("user32.dll")
	procFindWindowExW      = foregroundWindowUser32.NewProc("FindWindowExW")
	procShowWindow         = foregroundWindowUser32.NewProc("ShowWindow")
	procSetForegroundW     = foregroundWindowUser32.NewProc("SetForegroundWindow")
)

const swRestore = 9

// activateForegroundWindow 会尽力恢复并单次前置当前配置的游戏窗口。
// 为了保持实现最小化，这里有意忽略 classRegex 参数。
func activateForegroundWindow(title string, _ string) error {
	hwnd, err := findWindowByTitle(title)
	if err != nil {
		return err
	}

	procShowWindow.Call(uintptr(hwnd), swRestore)
	if ret, _, _ := procSetForegroundW.Call(uintptr(hwnd)); ret == 0 {
		return fmt.Errorf("SetForegroundWindow failed")
	}
	return nil
}

func findWindowByTitle(title string) (windows.HWND, error) {
	hwnd, _, _ := procFindWindowExW.Call(
		0,
		0,
		0,
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(title))),
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("window not found: %s", title)
	}
	return windows.HWND(hwnd), nil
}
