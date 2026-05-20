package aspectratio

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &RestoreResolutionAction{}

// RestoreResolutionAction restores the original resolution recorded by the explicit 1280x720 task.
type RestoreResolutionAction struct{}

func (a *RestoreResolutionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if defaultChecker == nil {
		log.Warn().Str("component", "AspectRatioRestoreResolution").Msg("aspect ratio checker is not initialized")
		return true
	}
	return defaultChecker.restoreOriginalResolution(ctx)
}
