package gamelaunch

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows"
)

func processIDs(name string) ([]uint32, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	var ids []uint32
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		if strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), name) {
			ids = append(ids, entry.ProcessID)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, fmt.Errorf("enumerate %s: %w", name, err)
	}
	return ids, nil
}

func officialParent() (windows.Handle, error) {
	ids, err := processIDs("Games.exe")
	if err != nil {
		return 0, err
	}
	var rejected error
	for _, pid := range ids {
		handle, err := windows.OpenProcess(windows.PROCESS_CREATE_PROCESS|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, pid)
		if err != nil {
			rejected = fmt.Errorf("open Games.exe PID %d: %w", pid, err)
			continue
		}
		path := make([]uint16, 32768)
		size := uint32(len(path))
		err = windows.QueryFullProcessImageName(handle, 0, &path[0], &size)
		image := windows.UTF16ToString(path[:size])
		var fingerprint string
		if err == nil && !strings.EqualFold(filepath.Base(image), "Games.exe") {
			err = fmt.Errorf("parent image changed: %s", image)
		}
		if err == nil {
			fingerprint, err = verifyLauncher(image)
		}
		if err == nil {
			var state uint32
			state, err = windows.WaitForSingleObject(handle, 0)
			if err == nil && state != uint32(windows.WAIT_TIMEOUT) {
				err = errors.New("official launcher exited during verification")
			}
		}
		if err == nil {
			log.Info().Uint32("parent_pid", pid).Str("parent_path", image).
				Str("signer_sha1", fingerprint).Msg("verified official game parent")
			return handle, nil
		}
		windows.CloseHandle(handle)
		rejected = fmt.Errorf("verify Games.exe PID %d: %w", pid, err)
	}
	return 0, rejected
}

// createGame 使用原生 STARTUPINFOEXW，既不经过 shell，也不向官方进程复制标准输出句柄。
func createGame(path string, parent windows.Handle) (windows.ProcessInformation, error) {
	var process windows.ProcessInformation
	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return process, err
	}
	command, err := windows.UTF16PtrFromString(windows.EscapeArg(path))
	if err != nil {
		return process, err
	}
	dir, err := windows.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return process, err
	}
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return process, err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_PARENT_PROCESS, unsafe.Pointer(&parent), unsafe.Sizeof(parent)); err != nil {
		return process, err
	}
	startup := windows.StartupInfoEx{
		StartupInfo:             windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{}))},
		ProcThreadAttributeList: attributes.List(),
	}
	err = windows.CreateProcess(app, command, nil, nil, false, windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		nil, dir, &startup.StartupInfo, &process)
	return process, err
}

type windowSearch struct {
	pid   uint32
	ready bool
}

var gameWindowCallback = syscall.NewCallback(func(hwnd windows.HWND, search *windowSearch) uintptr {
	var owner uint32
	if _, err := windows.GetWindowThreadProcessId(hwnd, &owner); err != nil || owner != search.pid || !windows.IsWindowVisible(hwnd) {
		return 1
	}
	var class [256]uint16
	if _, err := windows.GetClassName(hwnd, &class[0], int32(len(class))); err == nil && windows.UTF16ToString(class[:]) == "UnityWndClass" {
		search.ready = true
	}
	return 1
})

func gameWindowReady(pid uint32) (bool, error) {
	search := windowSearch{pid: pid}
	err := windows.EnumWindows(gameWindowCallback, unsafe.Pointer(&search))
	return search.ready, err
}
