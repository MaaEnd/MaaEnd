package charactercontroller

import (
	"encoding/json"
	"time"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	searchInterval = 1000 * time.Millisecond
	searchStepMs   = 100

	axisForward  = "__CharacterControllerAxisLongPressForwardAction"
	axisBackward = "__CharacterControllerAxisLongPressBackwardAction"
	axisLeft     = "__CharacterControllerAxisLongPressLeftAction"
	axisRight    = "__CharacterControllerAxisLongPressRightAction"
)

// Fixed circle path: forward 2, left 2, down 4, right 4, up 4, left 2.
// Recognition runs after every two steps.
var characterSearchPath = []string{
	axisForward, axisForward,
	axisLeft, axisLeft,
	axisBackward, axisBackward, axisBackward, axisBackward,
	axisRight, axisRight, axisRight, axisRight,
	axisForward, axisForward, axisForward, axisForward,
	axisLeft, axisLeft,
}

type characterSearchParam struct {
	WaitNodes []string `json:"wait_nodes"`
}

// CharacterSearchAction walks a fixed WASD circle and searches for wait nodes.
type CharacterSearchAction struct{}

var _ maa.CustomActionRunner = &CharacterSearchAction{}

func (a *CharacterSearchAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p characterSearchParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &p); err != nil {
		log.Error().
			Err(err).
			Str("component", "CharacterSearchAction").
			Msg("failed to parse params")
		return false
	}

	if len(p.WaitNodes) == 0 {
		log.Error().
			Str("component", "CharacterSearchAction").
			Msg("wait_nodes is required")
		return false
	}

	ctrl := ctx.GetTasker().GetController()

	for i, entry := range characterSearchPath {
		if ctx.GetTasker().Stopping() {
			return false
		}

		override := map[string]any{
			entry: map[string]any{
				"duration": searchStepMs,
			},
		}
		ctx.RunAction(entry, maa.Rect{0, 0, 0, 0}, "", override)

		if (i+1)%2 != 0 {
			continue
		}

		time.Sleep(searchInterval)

		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil || img == nil {
			log.Warn().
				Err(err).
				Str("component", "CharacterSearchAction").
				Int("step", i+1).
				Msg("cache image failed")
			continue
		}

		found := false
		for _, node := range p.WaitNodes {
			if detail, err := ctx.RunRecognition(node, img); err == nil && detail != nil && detail.Hit {
				found = true
				break
			}
		}
		if found {
			log.Info().
				Str("component", "CharacterSearchAction").
				Int("step", i+1).
				Msg("target found")
			return true
		}
	}

	log.Info().
		Str("component", "CharacterSearchAction").
		Msg("target not found after circle search")
	return false
}
