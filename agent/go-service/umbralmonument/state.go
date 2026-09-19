package umbralmonument

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const prefix = "UmbralMonument"

// StateAction resets task-local progress or records the current battle result.
// Navigation and combat remain in Pipeline; no controller operations occur here.
type StateAction struct{}

var _ maa.CustomActionRunner = &StateAction{}

type state struct {
	IncludeHard bool             `json:"include_hard"`
	Seasons     map[string]bool  `json:"seasons"`
	Attempts    map[string]uint8 `json:"attempts"`
	Passed      map[string]int   `json:"passed"`
	Stage       string           `json:"stage"`
	Hard        bool             `json:"hard"`
}

type params struct {
	Phase string `json:"phase"`
}

func load(ctx *maa.Context) (state, error) {
	raw, err := ctx.GetNodeJSON(prefix + "Main")
	if err != nil {
		return state{}, err
	}
	var node struct {
		Attach struct {
			IncludeHard bool  `json:"include_hard"`
			State       state `json:"state"`
		} `json:"attach"`
	}
	if err = json.Unmarshal([]byte(raw), &node); err != nil {
		return state{}, err
	}
	s := node.Attach.State
	s.IncludeHard = node.Attach.IncludeHard
	return s, nil
}

func save(ctx *maa.Context, s state, reset ...string) error {
	patch := map[string]any{prefix + "Main": map[string]any{
		"attach": map[string]any{"include_hard": s.IncludeHard, "state": s},
	}}
	for _, name := range reset {
		patch[prefix+name] = map[string]any{"attach": map[string]any{"ready": false}}
	}
	return ctx.OverridePipeline(patch)
}

func (a *StateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var p params
	err := json.Unmarshal([]byte(arg.CustomActionParam), &p)
	if err == nil {
		err = update(ctx, p.Phase)
	}
	if err != nil {
		log.Error().Err(err).Str("component", prefix).Msg("cannot update progress")
	}
	return err == nil
}

func update(ctx *maa.Context, phase string) error {
	s, err := load(ctx)
	if err != nil {
		return err
	}
	switch phase {
	case "reset":
		s = state{IncludeHard: s.IncludeHard, Seasons: map[string]bool{}, Attempts: map[string]uint8{}, Passed: map[string]int{}}
		if err = ctx.OverridePipeline(map[string]any{prefix + "Abort": map[string]any{"enabled": false}}); err != nil {
			return err
		}
		return save(ctx, s, "SeasonsStart", "SeasonsEnd", "StagesStart", "StagesEnd")
	case "success", "failure":
		if s.Stage == "" || s.Attempts == nil || s.Passed == nil {
			return fmt.Errorf("no selected stage")
		}
		s.finish(phase == "success")
		log.Info().Str("component", prefix).Str("stage", s.Stage).Bool("hard", s.Hard).Str("result", phase).Msg("battle finished")
		return save(ctx, s, "StagesEnd")
	default:
		return fmt.Errorf("unknown state phase %q", phase)
	}
}

func (s *state) finish(won bool) {
	if won {
		level := 1
		if s.Hard {
			level = 2
		}
		s.Passed[s.Stage] = max(s.Passed[s.Stage], level)
	} else if !s.Hard {
		// A failed normal stage must not be retried as hard during this run.
		s.Attempts[s.Stage] |= 2
	}
}

func nextDifficulty(completed int, attempted uint8, includeHard bool) (hard, found bool) {
	if completed == 0 && attempted&1 == 0 {
		return false, true
	}
	if completed == 1 && includeHard && attempted&2 == 0 {
		return true, true
	}
	return false, false
}
