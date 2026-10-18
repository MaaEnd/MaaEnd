package everynthhit

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const actionName = "EveryNthHitReset"

type resetParam struct {
	Key string `json:"key"`
}

// EveryNthHitReset clears the counter of key so the next hit restarts the cycle.
type EveryNthHitReset struct{}

var _ maa.CustomActionRunner = &EveryNthHitReset{}

// Run implements maa.CustomActionRunner.
func (*EveryNthHitReset) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Str("component", actionName).Msg("custom action arg is nil")
		return false
	}

	var param resetParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil || param.Key == "" {
		log.Error().
			Err(err).
			Str("component", actionName).
			Str("param", arg.CustomActionParam).
			Msg("invalid parameters")
		return false
	}

	Reset(param.Key)
	return true
}
