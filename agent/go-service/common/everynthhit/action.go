package everynthhit

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const actionName = "EveryNthHitReset"

var _ maa.CustomActionRunner = &EveryNthHitReset{}

// everyNthHitResetParam 是 EveryNthHitReset 的 custom_action_param。
type everyNthHitResetParam struct {
	// Key 是要清零的计数器名字，必须与 EveryNthHit 使用的 key 一致。
	Key string `json:"key"`
}

// EveryNthHitReset 清零 EveryNthHit 中某个 key 的计数。
type EveryNthHitReset struct{}

// Run implements maa.CustomActionRunner.
func (a *EveryNthHitReset) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().
			Str("component", actionName).
			Msg("got nil custom action arg")
		return false
	}

	var param everyNthHitResetParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", actionName).
			Str("param", arg.CustomActionParam).
			Msg("failed to parse custom_action_param")
		return false
	}
	if param.Key == "" {
		log.Error().
			Str("component", actionName).
			Str("param", arg.CustomActionParam).
			Msg("custom_action_param.key is required")
		return false
	}

	Reset(param.Key)
	log.Debug().
		Str("component", actionName).
		Str("key", param.Key).
		Msg("counter reset")
	return true
}
