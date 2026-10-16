package combatprotocol

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &StateRecognition{}

// stateParam names what the node expects from the rotation state: a combat
// task name ("AutoEssence" / "ProtocolSpace") for the run dispatch, or
// "rotated" for the stop node that ends the run after a threshold rotation.
type stateParam struct {
	Expect string `json:"expect"`
}

// StateRecognition reports a hit when the rotation state matches the
// expectation: the current slot runs the expected task, or the rotation has
// just advanced in this battle (expect "rotated").
type StateRecognition struct{}

// Run implements maa.CustomRecognitionRunner.
func (r *StateRecognition) Run(_ *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	var params stateParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolState").
			Msg("failed to parse params")
		return nil, false
	}

	st, err := readCurrentState()
	if err != nil {
		log.Error().
			Err(err).
			Str("component", "CombatProtocolState").
			Msg("failed to load rotation state")
		return nil, false
	}

	if params.Expect == RotatedExpect {
		if !st.RotatedThisBattle {
			return nil, false
		}
		detail, _ := json.Marshal(map[string]any{
			"rotated":    true,
			"slot_index": st.CurrentIndex,
			"mode":       st.Mode,
		})
		return &maa.CustomRecognitionResult{
			Box:    arg.Roi,
			Detail: string(detail),
		}, true
	}

	if params.Expect != TaskAutoEssence && params.Expect != TaskProtocolSpace {
		log.Error().
			Str("component", "CombatProtocolState").
			Str("expect", params.Expect).
			Msg("unknown expected task")
		return nil, false
	}
	slot, ok := st.currentSlot()
	if !ok || slot.Task != params.Expect {
		return nil, false
	}
	detail, _ := json.Marshal(map[string]any{
		"task":       slot.Task,
		"slot_index": st.CurrentIndex,
		"mode":       st.Mode,
	})
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(detail),
	}, true
}
