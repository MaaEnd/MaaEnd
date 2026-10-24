package closegame

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	gamesetting "github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/gamesetting"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const gameProcessName = "Endfield.exe"

// launcherProcessNames 列出各区服启动器的进程名；关闭游戏前逐个结束。
var launcherProcessNames = []string{"Launcher.exe", "Games.exe"}

type CloseGameAction struct{}

var _ maa.CustomActionRunner = &CloseGameAction{}

type closeGameSettingParam struct {
	CloseLauncher          bool            `json:"CloseLauncher,omitempty"`
	ApplyGameSetting       bool            `json:"ApplyGameSetting,omitempty"`
	GameSettingRegion      string          `json:"GameSettingRegion,omitempty"`
	GameSettingLanguage    string          `json:"GameSettingLanguage,omitempty"`
	GameSettingDisplayType string          `json:"GameSettingDisplayType,omitempty"`
	ResolutionWidth        json.RawMessage `json:"ResolutionWidth,omitempty"`
	ResolutionHeight       json.RawMessage `json:"ResolutionHeight,omitempty"`
	GraphicsQuality        int             `json:"GraphicsQuality"`
	FrameRate              int             `json:"FrameRate"`
	AutoHDR                string          `json:"AutoHDR,omitempty"`
}

func parseUint32FromRaw(raw json.RawMessage) (uint32, bool) {
	if len(raw) == 0 {
		return 0, false
	}

	var u uint32
	if err := json.Unmarshal(raw, &u); err == nil && u != 0 {
		return u, true
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		if v, err := strconv.ParseUint(s, 10, 32); err == nil && v != 0 {
			return uint32(v), true
		}
	}

	return 0, false
}

func loadAttach(ctx *maa.Context, nodeName string) closeGameSettingParam {
	params := closeGameSettingParam{
		GameSettingRegion:      "CN",
		GameSettingLanguage:    "Unchanged",
		GameSettingDisplayType: "Window",
		GraphicsQuality:        -1,
		FrameRate:              -1,
		AutoHDR:                "Unchanged",
	}
	if ctx == nil || nodeName == "" {
		return params
	}

	raw, err := ctx.GetNodeJSON(nodeName)
	if err != nil || strings.TrimSpace(raw) == "" {
		log.Warn().Err(err).Str("node", nodeName).Msg("CloseGameAction failed to get node json")
		return params
	}

	var wrapper struct {
		Attach closeGameSettingParam `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		log.Error().Err(err).Str("node", nodeName).Msg("CloseGameAction failed to parse attach")
		return params
	}
	if wrapper.Attach.GameSettingRegion != "" {
		params.GameSettingRegion = wrapper.Attach.GameSettingRegion
	}
	// 语言用 case 名传递而非数值：简体中文对应 0，用整数会与「未提供」混淆
	if wrapper.Attach.GameSettingLanguage != "" {
		params.GameSettingLanguage = wrapper.Attach.GameSettingLanguage
	}
	if wrapper.Attach.GameSettingDisplayType != "" {
		params.GameSettingDisplayType = wrapper.Attach.GameSettingDisplayType
	}
	params.CloseLauncher = wrapper.Attach.CloseLauncher
	params.ApplyGameSetting = wrapper.Attach.ApplyGameSetting
	params.ResolutionWidth = wrapper.Attach.ResolutionWidth
	params.ResolutionHeight = wrapper.Attach.ResolutionHeight
	// 0 是 Go int 零值；未出现在 attach 时保持默认 -1（不修改）
	if wrapper.Attach.GraphicsQuality != 0 {
		params.GraphicsQuality = wrapper.Attach.GraphicsQuality
	}
	if wrapper.Attach.FrameRate != 0 {
		params.FrameRate = wrapper.Attach.FrameRate
	}
	if wrapper.Attach.AutoHDR != "" {
		params.AutoHDR = wrapper.Attach.AutoHDR
	}
	return params
}

func (a *CloseGameAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	const defaultResolution = "1280x720"

	nodeName := "CloseGamePC"
	if arg != nil && arg.CurrentTaskName != "" {
		nodeName = arg.CurrentTaskName
	}
	params := loadAttach(ctx, nodeName)

	resolution := defaultResolution
	width, widthOK := parseUint32FromRaw(params.ResolutionWidth)
	height, heightOK := parseUint32FromRaw(params.ResolutionHeight)
	switch {
	case widthOK && heightOK:
		resolution = fmt.Sprintf("%dx%d", width, height)
	case widthOK || heightOK || len(params.ResolutionWidth) > 0 || len(params.ResolutionHeight) > 0:
		log.Error().
			Bool("width_ok", widthOK).
			Bool("height_ok", heightOK).
			Str("width_raw", string(params.ResolutionWidth)).
			Str("height_raw", string(params.ResolutionHeight)).
			Str("fallback", defaultResolution).
			Msg("CloseGameAction: incomplete or invalid resolution attach, keeping default")
		return false
	}

	// 先关启动器，再关游戏：启动器若仍在运行可能会重新拉起游戏进程。
	if params.CloseLauncher {
		for _, name := range launcherProcessNames {
			if !killByName(name) {
				return false
			}
		}
	}

	if !killByName(gameProcessName) {
		return false
	}
	if !waitEndfieldExit(ctx, 5*time.Second) {
		return false
	}

	if !params.ApplyGameSetting {
		log.Info().Msg("CloseGameAction: skip game settings")
		return true
	}

	if runtime.GOOS != "windows" {
		log.Info().Msg("CloseGameAction: game settings are only supported on windows, skipped")
		return true
	}

	if err := gamesetting.SetRegion(params.GameSettingRegion); err != nil {
		log.Error().
			Err(err).
			Str("region", params.GameSettingRegion).
			Msg("CloseGameAction: invalid game region")
		return false
	}

	if ok := gamesetting.Apply(params.GameSettingDisplayType, resolution); !ok {
		log.Error().
			Str("region", params.GameSettingRegion).
			Str("display_type", params.GameSettingDisplayType).
			Str("resolution", resolution).
			Msg("CloseGameAction: failed to apply game settings")
		return false
	}

	// Apply 已按区服选定注册表路径，此处才能写入语言项。
	// 游戏此时已退出，写入的语言会在下次启动游戏时生效。
	if err := gamesetting.ApplyTextLanguage(params.GameSettingLanguage); err != nil {
		log.Error().Err(err).Str("language", params.GameSettingLanguage).Msg("CloseGameAction: failed to apply game language")
		return false
	}

	if params.GraphicsQuality > 0 {
		if err := gamesetting.SetVideoQualityMain(uint32(params.GraphicsQuality)); err != nil {
			log.Error().Err(err).Int("graphics_quality", params.GraphicsQuality).Msg("CloseGameAction: failed to set graphics quality")
			return false
		}
		log.Info().Int("graphics_quality", params.GraphicsQuality).Msg("CloseGameAction: applied graphics quality")
	}

	if params.FrameRate > 0 {
		if err := gamesetting.SetVideoFrameRate8(uint32(params.FrameRate)); err != nil {
			log.Error().Err(err).Int("frame_rate", params.FrameRate).Msg("CloseGameAction: failed to set frame rate")
			return false
		}
		log.Info().Int("frame_rate", params.FrameRate).Msg("CloseGameAction: applied frame rate")
	}

	if err := gamesetting.ApplyAutoHDR(params.AutoHDR); err != nil {
		log.Error().Err(err).Str("auto_hdr", params.AutoHDR).Msg("CloseGameAction: failed to apply Auto HDR")
		return false
	}

	log.Info().
		Str("region", params.GameSettingRegion).
		Str("language", params.GameSettingLanguage).
		Str("display_type", params.GameSettingDisplayType).
		Str("resolution", resolution).
		Int("graphics_quality", params.GraphicsQuality).
		Int("frame_rate", params.FrameRate).
		Str("auto_hdr", params.AutoHDR).
		Msg("CloseGameAction: applied game settings")

	return true
}

// waitEndfieldExit 轮询等待 Endfield.exe 退出；超时 5s，每秒检查 Stopping。
// 返回 false 表示任务已要求停止，调用方应立即结束。
func waitEndfieldExit(ctx *maa.Context, timeout time.Duration) bool {
	const pollInterval = time.Second
	deadline := time.Now().Add(timeout)

	for {
		if ctx != nil && ctx.GetTasker() != nil && ctx.GetTasker().Stopping() {
			log.Info().Msg("CloseGameAction: task stopping while waiting for Endfield.exe exit")
			return false
		}

		running, err := isEndfieldRunning()
		if err != nil {
			log.Error().Err(err).Msg("CloseGameAction: failed to check Endfield.exe while waiting")
			return false
		}
		if !running {
			log.Info().Msg("CloseGameAction: Endfield.exe exited")
			return true
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			log.Warn().Dur("timeout", timeout).Msg("CloseGameAction: timed out waiting for Endfield.exe exit")
			return true
		}

		sleep := pollInterval
		if remaining < sleep {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
}

func isEndfieldRunning() (bool, error) {
	procs, err := process.Processes()
	if err != nil {
		return false, err
	}
	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}
		if strings.EqualFold(name, gameProcessName) {
			return true, nil
		}
	}
	return false, nil
}

// killByName 结束所有匹配进程名的进程；未找到视为成功。
// 枚举或 Kill 失败返回 false，由调用方终止整个 Action。
func killByName(name string) bool {
	procs, err := process.Processes()
	if err != nil {
		log.Error().Err(err).Str("process", name).Msg("CloseGameAction failed to enumerate processes")
		return false
	}

	killedAny := false
	for _, p := range procs {
		procName, err := p.Name()
		if err != nil || !strings.EqualFold(procName, name) {
			continue
		}

		killedAny = true
		if err := p.Kill(); err != nil {
			log.Error().Err(err).Str("process", name).Msg("CloseGameAction failed to kill process")
			return false
		}
	}

	if killedAny {
		log.Info().Str("process", name).Msg("CloseGameAction: process killed")
	} else {
		log.Info().Str("process", name).Msg("CloseGameAction: process not running")
	}
	return true
}
