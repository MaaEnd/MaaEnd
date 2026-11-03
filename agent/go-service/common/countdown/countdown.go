package countdown

import (
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/dynamic_html_log"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// 默认每次sleep500毫秒
var step = time.Millisecond * 500

type Action struct {
}

func (action *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", "CountDown").Msg("interruptible sleep: nil arg")
		return false
	}
	p := action.parseParams(arg)
	if p == nil {
		return false
	}

	// 获取一个新的动态日志实例
	dynLog := dynamic_html_log.New()

	// 获取一个副本，以便结束后输出总时间
	duration := p.Duration
	for duration > 0 {
		// 任务终止
		if ctx.GetTasker().Stopping() {
			log.Info().Str("component", "CountDown").Msg("task stopping, exit early")
			dynLog.T(p.StopMessage).Print()
			return true
		}

		// 更新倒计时
		mf, sf := durationFormat(duration, p.DurationFormatMinute, p.DurationFormatSec)
		dynLog.T(p.StepMessage, mf, sf).Print()

		time.Sleep(step)
		duration -= step
	}

	// 倒计时结束
	mf, sf := durationFormat(p.Duration, p.DurationFormatMinute, p.DurationFormatSec)
	dynLog.T(p.DoneMessage, mf, sf).Print()
	return true
}
