package autoecofarm

import (
	"encoding/json"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const interruptibleSleepChunkMs = 250

type interruptibleSleepParams struct {
	DurationMs       int `json:"durationMs"`
	ReportIntervalMs int `json:"reportIntervalMs,omitempty"`
}

type autoEcoFarmInterruptibleSleep struct{}

var _ maa.CustomActionRunner = &autoEcoFarmInterruptibleSleep{}

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

	if params.ReportIntervalMs <= 0 {
		params.ReportIntervalMs = 5000
	}

	remaining := params.DurationMs
	nextReportRemaining := remaining - params.ReportIntervalMs

	for remaining > 0 {
		if ctx.GetTasker().Stopping() {
			log.Info().Str("component", "AutoEcoFarm").Msg("interruptible sleep: task stopping, exit early")
			maafocus.PrintLargeContentTrimNewline(
				i18n.RenderHTML("autoecofarm.interruptible_sleep_done", map[string]any{}),
			)
			return true
		}

		chunk := interruptibleSleepChunkMs
		if remaining < chunk {
			chunk = remaining
		}
		time.Sleep(time.Duration(chunk) * time.Millisecond)
		remaining -= chunk

		if params.ReportIntervalMs > 0 && remaining < nextReportRemaining {
			seconds := (remaining + 999) / 1000
			maafocus.PrintLargeContentTrimNewline(
				i18n.RenderHTML("autoecofarm.interruptible_sleep", map[string]any{
					"RemainingSeconds": seconds,
				}),
			)
			nextReportRemaining -= params.ReportIntervalMs
		}
	}

	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("autoecofarm.interruptible_sleep_done", map[string]any{}),
	)
	return true
}
