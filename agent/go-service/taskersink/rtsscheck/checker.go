package rtsscheck

import (
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/control"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// RTSSChecker blocks tasks when RTSS EnableOSD is on for Endfield.exe.
// The check runs at most once per go-service process lifetime.
type RTSSChecker struct {
	checked bool
}

// OnTaskerTask handles tasker task events.
func (c *RTSSChecker) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
	if event != maa.EventStatusStarting {
		return
	}
	if detail.Entry == "MaaTaskerPostStop" {
		return
	}
	if c.checked {
		return
	}
	if !isWin32Controller(tasker) {
		log.Debug().
			Uint64("task_id", detail.TaskID).
			Str("entry", detail.Entry).
			Str("controller_type", pienv.ControllerType()).
			Msg("Skipping RTSS OSD check: not Win32 controller")
		return
	}

	log.Debug().
		Uint64("task_id", detail.TaskID).
		Str("entry", detail.Entry).
		Msg("Checking RTSS EnableOSD before task execution")

	enabled, profile, err := isEnableOSDEnabled()
	c.checked = true
	if err != nil {
		log.Warn().Err(err).Msg("RTSS OSD check failed; allowing task (fail-open)")
		return
	}
	if !enabled {
		log.Debug().
			Str("profile", profile).
			Msg("RTSS OSD check passed: EnableOSD is off or RTSS not installed")
		return
	}

	log.Warn().
		Str("profile", profile).
		Msg("RTSS EnableOSD is enabled; stopping task")

	maafocus.PrintLargeContentTrimNewline(
		i18n.RenderHTML("tasker.rtss_osd_warning", map[string]any{
			"Profile": profile,
		}),
	)
	tasker.PostStop()
}

func isWin32Controller(tasker *maa.Tasker) bool {
	if controlType := normalizeControllerType(pienv.ControllerType()); controlType == control.CONTROL_TYPE_WIN32 {
		return true
	}
	controller := tasker.GetController()
	if controller == nil {
		return false
	}
	controlType, err := control.GetControlType(controller)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to resolve controller type for RTSS check")
		return false
	}
	return normalizeControllerType(controlType) == control.CONTROL_TYPE_WIN32
}

func normalizeControllerType(controllerType string) string {
	switch strings.ToLower(strings.TrimSpace(controllerType)) {
	case "adb":
		return control.CONTROL_TYPE_ADB
	case "win32":
		return control.CONTROL_TYPE_WIN32
	case "linux":
		return control.CONTROL_TYPE_LINUX
	case "macos":
		return control.CONTROL_TYPE_MACOS
	default:
		return ""
	}
}
