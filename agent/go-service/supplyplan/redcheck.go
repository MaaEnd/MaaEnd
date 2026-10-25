package supplyplan

import (
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	redCheckComponent = "SupplyPlanRedCheckNext"
	planNode          = "SupplyPlanOperatorPlan"
	routeNode         = "SupplyPlanRedCheckRoute"
)

// RedCheckNextAction reads, at most once per game day, which routes each checked
// operator still shows in red on the cultivation overview. It only chooses the next
// operator and patches the reader's identity nodes; Pipeline runs the reader.
type RedCheckNextAction struct{}

var _ maa.CustomActionRunner = &RedCheckNextAction{}

// identity locates one operator on the cultivation overview.
type identity struct {
	Templates []string `json:"templates"`
	Name      string   `json:"name"`
}

type redCheckParam struct {
	Operators map[string]identity `json:"operators"`
}

var (
	attemptMu sync.Mutex
	attempts  = map[int64]map[string]bool{}
)

// attempt reports whether name was already sent to the reader in this task,
// so a reader exit that records nothing can never loop.
func attempt(taskID int64, name string) bool {
	attemptMu.Lock()
	defer attemptMu.Unlock()
	if attempts[taskID] == nil {
		attempts = map[int64]map[string]bool{taskID: {}}
	}
	if attempts[taskID][name] {
		return true
	}
	attempts[taskID][name] = true
	return false
}

// Run sends the next unchecked operator to the reader, or continues to IMS sync and planning.
func (a *RedCheckNextAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", redCheckComponent).Msg("got nil context or arg")
		return false
	}
	var param redCheckParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", redCheckComponent).Msg("failed to parse param")
		return false
	}
	raw, err := ctx.GetNodeJSON(planNode)
	if err != nil {
		log.Error().Err(err).Str("component", redCheckComponent).Msg("failed to read selected operators")
		return false
	}
	var node struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		log.Error().Err(err).Str("component", redCheckComponent).Msg("failed to parse selected operators")
		return false
	}
	checked := cultivation.Checked(arg.TaskID)
	now := time.Now()
	for _, name := range selectedOperators(node.Attach) {
		if _, ok := checked[name]; ok {
			continue
		}
		if red, ok := cultivation.Cached(name, now); ok {
			cultivation.Record(arg.TaskID, name, red)
			continue
		}
		who, ok := param.Operators[name]
		if !ok || len(who.Templates) == 0 || who.Name == "" || attempt(arg.TaskID, name) {
			// No portrait, or the reader already ran without a result: plan this operator unfiltered.
			cultivation.Record(arg.TaskID, name, cultivation.Red{Failed: true, Day: cultivation.GameDay(now)})
			continue
		}
		cultivation.SetTarget(arg.TaskID, name)
		if err := ctx.OverridePipeline(readerPatch(who)); err != nil {
			log.Error().Err(err).Str("component", redCheckComponent).Str("operator", name).Msg("failed to target cultivation reader")
			return false
		}
		maafocus.Print(ctx, i18n.T("supplyplan.red_check_reading", i18n.T("operator."+name)))
		log.Info().Str("component", redCheckComponent).Str("operator", name).Msg("reading red cultivation labels")
		return true
	}
	if err := ctx.OverridePipeline(map[string]any{routeNode: map[string]any{
		"next": []string{"[JumpBack]SyncItemData", planNode},
	}}); err != nil {
		log.Error().Err(err).Str("component", redCheckComponent).Msg("failed to continue to planning")
		return false
	}
	return true
}

// readerPatch selects one operator in the cultivation reader, mirroring a fixed operator choice.
func readerPatch(who identity) map[string]any {
	return map[string]any{
		"CultivationInOverview": map[string]any{
			"action": map[string]any{"type": "Custom", "param": map[string]any{
				"custom_action": "PipelineOverrideAction",
				"custom_action_param": map[string]any{"patch": map[string]any{
					"CultivationOperatorTop": map[string]any{"attach": map[string]any{"ready": false}},
					"CultivationOperatorEnd": map[string]any{"attach": map[string]any{"ready": false}},
				}},
			}},
			"next": []string{"CultivationOperatorCheckUp"},
		},
		"CultivationOperatorSelected": map[string]any{
			"enabled": true,
			"recognition": map[string]any{"type": "OCR", "param": map[string]any{
				"roi": []int{480, 85, 300, 40}, "expected": []string{who.Name},
			}},
		},
		"CultivationOperatorPortrait": map[string]any{
			"enabled": true,
			"recognition": map[string]any{"type": "TemplateMatch", "param": map[string]any{
				"roi": []int{30, 100, 350, 520}, "template": slices.Clone(who.Templates), "threshold": 0.8,
			}},
		},
		routeNode: map[string]any{"next": []string{"CultivationScan"}},
	}
}

// experienceRoutes are never filtered: merged experience-card labels are not reliably
// identified, so a missing red reading there cannot prove the operator has enough.
var experienceRoutes = map[string]bool{"COGNITIVE_CARRIER_EXP": true, "COMBAT_RECORD_EXP": true}

// applyRedChecks keeps, for each successfully read operator, only routes still shown in red.
// Operators without a reading (disabled, failed or no portrait) keep their full totals.
func applyRedChecks(targets map[string]map[string]int64, checks map[string]cultivation.Red) map[string]map[string]int64 {
	out := make(map[string]map[string]int64, len(targets))
	for name, routesOf := range targets {
		red, ok := checks[name]
		if !ok || red.Failed {
			out[name] = routesOf
			continue
		}
		kept := map[string]int64{}
		for key, n := range routesOf {
			if experienceRoutes[key] || slices.Contains(red.Routes, key) {
				kept[key] = n
			}
		}
		out[name] = kept
	}
	return out
}
