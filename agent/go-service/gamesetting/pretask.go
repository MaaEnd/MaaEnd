package gamesetting

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	// endfieldProcessName 是被监测的游戏主进程名。Recover 期间一旦检测到该进程，
	// 立即中止，避免覆盖运行中的游戏所持有的设置。
	endfieldProcessName = "Endfield.exe"

	settingDataDir  = "data"
	settingSubDir   = "GameSetting"
	settingFileName = "Setting.json"
)

// snapshot 是 Setting.json 的根结构：键为注册表值前缀（与 gamesetting_windows.go
// 中的 valuePrefix* 常量对应），值为对应 DWORD。
type snapshot struct {
	Values map[string]uint32 `json:"values"`
}

// Save 调用每个注册表项的 Get*，把读取到的值汇总到
// <exe-parent>/data/GameSetting/Setting.json。
// 单项读取失败仅记录 warn 并跳过；只要至少读到一项就继续。文件写失败返回 false。
func Save() bool {
	values := make(map[string]uint32, 21)
	save := func(key string, get func() (uint32, error)) {
		v, err := get()
		if err != nil {
			log.Warn().
				Err(err).
				Str("component", "gamesetting").
				Str("key", key).
				Msg("skip: get failed")
			return
		}
		values[key] = v
	}

	save("Screenmanager Fullscreen mode", GetScreenmanagerFullscreenMode)
	save("Screenmanager Resolution Height", GetScreenmanagerResolutionHeight)
	save("Screenmanager Resolution Width", GetScreenmanagerResolutionWidth)
	save("Screenmanager Resolution Window Height", GetScreenmanagerResolutionWindowHeight)
	save("Screenmanager Resolution Window Width", GetScreenmanagerResolutionWindowWidth)
	save("Screenmanager Window Position X", GetScreenmanagerWindowPositionX)
	save("Screenmanager Window Position Y", GetScreenmanagerWindowPositionY)
	save("video_custom_quality", GetVideoCustomQuality)
	save("video_frame_rate_8", GetVideoFrameRate8)
	save("video_full_screen", GetVideoFullScreen)
	save("video_quality_anisoLevel_1", GetVideoQualityAnisoLevel1)
	save("video_quality_contactshadow", GetVideoQualityContactShadow)
	save("video_quality_dlss_mode_1", GetVideoQualityDLSSMode1)
	save("video_quality_main", GetVideoQualityMain)
	save("video_quality_reflex", GetVideoQualityReflex)
	save("video_quality_sharpness", GetVideoQualitySharpness)
	save("video_quality_upscaler", GetVideoQualityUpscaler)
	save("video_resolution", GetVideoResolution)
	save("video_resolution_height", GetVideoResolutionHeight)
	save("video_resolution_width", GetVideoResolutionWidth)
	save("video_texture_quality_1", GetVideoTextureQuality1)

	if len(values) == 0 {
		log.Error().
			Str("component", "gamesetting").
			Msg("no values read from registry")
		return false
	}

	path, err := resolveSettingPath()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Msg("failed to resolve setting path")
		return false
	}
	if err := writeSnapshot(path, &snapshot{Values: values}); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("path", path).
			Msg("failed to write setting file")
		return false
	}

	log.Info().
		Str("component", "gamesetting").
		Str("path", path).
		Int("count", len(values)).
		Msg("settings saved")
	return true
}

// Recover 读取 Save 写入的 Setting.json，再逐项调用 Set* 把值写回注册表。
// 若检测到 Endfield.exe 运行、文件缺失/损坏、或任一 Set* 失败，返回 false。
// JSON 中未出现的键被静默跳过，不视为失败。
func Recover() bool {
	if isEndfieldRunning() {
		log.Warn().
			Str("component", "gamesetting").
			Str("process", endfieldProcessName).
			Msg("cannot recover settings: game is running")
		return false
	}

	path, err := resolveSettingPath()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Msg("failed to resolve setting path")
		return false
	}
	s, err := readSnapshot(path)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("path", path).
			Msg("failed to read setting file")
		return false
	}

	ok := true
	applied := 0
	restore := func(key string, set func(uint32) error) {
		v, exists := s.Values[key]
		if !exists {
			return
		}
		if err := set(v); err != nil {
			log.Error().
				Err(err).
				Str("component", "gamesetting").
				Str("key", key).
				Msg("failed to set value")
			ok = false
			return
		}
		applied++
	}

	restore("Screenmanager Fullscreen mode", SetScreenmanagerFullscreenMode)
	restore("Screenmanager Resolution Height", SetScreenmanagerResolutionHeight)
	restore("Screenmanager Resolution Width", SetScreenmanagerResolutionWidth)
	restore("Screenmanager Resolution Window Height", SetScreenmanagerResolutionWindowHeight)
	restore("Screenmanager Resolution Window Width", SetScreenmanagerResolutionWindowWidth)
	restore("Screenmanager Window Position X", SetScreenmanagerWindowPositionX)
	restore("Screenmanager Window Position Y", SetScreenmanagerWindowPositionY)
	restore("video_custom_quality", SetVideoCustomQuality)
	restore("video_frame_rate_8", SetVideoFrameRate8)
	restore("video_full_screen", SetVideoFullScreen)
	restore("video_quality_anisoLevel_1", SetVideoQualityAnisoLevel1)
	restore("video_quality_contactshadow", SetVideoQualityContactShadow)
	restore("video_quality_dlss_mode_1", SetVideoQualityDLSSMode1)
	restore("video_quality_main", SetVideoQualityMain)
	restore("video_quality_reflex", SetVideoQualityReflex)
	restore("video_quality_sharpness", SetVideoQualitySharpness)
	restore("video_quality_upscaler", SetVideoQualityUpscaler)
	restore("video_resolution", SetVideoResolution)
	restore("video_resolution_height", SetVideoResolutionHeight)
	restore("video_resolution_width", SetVideoResolutionWidth)
	restore("video_texture_quality_1", SetVideoTextureQuality1)

	if !ok {
		return false
	}

	log.Info().
		Str("component", "gamesetting").
		Str("path", path).
		Int("count", applied).
		Msg("settings recovered")
	return true
}

// resolveSettingPath 计算 Setting.json 的绝对路径：当前 exe 所在目录的上一级 / data / GameSetting / Setting.json。
func resolveSettingPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	parent := filepath.Dir(filepath.Dir(exe))
	return filepath.Join(parent, settingDataDir, settingSubDir, settingFileName), nil
}

func writeSnapshot(path string, s *snapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func readSnapshot(path string) (*snapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s snapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if len(s.Values) == 0 {
		return nil, errors.New("gamesetting: snapshot contains no values")
	}
	return &s, nil
}

// isEndfieldRunning 进行进程名等值匹配（大小写不敏感）。
// 若进程枚举失败，出于数据安全考虑视为"正在运行"，阻止 Recover。
func isEndfieldRunning() bool {
	procs, err := process.Processes()
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", "gamesetting").
			Msg("failed to enumerate processes, treating as game running")
		return true
	}

	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}
		if strings.EqualFold(name, endfieldProcessName) {
			return true
		}
	}
	return false
}
