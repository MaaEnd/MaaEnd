package autoecofarm

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// interruptibleSleepChunkMs 单次休眠粒度，便于在两次 sleep 之间检查
// Stopping，避免长 post_delay 无法停止任务。
const interruptibleSleepChunkMs = 250

type interruptibleSleepParams struct {
	DurationMs       int `json:"durationMs"`
	ReportIntervalMs int `json:"reportIntervalMs,omitempty"`
}

type autoEcoFarmInterruptibleSleep struct{}

var _ maa.CustomActionRunner = &autoEcoFarmInterruptibleSleep{}

// Run 在约 durationMs 毫秒内分片休眠；期间每 reportIntervalMs 输出一次剩
// 余秒数（HTML stdout，客户端只展示最新一条），收到停止信号则提前结束。
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
	// 无等待时间，直接返回
	if params.DurationMs <= 0 {
		return true
	}

	// 未指定报告间隔时默认 5 秒
	if params.ReportIntervalMs <= 0 {
		params.ReportIntervalMs = 5000
	}

	// 下次触发输出时的剩余毫秒阈值
	remaining := params.DurationMs
	nextReportRemaining := remaining - params.ReportIntervalMs

	for remaining > 0 {
		// 收到停止信号 → 提前结束
		if ctx.GetTasker().Stopping() {
			log.Info().Str("component", "AutoEcoFarm").Msg("interruptible sleep: task stopping, exit early")
			maafocus.PrintLargeContentTrimNewline(
				i18n.RenderHTML("autoecofarm.interruptible_sleep_stopped", map[string]any{}),
			)
			return true
		}

		// 剩余时间到达或跨过阈值 → 在 sleep 前输出 mm:ss 倒计时
		if remaining <= nextReportRemaining {
			m := remaining / 60000
			s := (remaining % 60000) / 1000
			formatted := fmt.Sprintf("%02d:%02d", m, s)
			maafocus.PrintLargeContentTrimNewline(
				i18n.RenderHTML("autoecofarm.interruptible_sleep", map[string]any{
					"Formatted": formatted,
				}),
			)
			nextReportRemaining -= params.ReportIntervalMs
		}

		// 分片休眠（250ms 粒度）
		chunk := interruptibleSleepChunkMs
		if remaining < chunk {
			chunk = remaining
		}
		time.Sleep(time.Duration(chunk) * time.Millisecond)
		remaining -= chunk
	}

	// 休眠自然结束 → 输出完成

	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("autoecofarm.interruptible_sleep_done", map[string]any{}),
	)
	return true
}
