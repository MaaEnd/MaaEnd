//go:build windows

package rtsscheck

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	endfieldProfileName = "Endfield.exe"
	rtssRegPathWow64    = `Software\WOW6432Node\Unwinder\RTSS`
	rtssRegPath         = `Software\Unwinder\RTSS`
	rtssHooksDLLName    = "RTSSHooks64.dll"
)

// isEnableOSDEnabled reads the effective RTSS profile EnableOSD for Endfield.exe.
// Missing RTSS / load failure returns enabled=false with a non-nil error (caller fail-opens).
func isEnableOSDEnabled() (enabled bool, profile string, err error) {
	installDir, err := readRTSSInstallDir()
	if err != nil {
		// RTSS not installed — treat as OSD disabled.
		return false, "", nil
	}

	dllPath := filepath.Join(installDir, rtssHooksDLLName)
	dll, err := windows.LoadDLL(dllPath)
	if err != nil {
		return false, "", fmt.Errorf("load %s: %w", dllPath, err)
	}
	defer dll.Release()

	procEnum, err := dll.FindProc("EnumProfiles")
	if err != nil {
		return false, "", fmt.Errorf("FindProc EnumProfiles: %w", err)
	}
	procLoad, err := dll.FindProc("LoadProfile")
	if err != nil {
		return false, "", fmt.Errorf("FindProc LoadProfile: %w", err)
	}
	procGet, err := dll.FindProc("GetProfileProperty")
	if err != nil {
		return false, "", fmt.Errorf("FindProc GetProfileProperty: %w", err)
	}

	profiles, err := enumProfiles(procEnum)
	if err != nil {
		return false, "", err
	}

	profile = ""
	profileLabel := "Global"
	if hasProfile(profiles, endfieldProfileName) {
		profile = endfieldProfileName
		profileLabel = endfieldProfileName
	}

	if err := loadProfile(procLoad, profile); err != nil {
		return false, profileLabel, err
	}

	enableOSD, err := getEnableOSD(procGet)
	if err != nil {
		return false, profileLabel, err
	}
	return enableOSD == 1, profileLabel, nil
}

func readRTSSInstallDir() (string, error) {
	for _, path := range []string{rtssRegPathWow64, rtssRegPath} {
		dir, err := readInstallDirFromKey(path)
		if err == nil && dir != "" {
			return dir, nil
		}
	}
	return "", fmt.Errorf("RTSS InstallDir not found in registry")
}

func readInstallDirFromKey(path string) (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()

	dir, _, err := k.GetStringValue("InstallDir")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(dir, `/\`), nil
}

func enumProfiles(proc *windows.Proc) ([]string, error) {
	const bufSize = 65536
	buf := make([]byte, bufSize)
	r1, _, callErr := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(bufSize))
	needed := uint32(r1)
	if needed == 0 {
		if callErr != windows.ERROR_SUCCESS {
			return nil, fmt.Errorf("EnumProfiles: %w", callErr)
		}
		return nil, nil
	}
	if needed > bufSize {
		buf = make([]byte, needed)
		r1, _, callErr = proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(needed))
		needed = uint32(r1)
		if needed == 0 && callErr != windows.ERROR_SUCCESS {
			return nil, fmt.Errorf("EnumProfiles: %w", callErr)
		}
	}

	n := int(needed)
	if n <= 0 {
		return nil, nil
	}
	if n > len(buf) {
		n = len(buf)
	}
	// EnumProfiles returns required size including trailing NUL; trim it.
	for n > 0 && buf[n-1] == 0 {
		n--
	}
	list := string(buf[:n])
	if list == "" {
		return nil, nil
	}
	parts := strings.Split(list, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

func hasProfile(profiles []string, name string) bool {
	for _, p := range profiles {
		if strings.EqualFold(p, name) {
			return true
		}
	}
	return false
}

func loadProfile(proc *windows.Proc, profile string) error {
	ptr, err := windows.BytePtrFromString(profile)
	if err != nil {
		return err
	}
	_, _, callErr := proc.Call(uintptr(unsafe.Pointer(ptr)))
	if callErr != windows.ERROR_SUCCESS {
		// void API; non-success last-error is common and ignored unless Call itself failed badly.
		_ = callErr
	}
	return nil
}

func getEnableOSD(proc *windows.Proc) (uint32, error) {
	name, err := windows.BytePtrFromString("EnableOSD")
	if err != nil {
		return 0, err
	}
	var value uint32
	r1, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&value)),
		uintptr(unsafe.Sizeof(value)),
	)
	if r1 == 0 {
		if callErr != windows.ERROR_SUCCESS {
			return 0, fmt.Errorf("GetProfileProperty EnableOSD: %w", callErr)
		}
		return 0, fmt.Errorf("GetProfileProperty EnableOSD returned FALSE")
	}
	return value, nil
}
