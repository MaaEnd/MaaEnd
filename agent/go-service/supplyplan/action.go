package supplyplan

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const component = "SupplyPlanOperatorPlan"

// maxLimit matches the SupplyPlanLimits input cap, keeping cross products inside int64.
const maxLimit = 3000000000

// OperatorPlanAction turns the selected operators' fresh-to-max growth totals into
// target-inventory limits. Pipeline syncs IMS before this node and dispatches after it.
type OperatorPlanAction struct{}

var _ maa.CustomActionRunner = &OperatorPlanAction{}

// planParam holds generated per-operator route totals (see tools/pipeline-generate/SupplyPlanOperator).
type planParam struct {
	Targets map[string]map[string]int64 `json:"targets"`
}

// Run sums the operators checked in this node's attach and overrides every
// SupplyPlanInsufficient_* and SupplyPlanRewardItemMet_* expression.
func (a *OperatorPlanAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		log.Error().Str("component", component).Msg("got nil context or arg")
		return false
	}
	var param planParam
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse param")
		return false
	}
	raw, err := ctx.GetNodeJSON(arg.CurrentTaskName)
	if err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to read node attach")
		return false
	}
	var node struct {
		Attach map[string]json.RawMessage `json:"attach"`
	}
	if err := json.Unmarshal([]byte(raw), &node); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse node attach")
		return false
	}
	selected := selectedOperators(node.Attach)
	// Readings exist only when the optional daily red-label check ran in this task.
	checks := cultivation.Checked(arg.TaskID)
	totals, err := sumTargets(applyRedChecks(param.Targets, checks), selected)
	if err != nil {
		log.Error().Err(err).Str("component", component).Strs("operators", selected).Msg("invalid operator targets")
		maafocus.Print(ctx, i18n.T("supplyplan.operator_plan_failed"))
		return false
	}
	if len(selected) == 0 {
		// No operator checked: keep the manual SupplyPlanLimits values.
		return true
	}
	limits := make(map[string]string, len(routes))
	for _, r := range routes {
		limits[r.Key] = strconv.FormatInt(totals[r.Key], 10)
	}
	patch := map[string]any{}
	for name, expr := range expressions(limits) {
		patch[name] = map[string]any{"recognition": map[string]any{"param": map[string]any{
			"custom_recognition_param": map[string]any{"expression": expr},
		}}}
	}
	if err := ctx.OverridePipeline(patch); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to override supply plan limits")
		return false
	}
	text := summary(selected, totals, ims.ItemsSnapshot(), checks)
	maafocus.Print(ctx, text)
	log.Info().Str("component", component).Strs("operators", selected).Interface("limits", totals).Str("summary", text).Msg("operator growth totals applied to target inventory")
	return true
}

// selectedOperators reads checkbox cases, each writing {"<Operator>": true} into attach.
func selectedOperators(attach map[string]json.RawMessage) []string {
	var out []string
	for name, value := range attach {
		var on bool
		if json.Unmarshal(value, &on) == nil && on {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// sumTargets adds the selected operators' totals; routes absent from all of them stay 0,
// which the dispatch expressions treat as not planned.
func sumTargets(targets map[string]map[string]int64, selected []string) (map[string]int64, error) {
	known := map[string]bool{}
	for _, r := range routes {
		known[r.Key] = true
	}
	totals := map[string]int64{}
	for _, name := range selected {
		operator, ok := targets[name]
		if !ok {
			return nil, fmt.Errorf("no generated targets for operator %s", name)
		}
		for key, n := range operator {
			if !known[key] || n < 0 {
				return nil, fmt.Errorf("operator %s has invalid route %s=%d", name, key, n)
			}
			if totals[key] > maxLimit-n {
				return nil, errors.New("route total exceeds the target-inventory limit")
			}
			totals[key] += n
		}
	}
	return totals, nil
}

var labelImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)

// summary lists total, owned and remaining amount for every planned route.
func summary(selected []string, totals map[string]int64, items map[string]int, checks map[string]cultivation.Red) string {
	names := make([]string, len(selected))
	for i, name := range selected {
		names[i] = i18n.T("operator." + name)
	}
	lines := []string{i18n.T("supplyplan.operator_plan", strings.Join(names, i18n.Separator()))}
	for i, name := range selected {
		red, ok := checks[name]
		switch {
		case !ok:
		case red.Failed:
			lines = append(lines, i18n.T("supplyplan.red_check_failed", names[i]))
		default:
			lines = append(lines, i18n.T("supplyplan.red_check_result", names[i], len(red.Routes)))
		}
	}
	for _, r := range routes {
		total := totals[r.Key]
		if total == 0 {
			continue
		}
		owned := r.owned(items)
		label := strings.TrimSpace(labelImage.ReplaceAllString(i18n.T(r.Label), ""))
		lines = append(lines, i18n.T("supplyplan.operator_plan_row", label, total, owned, max(0, total-owned)))
	}
	return strings.Join(lines, "\n")
}
