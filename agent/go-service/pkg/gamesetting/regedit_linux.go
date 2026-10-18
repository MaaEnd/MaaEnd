//go:build linux

package gamesetting

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

type regEntry struct {
	name string // fullname with hash
	typ  string
	val  string
}

// regValueCache 缓存 key -> (valueName with no hash suffix -> regEntry)
var regValueCache = map[string]map[string]regEntry{}

// regLineRe match "<值名_h><哈希>    <类型>    <值>"
var regLineRe = regexp.MustCompile(`^\s*(.+?_h)(\d+)\s{2,}(REG_\w+)\s+(.+?)\s*$`)

// runReg run $winbin reg args...
func runReg(args ...string) ([]string, error) {
	if winePrefix == "" {
		return nil, fmt.Errorf("gamesetting: wine prefix not set")
	}
	if wineBin == "" {
		return nil, fmt.Errorf("gamesetting: wine path not set, provide the wine binary of the prefix environment")
	}
	cmd := exec.Command(wineBin, append([]string{"reg"}, args...)...)
	cmd.Env = append(os.Environ(),
		"WINEPREFIX="+winePrefix,
		"WINEDLLOVERRIDES=winedbg.exe=d", // 禁用 winedbg，避免后台服务崩溃时蹦出一堆winedbg
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gamesetting: wine reg %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.Split(string(out), "\n"), nil
}

// queryValues 枚举整键，对应 Windows 端的 ReadValueNames
func queryValues(keyPath string) (map[string]regEntry, error) {
	if cached, ok := regValueCache[keyPath]; ok {
		return cached, nil
	}
	lines, err := runReg("query", `HKCU\`+keyPath)
	if err != nil {
		return nil, err
	}
	values := map[string]regEntry{}
	for _, line := range lines {
		m := regLineRe.FindStringSubmatch(line)
		if m != nil {
			// \1 值名前缀; \2 哈希; \3 类型; \4 值
			values[m[1]] = regEntry{name: m[1] + m[2], typ: m[3], val: m[4]}
		}
	}
	regValueCache[keyPath] = values
	return values, nil
}

func findValue(values map[string]regEntry, prefix, keyPath string) (regEntry, error) {
	if entry, ok := values[prefix]; ok {
		return entry, nil
	}
	return regEntry{}, fmt.Errorf("gamesetting: value %q not found under HKCU\\%s", prefix, keyPath)
}

func getDWord(prefix string) (uint32, error) {
	values, err := queryValues(registryPath)
	if err != nil {
		return 0, err
	}
	entry, err := findValue(values, prefix, registryPath)
	if err != nil {
		return 0, err
	}
	if entry.typ != "REG_DWORD" {
		return 0, fmt.Errorf("gamesetting: value %q is %s, want REG_DWORD", entry.name, entry.typ)
	}
	val, err := strconv.ParseUint(entry.val, 0, 32)
	if err != nil {
		return 0, fmt.Errorf("gamesetting: parse %q under HKCU\\%s: %w", entry.val, registryPath, err)
	}
	return uint32(val), nil
}

func setDWord(prefix string, value uint32) error {
	values, err := queryValues(registryPath)
	if err != nil {
		return err
	}
	entry, err := findValue(values, prefix, registryPath)
	if err != nil {
		return err
	}
	// wine reg add $key /v $vname /t REG_DWORD /d $value /f
	_, err = runReg("add", `HKCU\`+registryPath, "/v", entry.name, "/t", "REG_DWORD", "/d", strconv.FormatUint(uint64(value), 10), "/f")
	delete(regValueCache, registryPath)
	return err
}

// GetCachedUID 读 PLDK_cachedRoleId 区服由 ResolveRegion 决定
func GetCachedUID() (string, error) {
	region, err := ResolveRegion()
	if err != nil {
		return "", err
	}
	keyPath := registryPathCN
	if region == regionGlobal {
		keyPath = registryPathGlobal
	}

	values, err := queryValues(keyPath)
	if err != nil {
		return "", err
	}
	entry, err := findValue(values, valuePrefixPLDKCachedRoleId, keyPath)
	if err != nil {
		return "", err
	}
	if entry.typ != "REG_BINARY" {
		return "", fmt.Errorf("gamesetting: value %q is %s, want REG_BINARY", entry.name, entry.typ)
	}
	raw, err := hex.DecodeString(entry.val)
	if err != nil {
		return "", fmt.Errorf("gamesetting: decode %q under HKCU\\%s: %w", entry.val, keyPath, err)
	}
	uid := strings.TrimSpace(strings.TrimRight(string(raw), "\x00"))
	if err := validateCachedUID(uid); err != nil {
		return "", err
	}
	return uid, nil
}

// IsAutoHDREnabled 在非 Windows 平台始终视为未开启
func IsAutoHDREnabled() (bool, error) {
	return false, nil
}

// ApplyAutoHDR 校验 mode；Enable/Disable 在 wine 下无对应系统设置，跳过
func ApplyAutoHDR(mode string) error {
	switch strings.TrimSpace(mode) {
	case "", optionUnchanged:
		return nil
	case "Disable", "Enable":
		log.Info().
			Str("component", "gamesetting").
			Str("mode", mode).
			Msg("skip Auto HDR: not applicable on linux")
		return nil
	default:
		return fmt.Errorf("gamesetting: unknown Auto HDR mode %q", mode)
	}
}

// FindLauncherInstallDir 仅 Windows 实现；Linux 由用户在选项中提供启动器目录
func FindLauncherInstallDir(_ string) (string, error) {
	return "", fmt.Errorf("gamesetting: FindLauncherInstallDir is windows only")
}

// FindLauncherInstallDirByProtocol 仅 Windows 实现
func FindLauncherInstallDirByProtocol(_ string) (string, error) {
	return "", fmt.Errorf("gamesetting: FindLauncherInstallDirByProtocol is windows only")
}
