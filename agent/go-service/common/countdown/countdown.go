package countdown

import (
	"fmt"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 默认每次sleep500毫秒
var (
	step = time.Millisecond * 500
)

const (
	shortDurationMax = 10 * time.Second
	midDurationMax   = 5 * time.Minute
	midLogInterval   = 30 * time.Second
	longLogInterval  = 60 * time.Second
)

type Action struct {
}

// logInterval 按总时长决定倒计时日志的输出间隔，返回 0 表示只输出开始和结束
func logInterval(duration time.Duration) time.Duration {
	switch {
	case duration <= shortDurationMax:
		return 0
	case duration <= midDurationMax:
		return midLogInterval
	default:
		return longLogInterval
	}
}

func (action *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", "CountDown").Msg("interruptible sleep: nil arg")
		return false
	}
	p, err := action.parseParams(arg)
	if err != nil {
		log.Error().
			Str("component", "CountDown").
			Err(err).
			Msg("Failed to parse params")
		return false
	}
	if p == nil {
		return true
	}

	// 获取一个副本，以便结束后输出总时间
	duration := p.duration
	interval := logInterval(duration)
	start := time.Now()
	deadline := start.Add(duration)
	nextLogAt := start
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		// 任务终止
		if tasker := ctx.GetTasker(); tasker != nil {
			if tasker.Stopping() {
				log.Info().Str("component", "CountDown").Msg("task stopping, exit early")
				maafocus.Print(ctx, i18n.T(p.StopMessage))
				return true
			}
		} else {
			log.Info().Str("component", "CountDown").Msg("get tasker fail")
			maafocus.Print(ctx, i18n.T(p.StopMessage))
			return true
		}

		// 按间隔输出剩余时间
		if !time.Now().Before(nextLogAt) {
			p.duration = remaining
			maafocus.Print(ctx, i18n.T(p.StepMessage, p.durationFormat()))
			if interval > 0 {
				nextLogAt = nextLogAt.Add(interval)
			} else {
				nextLogAt = deadline
			}
		}

		remaining = time.Until(deadline)
		if remaining <= 0 {
			break
		}
		time.Sleep(min(step, remaining))
	}

	// 倒计时结束
	p.duration = duration
	dm := i18n.T(p.DoneMessage)
	if strings.Contains(dm, "%s") {
		dm = fmt.Sprintf(dm, p.durationFormat())
	}
	maafocus.Print(ctx, dm)
	return true
}
