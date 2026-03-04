package inputreplay

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	keyW = 0x57
	keyA = 0x41
)

var keyNameToCode = map[string]int{
	"W": keyW,
	"A": keyA,
}

// ReplayRecordedInput replays a recorded sequence of W/A key events with timing.
type ReplayRecordedInput struct{}

// replayStep represents a single recorded key event.
type replayStep struct {
	Action string `json:"action"` // "down" or "up"
	Key    string `json:"key"`    // "W" or "A"
	T      int64  `json:"t"`      // relative timestamp in milliseconds
}

type replayParam struct {
	Steps []replayStep `json:"steps"`
}

var _ maa.CustomActionRunner = &ReplayRecordedInput{}

func (a *ReplayRecordedInput) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, err := parseParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Msg("[InputReplay] Failed to parse parameters")
		return false
	}
	if len(param.Steps) == 0 {
		log.Error().Msg("[InputReplay] Steps list is empty")
		return false
	}

	ctrl := ctx.GetTasker().GetController()

	log.Info().Int("count", len(param.Steps)).Msg("[InputReplay] Starting replay")

	var prevT int64
	for i, step := range param.Steps {
		if ctx.GetTasker().Stopping() {
			log.Warn().Msg("[InputReplay] Task stopping, releasing keys")
			releaseAll(ctrl)
			return false
		}

		dt := step.T - prevT
		if dt > 0 {
			time.Sleep(time.Duration(dt) * time.Millisecond)
		}
		prevT = step.T

		code, ok := keyNameToCode[step.Key]
		if !ok {
			log.Error().Str("key", step.Key).Int("step", i).Msg("[InputReplay] Unknown key")
			releaseAll(ctrl)
			return false
		}

		switch step.Action {
		case "down":
			ctrl.PostKeyDown(int32(code)).Wait()
			log.Debug().Str("key", step.Key).Int64("t", step.T).Int("step", i).Msg("[InputReplay] key_down")
		case "up":
			ctrl.PostKeyUp(int32(code)).Wait()
			log.Debug().Str("key", step.Key).Int64("t", step.T).Int("step", i).Msg("[InputReplay] key_up")
		default:
			log.Error().Str("action", step.Action).Msg("[InputReplay] Unknown action")
			releaseAll(ctrl)
			return false
		}
	}

	releaseAll(ctrl)
	log.Info().Msg("[InputReplay] Replay finished")
	return true
}

func parseParam(raw string) (*replayParam, error) {
	var param replayParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, fmt.Errorf("failed to parse: %w", err)
	}
	if len(param.Steps) == 0 {
		return nil, fmt.Errorf("steps is required and cannot be empty")
	}
	return &param, nil
}

func releaseAll(ctrl *maa.Controller) {
	ctrl.PostKeyUp(int32(keyW)).Wait()
	ctrl.PostKeyUp(int32(keyA)).Wait()
}
