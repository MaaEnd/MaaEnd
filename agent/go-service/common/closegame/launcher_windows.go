package closegame

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

const webEngineProcessName = "QtWebEngineProcess.exe"
const closeProcessAccess = windows.PROCESS_QUERY_LIMITED_INFORMATION | windows.PROCESS_TERMINATE | windows.SYNCHRONIZE

func closeLaunchers() bool {
	entries, err := launcherProcessSnapshot()
	if err != nil {
		log.Error().Err(err).Msg("CloseGameAction: failed to enumerate launchers")
		return false
	}

	ok := true
	for _, name := range launcherProcessNames {
		for _, entry := range entries {
			if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
				continue
			}
			if err := closeLauncher(entry.ProcessID, name); err != nil {
				log.Error().Err(err).Str("process", name).Uint32("pid", entry.ProcessID).Msg("CloseGameAction: failed to close launcher")
				ok = false
			}
		}
	}
	return ok
}

func closeLauncher(pid uint32, name string) error {
	handle, err := windows.OpenProcess(closeProcessAccess, false, pid)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil // 枚举后已退出。
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	image, err := processImagePath(handle)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Base(image), name) {
		return nil // PID 已被其他进程复用。
	}
	if err := terminateProcess(handle); err != nil {
		return err
	}
	if status, err := windows.WaitForSingleObject(handle, 5000); err != nil {
		return err
	} else if status != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("waiting for launcher exit returned %#x", status)
	}
	log.Info().Str("process", name).Uint32("pid", pid).Msg("CloseGameAction: process killed")

	var started, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &started, &exited, &kernel, &user); err != nil {
		return err
	}
	// 等待主进程退出后再枚举，覆盖关闭期间启动的子进程。
	// 保留主进程句柄，防止清理时复用它的 PID。
	entries, err := launcherProcessSnapshot()
	if err != nil {
		return err
	}
	helperPath := filepath.Join(filepath.Dir(image), webEngineProcessName)
	var cleanupErr error
	for _, entry := range entries {
		if entry.ParentProcessID != pid || !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), webEngineProcessName) {
			continue
		}
		if err := closeWebEngineProcess(entry.ProcessID, helperPath, started, exited); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("close WebEngine child %d: %w", entry.ProcessID, err))
		}
	}
	return cleanupErr
}

func closeWebEngineProcess(pid uint32, expectedPath string, parentStart, parentExit windows.Filetime) error {
	handle, err := windows.OpenProcess(closeProcessAccess, false, pid)
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return nil
	}
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)

	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return err
	}
	if created.Nanoseconds() < parentStart.Nanoseconds() || created.Nanoseconds() > parentExit.Nanoseconds() {
		return nil
	}
	image, err := processImagePath(handle)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(image), expectedPath) {
		return nil
	}
	if err := terminateProcess(handle); err != nil {
		return err
	}
	log.Info().Str("process", webEngineProcessName).Uint32("pid", pid).Msg("CloseGameAction: WebEngine child terminated")
	return nil
}

func terminateProcess(handle windows.Handle) error {
	err := windows.TerminateProcess(handle, 1)
	if err == nil {
		return nil
	}
	if status, waitErr := windows.WaitForSingleObject(handle, 0); waitErr == nil && status == windows.WAIT_OBJECT_0 {
		return nil // 进程可能自行退出。
	}
	return err
}

func launcherProcessSnapshot() ([]windows.ProcessEntry32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	var entries []windows.ProcessEntry32
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err := windows.Process32First(snapshot, &entry); ; err = windows.Process32Next(snapshot, &entry) {
		if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
}

func processImagePath(handle windows.Handle) (string, error) {
	var buf [32768]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}
