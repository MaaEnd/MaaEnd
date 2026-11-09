package countdown

import (
	"fmt"
	"strings"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/dynamic_html_log"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 默认每次sleep500毫秒
var (
	step = time.Millisecond * 500
)

type Action struct {
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

	// 获取一个新的动态日志实例
	dynLog := dynamic_html_log.New()

	// 获取一个副本，以便结束后输出总时间
	duration := p.duration
	deadline := time.Now().Add(duration)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		// 任务终止
		if tasker := ctx.GetTasker(); tasker != nil {
			if tasker.Stopping() {
				log.Info().Str("component", "CountDown").Msg("task stopping, exit early")
				dynLog.T(p.StopMessage).Print()
				return true
			}
		} else {
			log.Info().Str("component", "CountDown").Msg("get tasker fail")
			dynLog.T(p.StopMessage).Print()
			return true
		}

		// 更新倒计时
		p.duration = remaining
		dynLog.T(p.StepMessage, p.durationFormat()).Print()

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
	dynLog.Message(dm).Print()
	return true
}
