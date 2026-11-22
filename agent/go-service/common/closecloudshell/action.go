package closecloudshell

import (
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const shellProcessName = "maaend-cloud.exe"

// CloseCloudShellAction 结束云终末地网页外壳，用于 CloudWeb 控制器的「关闭游戏（网页）」。
// 登录状态保存在外壳的 cache/cloud-shell 里，结束进程不会丢失。
type CloseCloudShellAction struct{}

var _ maa.CustomActionRunner = &CloseCloudShellAction{}

func (a *CloseCloudShellAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	procs, err := process.Processes()
	if err != nil {
		log.Error().Err(err).Str("process", shellProcessName).Msg("CloseCloudShellAction failed to enumerate processes")
		return false
	}

	killedAny := false
	for _, p := range procs {
		name, err := p.Name()
		if err != nil || !strings.EqualFold(name, shellProcessName) {
			continue
		}

		killedAny = true
		if err := p.Kill(); err != nil {
			log.Error().Err(err).Str("process", shellProcessName).Msg("CloseCloudShellAction failed to kill process")
			return false
		}
	}

	if killedAny {
		log.Info().Str("process", shellProcessName).Msg("CloseCloudShellAction: process killed")
	} else {
		log.Info().Str("process", shellProcessName).Msg("CloseCloudShellAction: process not running")
	}
	return true
}
