package subtask

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type subTaskParam struct {
	Sub      []string `json:"sub"`
	Continue *bool    `json:"continue,omitempty"`
	Strict   *bool    `json:"strict,omitempty"`
}

type SubTaskAction struct{}

func (a *SubTaskAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Msg("SubTask got nil custom action arg")
		return false
	}

	var params subTaskParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("param", arg.CustomActionParam).
			Msg("SubTask failed to parse custom_action_param")
		return false
	}

	if len(params.Sub) == 0 {
		log.Error().Msg("SubTask requires non-empty custom_action_param.sub")
		return false
	}

	continueOnSubFailure := false
	if params.Continue != nil {
		continueOnSubFailure = *params.Continue
	}

	failActionOnSubFailure := true
	if params.Strict != nil {
		failActionOnSubFailure = *params.Strict
	}

	hasSubFailure := false

	for i, taskName := range params.Sub {
		if taskName == "" {
			log.Error().
				Int("index", i).
				Msg("SubTask received empty task name in custom_action_param.sub")
			hasSubFailure = true
			if !continueOnSubFailure {
				break
			}
			continue
		}

		if _, err := ctx.RunTask(taskName); err != nil {
			log.Error().
				Err(err).
				Int("index", i).
				Str("task", taskName).
				Msg("SubTask failed to run sub task")
			hasSubFailure = true
			if !continueOnSubFailure {
				break
			}
		}
	}

	if hasSubFailure && failActionOnSubFailure {
		return false
	}

	return true
}
