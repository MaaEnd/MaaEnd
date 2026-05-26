//go:build !windows

package accountswitch

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type WindowAction struct{}

func (a *WindowAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	_ = ctx
	_ = arg
	log.Warn().
		Str("component", componentName).
		Msg("account switch window action is only supported on Windows")
	return false
}
