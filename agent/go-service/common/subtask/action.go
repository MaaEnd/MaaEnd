package subtask

import (
	"encoding/json"
	"math/rand"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type subTaskParam struct {
	Sub          []string `json:"sub"`
	Continue     *bool    `json:"continue,omitempty"`
	Strict       *bool    `json:"strict,omitempty"`
	RandomChoice *int     `json:"random_choice,omitempty"`
}

type SubTaskAction struct{}

// Compile-time interface check
var _ maa.CustomActionRunner = &SubTaskAction{}

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

	continueOnSubFailure := false
	if params.Continue != nil {
		continueOnSubFailure = *params.Continue
	}

	failActionOnSubFailure := true
	if params.Strict != nil {
		failActionOnSubFailure = *params.Strict
	}

	subTasks := params.Sub

	// Keep only the sub tasks whose node is resolvable and enabled
	{
		enabled := make([]string, 0, len(subTasks))
		for _, taskName := range subTasks {
			if taskName == "" {
				continue
			}
			node, err := ctx.GetNode(taskName)
			if err != nil {
				log.Warn().
					Err(err).
					Str("task", taskName).
					Msg("SubTask failed to get sub task node, skipping it")
				continue
			}
			if node.Enabled == nil || *node.Enabled {
				enabled = append(enabled, taskName)
			}
		}
		subTasks = enabled
	}

	// If random choice is specified
	if params.RandomChoice != nil && *params.RandomChoice > 0 {
		count := min(*params.RandomChoice, len(subTasks))
		shuffled := make([]string, len(subTasks))
		copy(shuffled, subTasks)
		rand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		subTasks = shuffled[:count]
		log.Info().
			Int("random_choice", *params.RandomChoice).
			Int("picked_count", count).
			Strs("tasks", subTasks).
			Msg("SubTask randomly picked sub tasks to run")
	}

	if len(subTasks) == 0 {
		log.Error().Msg("SubTask has no resolvable and enabled sub tasks to run")
		return false
	}

	hasSubFailure := false

	// Sequentially run the filtered sub tasks
	for i, taskName := range subTasks {
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

		detail, err := ctx.RunTask(taskName)

		if err != nil || detail == nil {
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

		if !detail.Status.Success() {
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
