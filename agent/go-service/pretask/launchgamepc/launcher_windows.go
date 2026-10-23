//go:build windows

package launchgamepc

import (
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const (
	wmNull          = 0x0000
	wmClose         = 0x0010
	smtoAbortIfHung = 0x0002

	th32csSnapProcess = 0x00000002
	processTerminate  = 0x0001
	processQueryInfo  = 0x1000
	synchronize       = 0x00100000
	invalidHandle     = ^uintptr(0)
)

var (
	launcherUser32   = windows.NewLazySystemDLL("user32.dll")
	launcherKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procSendMessageTimeoutW       = launcherUser32.NewProc("SendMessageTimeoutW")
	procEnumWindows               = launcherUser32.NewProc("EnumWindows")
	procGetWindowThreadProcessID  = launcherUser32.NewProc("GetWindowThreadProcessId")
	procPostMessageW              = launcherUser32.NewProc("PostMessageW")
	procCreateToolhelp32Snapshot  = launcherKernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW           = launcherKernel32.NewProc("Process32FirstW")
	procProcess32NextW            = launcherKernel32.NewProc("Process32NextW")
	procOpenProcess               = launcherKernel32.NewProc("OpenProcess")
	procCloseHandle               = launcherKernel32.NewProc("CloseHandle")
	procWaitForSingleObject       = launcherKernel32.NewProc("WaitForSingleObject")
	procTerminateProcess          = launcherKernel32.NewProc("TerminateProcess")
	procQueryFullProcessImageName = launcherKernel32.NewProc("QueryFullProcessImageNameW")
)

// processEntry32 对应 PROCESSENTRY32W；只读取进程名与 PID，不打开进程句柄。
type processEntry32 struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [260]uint16
}

type launcherProcess struct {
	pid  uint32
	name string
}

// 等待游戏窗口响应耗时连续多次不超过特定时长
func waitGameReady(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	streak := 0
	for time.Now().Before(deadline) {
		hWnd, ok := findWindow(gameClassRegex, gameWindowRegex)
		if ok && windowResponsive(hWnd) {
			streak++
			if streak >= readyStreak {
				log.Info().
					Str("component", component).
					Int("streak", streak).
					Dur("ping_threshold", readyPingMs).
					Msg("game window became responsive")
				return true
			}
		} else {
			streak = 0
		}
		time.Sleep(readyPollWait)
	}
	return false
}

// windowResponsive 向窗口发送 WM_NULL 并测量处理耗时，超时或耗时超过阈值均视为未就绪。
func windowResponsive(hWnd unsafe.Pointer) bool {
	var result uintptr
	start := time.Now()
	ret, _, _ := procSendMessageTimeoutW.Call(
		uintptr(hWnd),
		wmNull,
		0,
		0,
		smtoAbortIfHung,
		500,
		uintptr(unsafe.Pointer(&result)),
	)
	if ret == 0 {
		return false
	}
	return time.Since(start) <= readyPingMs
}

// WM_CLOSE关启动器进程，强制结束兜底
func closeLauncher(installDir string, launcherPID uint32) {
	if installDir == "" && launcherPID == 0 {
		log.Warn().
			Str("component", component).
			Msg("launcher target unknown, skip closing")
		return
	}

	targets := findLauncherProcesses(installDir, launcherPID)
	if len(targets) == 0 {
		log.Info().
			Str("component", component).
			Msg("no launcher process to close")
		return
	}

	for _, target := range targets {
		alive, err := processAlive(target.pid)
		if err != nil {
			log.Warn().
				Err(err).
				Str("component", component).
				Str("name", target.name).
				Uint32("pid", target.pid).
				Msg("failed to query launcher process, skip closing")
			continue
		}
		if !alive {
			log.Debug().
				Str("component", component).
				Str("name", target.name).
				Uint32("pid", target.pid).
				Msg("launcher already exited, skip closing")
			continue
		}

		closed := requestWindowClose(target.pid)
		if waitProcessExit(target.pid, launcherCloseTimeout) {
			log.Info().
				Str("component", component).
				Str("name", target.name).
				Uint32("pid", target.pid).
				Int("wm_close", closed).
				Msg("launcher exited gracefully")
			continue
		}
		if terminateProcess(target.pid) {
			log.Warn().
				Str("component", component).
				Str("name", target.name).
				Uint32("pid", target.pid).
				Dur("timeout", launcherCloseTimeout).
				Msg("launcher did not exit in time, terminated")
			continue
		}
		log.Error().
			Str("component", component).
			Str("name", target.name).
			Uint32("pid", target.pid).
			Msg("failed to terminate launcher")
	}
}

func processAlive(pid uint32) (bool, error) {
	handle, _, callErr := procOpenProcess.Call(synchronize, 0, uintptr(pid))
	if handle == 0 {
		if callErr == windows.ERROR_INVALID_PARAMETER {
			return false, nil // 进程不存在
		}
		return false, callErr
	}
	defer procCloseHandle.Call(handle)

	ret, _, _ := procWaitForSingleObject.Call(handle, 0)
	return ret != 0, nil
}

func findLauncherProcesses(installDir string, launcherPID uint32) []launcherProcess {
	snapshot, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapProcess, 0)
	if snapshot == invalidHandle {
		log.Error().
			Str("component", component).
			Msg("failed to create process snapshot")
		return nil
	}
	defer procCloseHandle.Call(snapshot)

	prefix := ""
	if installDir != "" {
		prefix = strings.ToLower(filepath.Clean(installDir)) + string(filepath.Separator)
	}

	entry := processEntry32{Size: uint32(unsafe.Sizeof(processEntry32{}))}
	seen := make(map[uint32]struct{})
	var targets []launcherProcess

	ok, _, _ := procProcess32FirstW.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		name := windows.UTF16ToString(entry.ExeFile[:])
		pid := entry.ProcessID
		if pid != 0 && isLauncherProcessName(name) {
			_, duplicate := seen[pid]
			matchedPID := launcherPID != 0 && pid == launcherPID
			matchedDir := prefix != "" && processInDir(pid, prefix)
			if !duplicate && (matchedPID || matchedDir) {
				targets = append(targets, launcherProcess{pid: pid, name: name})
				seen[pid] = struct{}{}
			}
		}
		ok, _, _ = procProcess32NextW.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return targets
}

func isLauncherProcessName(name string) bool {
	return strings.EqualFold(name, launcherProcessName) || strings.EqualFold(name, launcherExeName)
}

func processInDir(pid uint32, prefix string) bool {
	handle, _, _ := procOpenProcess.Call(processQueryInfo, 0, uintptr(pid))
	if handle == 0 {
		return false
	}
	defer procCloseHandle.Call(handle)

	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	ret, _, _ := procQueryFullProcessImageName.Call(
		handle,
		0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if ret == 0 {
		return false
	}
	exe := strings.ToLower(filepath.Clean(windows.UTF16ToString(buf[:size])))
	return strings.HasPrefix(exe, prefix)
}

func requestWindowClose(pid uint32) int {
	count := 0
	callback := windows.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var owner uint32
		procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner == pid {
			procPostMessageW.Call(hwnd, wmClose, 0, 0)
			count++
		}
		return 1
	})
	procEnumWindows.Call(callback, 0)
	return count
}

func waitProcessExit(pid uint32, timeout time.Duration) bool {
	handle, _, callErr := procOpenProcess.Call(synchronize, 0, uintptr(pid))
	if handle == 0 {
		return callErr == windows.ERROR_INVALID_PARAMETER
	}
	defer procCloseHandle.Call(handle)

	ret, _, _ := procWaitForSingleObject.Call(handle, uintptr(timeout/time.Millisecond))
	return ret == 0 // WAIT_OBJECT_0
}

func terminateProcess(pid uint32) bool {
	handle, _, _ := procOpenProcess.Call(processTerminate, 0, uintptr(pid))
	if handle == 0 {
		return false
	}
	defer procCloseHandle.Call(handle)

	ret, _, _ := procTerminateProcess.Call(handle, 0)
	return ret != 0
}
