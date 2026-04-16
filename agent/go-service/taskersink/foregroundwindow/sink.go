package foregroundwindow

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ForegroundWindowSink 会在 Win32-Front 任务启动前尽力将游戏窗口置前。
type ForegroundWindowSink struct {
	activated bool
}

var _ maa.TaskerEventSink = &ForegroundWindowSink{}

// Register 注册前台窗口 tasker sink。
func Register() {
	maa.AgentServerAddTaskerSink(&ForegroundWindowSink{})
}

// OnTaskerTask 会在 Win32-Front 任务启动时尝试前置游戏窗口。
func (s *ForegroundWindowSink) OnTaskerTask(_ *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	switch event {
	case maa.EventStatusStarting:
		if s.activated {
			return
		}
		if detail.Entry == "MaaTaskerPostStop" {
			return
		}
		ctrl := pienv.GetController()
		if ctrl == nil || ctrl.Win32 == nil {
			return
		}
		if pienv.ControllerName() != "Win32-Front" {
			return
		}

		if err := activateForegroundWindow(ctrl.Win32.WindowRegex, ctrl.Win32.ClassRegex); err != nil {
			log.Warn().
				Err(err).
				Str("component", "foregroundwindow").
				Str("controller", pienv.ControllerName()).
				Uint64("task_id", detail.TaskID).
				Str("entry", detail.Entry).
				Msg("failed to activate window")
			return
		}
		s.activated = true
		log.Info().
			Str("component", "foregroundwindow").
			Str("controller", pienv.ControllerName()).
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Msg("foreground window activated")
	case maa.EventStatusSucceeded, maa.EventStatusFailed:
		s.activated = false
	}
}
