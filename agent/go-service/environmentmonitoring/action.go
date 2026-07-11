package environmentmonitoring

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type routeParam struct {
	Name string `json:"name"`
}

type runState struct {
	current  string
	failures []string
}

var states = struct {
	sync.Mutex
	byContext map[string]runState
}{byContext: make(map[string]runState)}

type ResetAction struct{}
type SetCurrentRouteAction struct{}
type RecordFailureAction struct{}
type FinishAction struct{}

var (
	_ maa.CustomActionRunner = (*ResetAction)(nil)
	_ maa.CustomActionRunner = (*SetCurrentRouteAction)(nil)
	_ maa.CustomActionRunner = (*RecordFailureAction)(nil)
	_ maa.CustomActionRunner = (*FinishAction)(nil)
)

func contextKey(ctx *maa.Context) string {
	return fmt.Sprintf("%p", ctx)
}

func (a *ResetAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	states.Lock()
	states.byContext[contextKey(ctx)] = runState{}
	states.Unlock()
	return true
}

func (a *SetCurrentRouteAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg == nil {
		log.Error().Msg("EnvironmentMonitoringSetCurrentRoute received nil argument")
		return false
	}

	var param routeParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil || param.Name == "" {
		log.Error().Err(err).Str("param", arg.CustomActionParam).Msg("EnvironmentMonitoringSetCurrentRoute received invalid parameters")
		return false
	}

	key := contextKey(ctx)
	states.Lock()
	state := states.byContext[key]
	state.current = param.Name
	states.byContext[key] = state
	states.Unlock()
	return true
}

func (a *RecordFailureAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	key := contextKey(ctx)
	states.Lock()
	state := states.byContext[key]
	failedRoute := state.current
	if failedRoute != "" {
		state.failures = append(state.failures, failedRoute)
		state.current = ""
		states.byContext[key] = state
	}
	states.Unlock()

	if failedRoute == "" {
		log.Error().Msg("environment monitoring failed without a current route")
		return false
	}

	maafocus.Print(ctx, i18n.T("environmentmonitoring.route_failed", failedRoute))
	return true
}

func (a *FinishAction) Run(ctx *maa.Context, _ *maa.CustomActionArg) bool {
	key := contextKey(ctx)
	states.Lock()
	state := states.byContext[key]
	delete(states.byContext, key)
	states.Unlock()

	if len(state.failures) == 0 {
		return true
	}

	maafocus.Print(ctx, i18n.T("environmentmonitoring.failed_summary", strings.Join(state.failures, i18n.Separator())))
	return false
}
