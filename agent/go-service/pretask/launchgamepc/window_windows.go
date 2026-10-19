//go:build windows

package launchgamepc

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	monitorDefaultToNearest = 0x00000002
	monitorDefaultToPrimary = 0x00000001
	monitorInfoFlagPrimary  = 0x00000001
	swpNoSize               = 0x0001
	swpNoZOrder             = 0x0004
	swpNoActivate           = 0x0010
)

var (
	user32                = windows.NewLazySystemDLL("user32.dll")
	procMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	procMonitorFromPoint  = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
)

type winRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor winRect
	RcWork    winRect
	DwFlags   uint32
}

func getMonitorInfo(monitor uintptr) (monitorInfo, error) {
	info := monitorInfo{CbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ret, _, err := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ret == 0 {
		return info, fmt.Errorf("GetMonitorInfoW failed: %w", err)
	}
	return info, nil
}

// moveLauncherToPrimaryMonitor 将不在主屏上的启动器窗口移到主屏工作区左上角。
// 启动器固定按系统 DPI（登录时主屏缩放）渲染，位于缩放不同的副屏上时由 DWM 缩放显示，
// 而 PrintWindow 绕过 DWM，截图会与窗口尺寸不匹配，导致识别失败。
func moveLauncherToPrimaryMonitor(hWnd unsafe.Pointer) (bool, error) {
	monitor, _, _ := procMonitorFromWindow.Call(uintptr(hWnd), monitorDefaultToNearest)
	if monitor == 0 {
		return false, fmt.Errorf("MonitorFromWindow failed")
	}
	info, err := getMonitorInfo(monitor)
	if err != nil {
		return false, err
	}
	if info.DwFlags&monitorInfoFlagPrimary != 0 {
		return false, nil
	}

	// POINT{0, 0} 按值传递，在 x64 上打包为单个 0
	primary, _, _ := procMonitorFromPoint.Call(0, monitorDefaultToPrimary)
	if primary == 0 {
		return false, fmt.Errorf("MonitorFromPoint failed")
	}
	primaryInfo, err := getMonitorInfo(primary)
	if err != nil {
		return false, err
	}

	ret, _, err := procSetWindowPos.Call(
		uintptr(hWnd),
		0,
		uintptr(primaryInfo.RcWork.Left),
		uintptr(primaryInfo.RcWork.Top),
		0,
		0,
		swpNoSize|swpNoZOrder|swpNoActivate,
	)
	if ret == 0 {
		return false, fmt.Errorf("SetWindowPos failed: %w", err)
	}
	return true, nil
}
