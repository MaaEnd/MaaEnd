package combatprotocol

import (
	"encoding/json"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &AccumulateAction{}

// accumulateParam names the task whose completed run should be accounted.
type accumulateParam struct {
	Task string `json:"task"`
}

// AccumulateAction adds the sanity consumed by one completed battle to the
// current slot's accumulator. Once the accumulator reaches the slot's
// threshold the slot's record is cleared, the rotation advances and the
// current run is meant to end: the pipeline then stops the task, and the
// client starts the next slot with its own configuration.
type AccumulateAction struct{}

// Run implements maa.CustomActionRunner.
func (a *AccumulateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params accumulateParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolAccumulate").
			Msg("failed to parse params")
		return false
	}
	if params.Task != TaskAutoEssence && params.Task != TaskProtocolSpace {
		log.Error().
			Str("component", "CombatProtocolAccumulate").
			Str("task", params.Task).
			Msg("unknown task")
		return false
	}

	var progressMsg, roundMsg string
	stateMu.Lock()
	st, err := loadState()
	if err != nil {
		stateMu.Unlock()
		log.Error().
			Err(err).
			Str("component", "CombatProtocolAccumulate").
			Msg("failed to load rotation state")
		return false
	}
	if res, changed := accumulateBattle(&st, params.Task); changed {
		if res.Rotated {
			if res.RoundDone {
				roundMsg = i18n.T("combatprotocol.round_complete")
			}
			progressMsg = i18n.T(
				"combatprotocol.rotate",
				i18n.T(taskLabelKey(params.Task)),
				res.Accum,
				res.Threshold,
				i18n.T(taskLabelKey(st.Slots[res.Next].Task)),
			)
		} else {
			progressMsg = i18n.T(
				"combatprotocol.accumulate",
				i18n.T(taskLabelKey(params.Task)),
				res.Cost,
				res.Accum,
				res.Threshold,
			)
		}
		log.Info().
			Str("component", "CombatProtocolAccumulate").
			Str("task", params.Task).
			Int("cost", res.Cost).
			Int("accum", res.Accum).
			Int("threshold", res.Threshold).
			Bool("rotated", res.Rotated).
			Int("slot_index", st.CurrentIndex).
			Msg("sanity accumulated")
		if err := saveState(st); err != nil {
			log.Error().
				Err(err).
				Str("component", "CombatProtocolAccumulate").
				Msg("failed to save rotation state")
		}
	}
	stateMu.Unlock()

	if progressMsg != "" {
		maafocus.Print(ctx, progressMsg)
	}
	if roundMsg != "" {
		maafocus.Print(ctx, roundMsg)
	}
	return true
}
