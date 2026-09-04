package gamesetting

import (
	"encoding/json"
	"fmt"
	"strconv"
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

	optionUnchanged          = "Unchanged"
	defaultCameraSensitivity = "0"
	minimumCameraSensitivity = -5
	maximumCameraSensitivity = 5
)

type gameSettingOptions struct {
	Region             string `json:"GameSettingRegion"`
	DisplayType        string `json:"GameSettingDisplayType"`
	Resolution         string `json:"GameSettingResolution"`
	GraphicsQuality    string `json:"GameSettingGraphicsQuality"`
	FrameRate          string `json:"GameSettingFrameRate"`
	CameraSensitivityX string `json:"GameSettingCameraSensitivityX"`
	CameraSensitivityY string `json:"GameSettingCameraSensitivityY"`
	AutoHDR            string `json:"GameSettingAutoHDR"`
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

	cameraSensitivityX, err := parseCameraSensitivity(opts.CameraSensitivityX)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("camera_sensitivity_x", opts.CameraSensitivityX).
			Msg("invalid horizontal camera sensitivity")
		return false
	}
	cameraSensitivityY, err := parseCameraSensitivity(opts.CameraSensitivityY)
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Str("camera_sensitivity_y", opts.CameraSensitivityY).
			Msg("invalid vertical camera sensitivity")
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
		Int("camera_sensitivity_x", cameraSensitivityX).
		Int("camera_sensitivity_y", cameraSensitivityY).
		Str("auto_hdr", opts.AutoHDR).
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

	cameraSensitivityApplied := true
	if err := SetControllerCameraSpeedX(cameraSensitivityX); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Int("camera_sensitivity_x", cameraSensitivityX).
			Msg("failed to set horizontal camera sensitivity")
		cameraSensitivityApplied = false
	} else {
		log.Info().
			Str("component", "gamesetting").
			Int("camera_sensitivity_x", cameraSensitivityX).
			Msg("applied horizontal camera sensitivity")
	}

	if err := SetControllerCameraSpeedY(cameraSensitivityY); err != nil {
		log.Error().
			Err(err).
			Str("component", "gamesetting").
			Int("camera_sensitivity_y", cameraSensitivityY).
			Msg("failed to set vertical camera sensitivity")
		cameraSensitivityApplied = false
	} else {
		log.Info().
			Str("component", "gamesetting").
			Int("camera_sensitivity_y", cameraSensitivityY).
			Msg("applied vertical camera sensitivity")
	}
	if !cameraSensitivityApplied {
		return false
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
		Region:             regionCN,
		DisplayType:        displayTypeWindow,
		Resolution:         defaultResolution,
		GraphicsQuality:    optionUnchanged,
		FrameRate:          optionUnchanged,
		CameraSensitivityX: defaultCameraSensitivity,
		CameraSensitivityY: defaultCameraSensitivity,
		AutoHDR:            optionUnchanged,
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
	if opts.CameraSensitivityX == "" {
		opts.CameraSensitivityX = defaultCameraSensitivity
	}
	if opts.CameraSensitivityY == "" {
		opts.CameraSensitivityY = defaultCameraSensitivity
	}
	if opts.AutoHDR == "" {
		opts.AutoHDR = optionUnchanged
	}
	return opts, nil
}

// ValidateCameraSensitivity checks whether a camera sensitivity value is supported by Endfield.
func ValidateCameraSensitivity(value int) error {
	if value < minimumCameraSensitivity || value > maximumCameraSensitivity {
		return fmt.Errorf(
			"gamesetting: camera sensitivity %d must be between %d and %d",
			value,
			minimumCameraSensitivity,
			maximumCameraSensitivity,
		)
	}
	return nil
}

func parseCameraSensitivity(raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("gamesetting: parse camera sensitivity %q: %w", raw, err)
	}
	if err := ValidateCameraSensitivity(value); err != nil {
		return 0, err
	}
	return value, nil
}

func cameraSensitivityDWORD(value int) (uint32, error) {
	if err := ValidateCameraSensitivity(value); err != nil {
		return 0, err
	}
	return uint32(int32(value)), nil
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
