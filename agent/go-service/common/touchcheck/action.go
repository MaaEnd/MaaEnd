package touchcheck

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// GuardedAction checks touch support before forwarding an existing Pipeline action.
type GuardedAction struct{}

var _ maa.CustomActionRunner = &GuardedAction{}

type guardedParam struct {
	Node string `json:"node"`
}

// Run preserves the recognition box and leaves action timing on the caller node.
func (a *GuardedAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) (ok bool) {
	if arg == nil {
		return false
	}
	defer func() {
		if !ok && Rejected(arg.TaskID) {
			PreventRecovery(ctx, arg.CurrentTaskName)
		}
	}()
	var param guardedParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil || param.Node == "" || param.Node == arg.CurrentTaskName {
		log.Error().Err(err).Str("component", "TouchInputAction").Msg("invalid forwarding node")
		return false
	}
	if !Require(ctx) {
		return false
	}
	recoDetail := ""
	if arg.RecognitionDetail != nil {
		recoDetail = arg.RecognitionDetail.DetailJson
	}
	detail, err := ctx.RunAction(param.Node, arg.Box, recoDetail)
	if err != nil || detail == nil || !detail.Success {
		log.Error().Err(err).Str("component", "TouchInputAction").Str("node", param.Node).
			Msg("touch action failed")
		ReportFailure(ctx)
		return false
	}
	return true
}
