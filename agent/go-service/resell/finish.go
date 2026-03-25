package resell

import (
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ResellFinishAction - Finish Resell task custom action
type ResellFinishAction struct{}

var _ maa.CustomActionRunner = &ResellFinishAction{}

func (a *ResellFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Msg("[Resell]运行结束")
	return true
}
