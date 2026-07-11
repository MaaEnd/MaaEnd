package failurecollector

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type actionParam struct {
	Key          string `json:"key"`
	Task         string `json:"task,omitempty"`
	RecoveryTask string `json:"recovery_task,omitempty"`
	FailureTask  string `json:"failure_task,omitempty"`
}

type ResetAction struct{}
type RunTaskAction struct{}
type FinishAction struct{}

func parseParam(arg *maa.CustomActionArg) (actionParam, bool) {
	if arg == nil {
		return actionParam{}, false
	}
	var param actionParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil || param.Key == "" {
		log.Error().Err(err).Str("param", arg.CustomActionParam).Msg("FailureCollector received invalid parameters")
		return actionParam{}, false
	}
	return param, true
}

func (a *ResetAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg)
	if !ok {
		return false
	}
	Reset(param.Key)
	return true
}

func (a *RunTaskAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg)
	if !ok || param.Task == "" || param.FailureTask == "" {
		return false
	}
	node, err := ctx.GetNode(param.Task)
	if err != nil {
		log.Error().Err(err).Str("task", param.Task).Msg("FailureCollector failed to get subtask node")
		return false
	}
	if node.Enabled != nil && !*node.Enabled {
		return true
	}
	detail, err := ctx.RunTask(param.Task)
	if err == nil && detail != nil && detail.Status.Success() {
		return true
	}
	Record(param.Key, param.FailureTask)
	log.Error().Err(err).Str("task", param.Task).Msg("FailureCollector subtask failed")
	if param.RecoveryTask != "" {
		recovery, recoveryErr := ctx.RunTask(param.RecoveryTask)
		if recoveryErr != nil || recovery == nil || !recovery.Status.Success() {
			log.Error().Err(recoveryErr).Str("task", param.RecoveryTask).Msg("FailureCollector recovery subtask failed")
		}
	}
	return true
}

func (a *FinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg)
	if !ok {
		return false
	}
	failures := Finish(param.Key)
	for _, failureTask := range failures {
		detail, err := ctx.RunTask(failureTask)
		if err != nil || detail == nil || !detail.Status.Success() {
			log.Error().Err(err).Str("task", failureTask).Msg("FailureCollector failure notification task failed")
		}
	}
	return len(failures) == 0
}
