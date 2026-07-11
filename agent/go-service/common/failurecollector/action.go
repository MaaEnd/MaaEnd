package failurecollector

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type actionParam struct {
	Key        string `json:"key"`
	Name       string `json:"name,omitempty"`
	NameKey    string `json:"name_key,omitempty"`
	ItemKey    string `json:"item_key,omitempty"`
	SummaryKey string `json:"summary_key,omitempty"`
}

type ResetAction struct{}
type SetCurrentAction struct{}
type RecordAction struct{}
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

func (a *SetCurrentAction) Run(_ *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg)
	if !ok || (param.Name == "" && param.NameKey == "") {
		return false
	}
	if param.NameKey != "" {
		param.Name = i18n.InterfaceT(param.NameKey)
	}
	SetCurrent(param.Key, param.Name)
	return true
}

func (a *RecordAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg)
	if !ok {
		return false
	}
	failed := RecordCurrent(param.Key)
	if failed == "" {
		log.Error().Str("key", param.Key).Msg("FailureCollector has no current item")
		return false
	}
	if param.ItemKey != "" {
		maafocus.Print(ctx, i18n.T(param.ItemKey, failed))
	}
	return true
}

func (a *FinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param, ok := parseParam(arg)
	if !ok {
		return false
	}
	failures := Finish(param.Key)
	if len(failures) == 0 {
		return true
	}
	if param.SummaryKey != "" {
		maafocus.Print(ctx, i18n.T(param.SummaryKey, strings.Join(failures, i18n.Separator())))
	}
	return false
}
