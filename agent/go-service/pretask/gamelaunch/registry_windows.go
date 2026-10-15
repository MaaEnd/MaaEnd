package gamelaunch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	launcherUninstallID  = "33a0a6296a20400d503c59ac0fd6341e"
	launcherUninstallKey = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\` + launcherUninstallID
)

type registryLocation struct {
	root registry.Key
	view uint32
}

var launcherRegistryLocations = []registryLocation{
	{registry.CURRENT_USER, registry.WOW64_64KEY},
	{registry.CURRENT_USER, registry.WOW64_32KEY},
	{registry.LOCAL_MACHINE, registry.WOW64_64KEY},
	{registry.LOCAL_MACHINE, registry.WOW64_32KEY},
}

func launcherPathFromInstallLocation(location string) (string, error) {
	location = strings.TrimSpace(strings.Trim(location, `"`))
	if location == "" {
		return "", errors.New("empty launcher InstallLocation")
	}
	path := filepath.Clean(filepath.Join(location, "Launcher.exe"))
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("launcher path is a directory: %s", path)
	}
	return path, nil
}

// installedLauncher 通过官方安装器写入的卸载注册信息定位 Launcher.exe。
// 同时检查当前用户/本机与 32/64 位注册表视图，避免依赖安装盘符。
func installedLauncher() (string, error) {
	var lastErr error
	for _, location := range launcherRegistryLocations {
		key, err := registry.OpenKey(location.root, launcherUninstallKey, registry.QUERY_VALUE|location.view)
		if err != nil {
			lastErr = err
			continue
		}
		installLocation, _, valueErr := key.GetStringValue("InstallLocation")
		_ = key.Close()
		if valueErr != nil {
			lastErr = valueErr
			continue
		}
		path, pathErr := launcherPathFromInstallLocation(installLocation)
		if pathErr != nil {
			lastErr = pathErr
			continue
		}
		return path, nil
	}
	if lastErr != nil {
		return "", fmt.Errorf("official Hypergryph launcher installation registration not found: %w", lastErr)
	}
	return "", errors.New("official Hypergryph launcher installation registration not found")
}
