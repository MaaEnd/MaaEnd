package gamesetting

import (
	"encoding/json"
	"fmt"
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

	optionUnchanged = "Unchanged"
)

type gameSettingOptions struct {
	Region          string `json:"GameSettingRegion"`
	Language        string `json:"GameSettingLanguage"`
	DisplayType     string `json:"GameSettingDisplayType"`
	Resolution      string `json:"GameSettingResolution"`
	GraphicsQuality string `json:"GameSettingGraphicsQuality"`
	FrameRate       string `json:"GameSettingFrameRate"`
	AutoHDR         string `json:"GameSettingAutoHDR"`
}

// Run 对应 assets/tasks/pretasks/GameSetting.json 的 pretask 入口。
// Client 会把 option 取值序列化为 JSON 并追加为最后一个参数。
func Run(args []string) bool {
	opts, err := parseGameSettingOptions(args)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Strs("args", args).
			Msg("failed to parse GameSetting options")
		return false
	}

	if _, _, err := mapTextLanguage(opts.Language); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("language", opts.Language).
			Msg("invalid game language")
		return false
	}

	if isGameRunning() {
		log.Error().
			Str("component", "gamesetting").
			Msg("cannot apply game settings: game is running")
		return false
	}

	log.Info().
		Str("component", "gamesetting").
		Str("region", opts.Region).
		Str("language", opts.Language).
		Str("display_type", opts.DisplayType).
		Str("resolution", opts.Resolution).
		Str("graphics_quality", opts.GraphicsQuality).
		Str("frame_rate", opts.FrameRate).
		Str("auto_hdr", opts.AutoHDR).
		Msg("applying game settings")

	if !Apply(opts.Region, opts.DisplayType, opts.Resolution) {
		return false
	}

	// Apply 已按区服选定注册表路径，此处才能写入语言项。
	if err := ApplyTextLanguage(opts.Language); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("language", opts.Language).
			Msg("failed to set game language")
		return false
	}

	if quality, ok, err := mapGraphicsQuality(opts.GraphicsQuality); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("graphics_quality", opts.GraphicsQuality).
			Msg("invalid graphics quality")
		return false
	} else if ok {
		if err := SetVideoQualityMain(quality); err != nil {
			log.Error().
				Err(err).
				Str("component", "gamesetting").
				Uint32("graphics_quality", quality).
				Msg("failed to set graphics quality")
			return false
		}
		log.Info().
			Str("component", "gamesetting").
			Uint32("graphics_quality", quality).
			Msg("applied graphics quality")
	}

	if frameRate, ok, err := mapFrameRate(opts.FrameRate); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("frame_rate", opts.FrameRate).
			Msg("invalid frame rate")
		return false
	} else if ok {
		if err := SetVideoFrameRate8(frameRate); err != nil {
			log.Error().
				Err(err).
				Str("component", "gamesetting").
				Uint32("frame_rate", frameRate).
				Msg("failed to set frame rate")
			return false
		}
		log.Info().
			Str("component", "gamesetting").
			Uint32("frame_rate", frameRate).
			Msg("applied frame rate")
	}

	if err := ApplyAutoHDR(opts.AutoHDR); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("auto_hdr", opts.AutoHDR).
			Msg("failed to apply Auto HDR")
		return false
	}

	return true
}

func parseGameSettingOptions(args []string) (gameSettingOptions, error) {
	opts := gameSettingOptions{
		Region:          regionCN,
		Language:        optionUnchanged,
		DisplayType:     displayTypeWindow,
		Resolution:      defaultResolution,
		GraphicsQuality: optionUnchanged,
		FrameRate:       optionUnchanged,
		AutoHDR:         optionUnchanged,
	}
	if len(args) == 0 {
		return opts, nil
	}

	raw := strings.TrimSpace(args[len(args)-1])
	if !strings.HasPrefix(raw, "{") {
		return opts, nil
	}

	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return gameSettingOptions{}, err
	}
	if opts.Region == "" {
		opts.Region = regionCN
	}
	if opts.Language == "" {
		opts.Language = optionUnchanged
	}
	if opts.DisplayType == "" {
		opts.DisplayType = displayTypeWindow
	}
	if opts.Resolution == "" {
		opts.Resolution = defaultResolution
	}
	if opts.GraphicsQuality == "" {
		opts.GraphicsQuality = optionUnchanged
	}
	if opts.FrameRate == "" {
		opts.FrameRate = optionUnchanged
	}
	if opts.AutoHDR == "" {
		opts.AutoHDR = optionUnchanged
	}
	return opts, nil
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

// isGameRunning 检测 Endfield.exe 是否正在运行；进程枚举失败时视为正在运行。
func isGameRunning() bool {
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
