package visitfriends

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type countAction struct{}

var _ maa.CustomActionRunner = &countAction{}

func (a *countAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}

	var param struct {
		MaxVisits json.Number `json:"max_visits"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Msg("VisitFriends: invalid visit limit")
		return false
	}
	maxVisits, err := param.MaxVisits.Int64()
	if err != nil || maxVisits < 1 {
		log.Error().Err(err).Msg("VisitFriends: visit limit must be positive")
		return false
	}

	raw, err := ctx.GetNodeJSON(arg.CurrentTaskName)
	if err != nil {
		log.Error().Err(err).Msg("VisitFriends: failed to read visit count")
		return false
	}
	var node struct {
		Attach struct {
			Visits int64 `json:"visits"`
		} `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil || node.Attach.Visits < 0 {
		log.Error().Err(err).Msg("VisitFriends: invalid visit count")
		return false
	}

	patch, reached := countPatch(arg.CurrentTaskName, node.Attach.Visits+1, maxVisits)
	if err := ctx.OverridePipeline(patch); err != nil {
		log.Error().Err(err).Msg("VisitFriends: failed to update visit count")
		return false
	}
	log.Info().Int64("visits", node.Attach.Visits+1).Int64("limit", maxVisits).Bool("limit_reached", reached).Msg("VisitFriends: completed visit")
	return true
}

func countPatch(nodeName string, visits, maxVisits int64) (map[string]any, bool) {
	patch := map[string]any{
		nodeName: map[string]any{"attach": map[string]any{"visits": visits}},
	}
	reached := visits >= maxVisits
	if reached {
		patch["VisitFriendsMenuScan"] = map[string]any{
			"next": []string{"VisitFriendsVisitLimitReached"},
		}
	}
	return patch, reached
}

func Register() {
	maa.AgentServerRegisterCustomAction("VisitFriendsCountAction", &countAction{})
}
