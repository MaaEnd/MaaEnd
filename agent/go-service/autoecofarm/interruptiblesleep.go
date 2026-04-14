package autoecofarm

import (
	"encoding/json"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 单次休眠粒度，便于在两次 sleep 之间检查 Stopping，避免长 post_delay 无法停止任务。
const interruptibleSleepChunkMs = 250

type interruptibleSleepParams struct {
	DurationMs int `json:"durationMs"`
}

type autoEcoFarmInterruptibleSleep struct{}

var _ maa.CustomActionRunner = &autoEcoFarmInterruptibleSleep{}

// Run 在约 durationMs 毫秒内分片休眠；若收到停止任务信号则提前结束并返回 true。
func (a *autoEcoFarmInterruptibleSleep) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", "AutoEcoFarm").Msg("interruptible sleep: nil arg")
		return false
	}
	var params interruptibleSleepParams
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "AutoEcoFarm").
			Str("param", arg.CustomActionParam).
			Msg("interruptible sleep: parse param failed")
		return false
	}
	if params.DurationMs <= 0 {
		return true
	}
	remaining := params.DurationMs
	for remaining > 0 {
		if ctx.GetTasker().Stopping() {
			log.Info().Str("component", "AutoEcoFarm").Msg("interruptible sleep: task stopping, exit early")
			return true
		}
		chunk := interruptibleSleepChunkMs
		if remaining < chunk {
			chunk = remaining
		}
		time.Sleep(time.Duration(chunk) * time.Millisecond)
		remaining -= chunk
	}
	return true
}
