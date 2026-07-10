package gamesetting

import (
	"encoding/json"
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
)

type gameSettingOptions struct {
	Region      string `json:"GameSettingRegion"`
	DisplayType string `json:"GameSettingDisplayType"`
	Resolution  string `json:"GameSettingResolution"`
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
		Msg("applying game settings")

	return Apply(opts.Region, opts.DisplayType, opts.Resolution)
}

func parseGameSettingOptions(args []string) (gameSettingOptions, error) {
	opts := gameSettingOptions{
		Region:      regionCN,
		DisplayType: displayTypeWindow,
		Resolution:  defaultResolution,
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
	return opts, nil
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
