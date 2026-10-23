// Package touchcheck rejects tasks when the connected ADB backend lacks touch input.
package touchcheck

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/pienv"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Action checks the connected backend without pressing or moving a touch point.
type Action struct{}

var _ maa.CustomActionRunner = &Action{}

// Require runs the check in its own callback so borrowed controller handles stay local.
func Require(ctx *maa.Context) bool {
	detail, err := ctx.RunAction("TouchInputCheck", maa.Rect{}, "", map[string]any{
		"TouchInputCheck": map[string]any{
			"action": "Custom", "custom_action": "TouchInputCheck",
			"pre_delay": 0, "post_delay": 0,
		},
	})
	if err == nil && detail != nil && detail.Success {
		return true
	}
	if err != nil || detail == nil {
		ReportFailure(ctx)
	}
	return false
}

// Run releases the unused attack contact once; AdbShell cannot perform this operation.
// Success confirms the necessary touch API, not the game's full multi-touch behavior.
func (a *Action) Run(ctx *maa.Context, arg *maa.CustomActionArg) (ok bool) {
	defer func() {
		if !ok && arg != nil {
			PreventRecovery(ctx, arg.CurrentTaskName)
		}
	}()
	if arg == nil {
		return false
	}
	if Rejected(arg.TaskID) {
		return false
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false
	}
	ctrl := tasker.GetController()
	if ctrl == nil {
		reject(ctx, arg.TaskID, "touchcheck.controller_unavailable")
		return false
	}
	kind := pienv.ResolveControllerKind(ctrl)
	if !allowsCombat(kind, func() bool { return ctrl.PostTouchUp(4).Wait().Success() }) {
		key := "touchcheck.adb_fallback"
		if kind == pienv.ControllerKindUnknown {
			key = "touchcheck.controller_unavailable"
		}
		reject(ctx, arg.TaskID, key)
		return false
	}
	return true
}

func allowsCombat(kind string, release func() bool) bool {
	if kind == pienv.ControllerKindUnknown {
		return false
	}
	return kind != pienv.ControllerKindAdb || release()
}

// ReportFailure reports an ordinary input error without claiming a backend switch
// or task-wide interruption. Callers return false; the parent keeps its error policy.
func ReportFailure(ctx *maa.Context) {
	message := i18n.T("autofight.touch_input_unavailable")
	log.Error().Str("component", "TouchInputCheck").Msg(message)
	maafocus.Print(ctx, message)
}

func reject(ctx *maa.Context, taskID int64, key string) {
	rejections.Store(taskID, true)
	message := i18n.T(key)
	log.Error().Str("component", "TouchInputCheck").Str("reason", key).Msg(message)
	maafocus.Print(ctx, message)
}

// PreventRecovery keeps the rejected node's on_error branch from continuing the
// same task. It changes only the current context; independent tasks remain runnable.
func PreventRecovery(ctx *maa.Context, node string) {
	if err := ctx.OverridePipeline(map[string]any{node: map[string]any{"on_error": []string{}}}); err != nil {
		log.Error().Err(err).Str("component", "TouchInputCheck").Str("node", node).
			Msg("failed to remove recovery branch from rejected node")
	}
}
