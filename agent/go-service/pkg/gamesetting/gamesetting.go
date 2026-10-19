package gamesetting

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	displayTypeWindow     = "Window"
	displayTypeFullscreen = "Fullscreen"
	defaultResolution     = "1280x720"
	endfieldProcessName   = "Endfield.exe"

	regionCN     = "CN"
	regionGlobal = "Global"

	sdkDLLCN       = "hgsdk.dll"
	sdkDLLCNPCGame = "PCGameSDK.dll" // Bilibili 服，仍归国服
	sdkDLLGlobal   = "gfsdk.dll"

	optionUnchanged = "Unchanged"
)

// configuredRegion 为 SetRegion 显式值，或 detectRegionFromProcess 成功后的缓存；空表示尚未判定。
var configuredRegion string

// Settings 为启动游戏前写入的设置，取值与 LaunchGamePC 选项的 case 名一致；空值按默认处理。
type Settings struct {
	Language        string
	DisplayType     string
	Resolution      string
	GraphicsQuality string
	FrameRate       string
	AutoHDR         string
}

func (s Settings) withDefaults() Settings {
	if s.Language == "" {
		s.Language = optionUnchanged
	}
	if s.DisplayType == "" {
		s.DisplayType = displayTypeWindow
	}
	if s.Resolution == "" {
		s.Resolution = defaultResolution
	}
	if s.GraphicsQuality == "" {
		s.GraphicsQuality = optionUnchanged
	}
	if s.FrameRate == "" {
		s.FrameRate = optionUnchanged
	}
	if s.AutoHDR == "" {
		s.AutoHDR = optionUnchanged
	}
	return s
}

// ApplyAll 按区服写入全部游戏设置，需在游戏未运行时调用。region 取 "CN" / "Global"。
func ApplyAll(region string, s Settings) error {
	s = s.withDefaults()

	if _, _, err := mapTextLanguage(s.Language); err != nil {
		return err
	}
	if err := SetRegion(region); err != nil {
		return err
	}

	log.Info().
		Str("component", "gamesetting").
		Str("region", region).
		Str("language", s.Language).
		Str("display_type", s.DisplayType).
		Str("resolution", s.Resolution).
		Str("graphics_quality", s.GraphicsQuality).
		Str("frame_rate", s.FrameRate).
		Str("auto_hdr", s.AutoHDR).
		Msg("applying game settings")

	if !Apply(s.DisplayType, s.Resolution) {
		return fmt.Errorf("gamesetting: failed to apply display type %q with resolution %q", s.DisplayType, s.Resolution)
	}

	// Apply 已按区服选定注册表路径，此处才能写入语言项。
	if err := ApplyTextLanguage(s.Language); err != nil {
		return err
	}

	quality, ok, err := mapGraphicsQuality(s.GraphicsQuality)
	if err != nil {
		return err
	}
	if ok {
		if err := SetVideoQualityMain(quality); err != nil {
			return err
		}
		log.Info().
			Str("component", "gamesetting").
			Uint32("graphics_quality", quality).
			Msg("applied graphics quality")
	}

	frameRate, ok, err := mapFrameRate(s.FrameRate)
	if err != nil {
		return err
	}
	if ok {
		if err := SetVideoFrameRate8(frameRate); err != nil {
			return err
		}
		log.Info().
			Str("component", "gamesetting").
			Uint32("frame_rate", frameRate).
			Msg("applied frame rate")
	}

	return ApplyAutoHDR(s.AutoHDR)
}

// ApplyTextLanguage 按选项写入游戏文本语言：Unchanged 或空值不修改，非法值返回错误。
// 供 CloseGamePC 等「游戏已关闭」的入口调用；调用前需先由 Apply 选定区服注册表路径。
func ApplyTextLanguage(name string) error {
	value, ok, err := mapTextLanguage(name)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if err := SetLanguageTextChange(value); err != nil {
		return err
	}
	log.Info().
		Str("component", "gamesetting").
		Str("language", strings.TrimSpace(name)).
		Uint32("language_value", value).
		Msg("applied game language")
	return nil
}

// mapTextLanguage 把游戏语言选项映射为 language_text_change 的 DWORD 值。
// 第二个返回值为 false 表示保持游戏当前语言，用于区分「不修改」与简体中文的数值 0。
func mapTextLanguage(name string) (uint32, bool, error) {
	switch strings.TrimSpace(name) {
	case "", optionUnchanged:
		return 0, false, nil
	case "CN":
		return 0, true, nil
	case "EN":
		return 1, true, nil
	case "JP":
		return 2, true, nil
	case "KR":
		return 3, true, nil
	case "TC":
		return 4, true, nil
	case "MX":
		return 5, true, nil
	case "BR":
		return 6, true, nil
	case "FR":
		return 7, true, nil
	case "DE":
		return 8, true, nil
	case "RU":
		return 9, true, nil
	case "IT":
		return 10, true, nil
	case "ID":
		return 11, true, nil
	case "TH":
		return 12, true, nil
	case "VN":
		return 13, true, nil
	default:
		return 0, false, fmt.Errorf("gamesetting: unknown game language %q", name)
	}
}

func mapGraphicsQuality(name string) (uint32, bool, error) {
	switch strings.TrimSpace(name) {
	case "", optionUnchanged:
		return 0, false, nil
	case "VeryLow":
		return 5, true, nil
	case "Low":
		return 4, true, nil
	case "Medium":
		return 3, true, nil
	case "High":
		return 2, true, nil
	case "Ultra":
		return 1, true, nil
	default:
		return 0, false, fmt.Errorf("gamesetting: unknown graphics quality %q", name)
	}
}

func mapFrameRate(name string) (uint32, bool, error) {
	switch strings.TrimSpace(name) {
	case "", optionUnchanged:
		return 0, false, nil
	case "Fps30":
		return 3000, true, nil
	case "Fps60":
		return 2000, true, nil
	case "Fps120":
		return 1000, true, nil
	default:
		return 0, false, fmt.Errorf("gamesetting: unknown frame rate %q", name)
	}
}

// SetRegion 显式设置区服。"CN" / "Global" 有效；"" 清空后改由进程 DLL 自动判区。
func SetRegion(region string) error {
	region = strings.TrimSpace(region)
	switch region {
	case "", regionCN, regionGlobal:
		configuredRegion = region
		return nil
	default:
		return fmt.Errorf("gamesetting: unknown region %q", region)
	}
}

// ResolveRegion 返回当前区服：优先已缓存/已设置的值；否则根据运行中的 Endfield.exe 目录 DLL 判定并缓存。
func ResolveRegion() (string, error) {
	if configuredRegion != "" {
		return configuredRegion, nil
	}
	region, err := detectRegionFromProcess()
	if err != nil {
		return "", err
	}
	configuredRegion = region
	return region, nil
}

// detectRegionFromProcess 查找 Endfield.exe，按其目录下 SDK DLL 判区：
// 有 hgsdk.dll（官服）或 PCGameSDK.dll（Bilibili 服）→ 国服；仅有 gfsdk.dll → 国际服。
func detectRegionFromProcess() (string, error) {
	procs, err := process.Processes()
	if err != nil {
		return "", fmt.Errorf("gamesetting: enumerate processes failed: %w", err)
	}

	var dirs []string
	seen := make(map[string]struct{})
	for _, p := range procs {
		name, err := p.Name()
		if err != nil || !strings.EqualFold(name, endfieldProcessName) {
			continue
		}
		exe, err := p.Exe()
		if err != nil || strings.TrimSpace(exe) == "" {
			continue
		}
		dir := filepath.Clean(filepath.Dir(exe))
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	if len(dirs) == 0 {
		return "", fmt.Errorf("gamesetting: Endfield.exe not running; cannot auto-detect region")
	}
	if len(dirs) > 1 {
		return "", fmt.Errorf("gamesetting: multiple Endfield.exe directories found, cannot auto-detect region: %v", dirs)
	}

	dir := dirs[0]
	hasHG, err := fileExists(filepath.Join(dir, sdkDLLCN))
	if err != nil {
		return "", err
	}
	hasPCGame, err := fileExists(filepath.Join(dir, sdkDLLCNPCGame))
	if err != nil {
		return "", err
	}
	hasCN := hasHG || hasPCGame
	hasGlobal, err := fileExists(filepath.Join(dir, sdkDLLGlobal))
	if err != nil {
		return "", err
	}
	switch {
	case hasCN && !hasGlobal:
		return regionCN, nil
	case hasGlobal && !hasCN:
		return regionGlobal, nil
	case hasCN && hasGlobal:
		return "", fmt.Errorf("gamesetting: both CN SDK (%s/%s) and %s exist under %s", sdkDLLCN, sdkDLLCNPCGame, sdkDLLGlobal, dir)
	default:
		return "", fmt.Errorf("gamesetting: neither CN SDK (%s/%s) nor %s found under %s", sdkDLLCN, sdkDLLCNPCGame, sdkDLLGlobal, dir)
	}
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("gamesetting: stat %q failed: %w", path, err)
}
