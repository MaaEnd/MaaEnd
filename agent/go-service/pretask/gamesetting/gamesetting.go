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
	DisplayType     string `json:"GameSettingDisplayType"`
	Resolution      string `json:"GameSettingResolution"`
	GraphicsQuality string `json:"GameSettingGraphicsQuality"`
	FrameRate       string `json:"GameSettingFrameRate"`
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

	if isGameRunning() {
		log.Error().
			Str("component", "gamesetting").
			Msg("cannot apply game settings: game is running")
		return false
	}

	log.Info().
		Str("component", "gamesetting").
		Str("region", opts.Region).
		Str("display_type", opts.DisplayType).
		Str("resolution", opts.Resolution).
		Str("graphics_quality", opts.GraphicsQuality).
		Str("frame_rate", opts.FrameRate).
		Msg("applying game settings")

	if !Apply(opts.Region, opts.DisplayType, opts.Resolution) {
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

	return true
}

func parseGameSettingOptions(args []string) (gameSettingOptions, error) {
	opts := gameSettingOptions{
		Region:          regionCN,
		DisplayType:     displayTypeWindow,
		Resolution:      defaultResolution,
		GraphicsQuality: optionUnchanged,
		FrameRate:       optionUnchanged,
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
	return opts, nil
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
