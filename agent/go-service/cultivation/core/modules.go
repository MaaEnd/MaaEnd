package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TaskDocument is the part of the upstream interface contract used by adapters.
type TaskDocument struct {
	Task []struct {
		Name       string   `json:"name"`
		Entry      string   `json:"entry"`
		Controller []string `json:"controller"`
		Option     []string `json:"option"`
	} `json:"task"`
	Option map[string]OptionDefinition `json:"option"`
}

// OptionDefinition retains upstream patches rather than rebuilding module flows.
type OptionDefinition struct {
	Controller       []string           `json:"controller"`
	PipelineOverride map[string]any     `json:"pipeline_override"`
	Cases            []OptionDefinition `json:"cases"`
	Name             string             `json:"name"`
	Type             string             `json:"type"`
	// DefaultCase is a case name, or a list of names for a checkbox.
	DefaultCase json.RawMessage `json:"default_case"`
	// Option lists the sub-options a case enables.
	Option []string `json:"option"`
}

// ModulePlan is a data adapter, not an execution or consumption authorization.
type ModulePlan struct {
	Name       string           `json:"name"`
	Entry      string           `json:"entry"`
	Mode       string           `json:"mode"`
	Executable bool             `json:"executable"`
	Blockers   []string         `json:"blockers"`
	Targets    map[string]int64 `json:"targets,omitempty"`
	Patch      map[string]any   `json:"pipeline_override,omitempty"`
}

// ProgressRow reports comparable units; experience denominations share one row.
type ProgressRow struct {
	Key    string `json:"key"`
	Need   int64  `json:"need"`
	Owned  int64  `json:"owned"`
	Module string `json:"module"`
}

// ModulePreview binds dispatch data and observed progress to one saved revision.
type ModulePreview struct {
	Source          string        `json:"source"`
	Revision        uint64        `json:"revision"`
	Controller      string        `json:"controller"`
	ObservedAt      time.Time     `json:"observed_at"`
	Executable      bool          `json:"executable"`
	MaterialsReady  bool          `json:"materials_ready_at_scan"`
	CultivationDone bool          `json:"cultivation_done"`
	CompletionState string        `json:"completion_state"`
	Rows            []ProgressRow `json:"rows"`
	Modules         []ModulePlan  `json:"modules"`
	Manual          []Material    `json:"manual"`
	Weapon          *Weapon       `json:"weapon,omitempty"`
	Completion      *Completion   `json:"completion,omitempty"`
}

func optionCase(doc TaskDocument, option, name string) (map[string]any, error) {
	for _, c := range doc.Option[option].Cases {
		if c.Name == name && len(c.PipelineOverride) > 0 {
			// Each adapter owns its copy; never mutate a shared resource document.
			data, err := json.Marshal(c.PipelineOverride)
			if err != nil {
				return nil, err
			}
			var patch map[string]any
			if err = json.Unmarshal(data, &patch); err != nil {
				return nil, err
			}
			return patch, nil
		}
	}
	return nil, fmt.Errorf("missing upstream option %s/%s", option, name)
}

func expression(node any) (string, error) {
	m, ok := node.(map[string]any)
	if !ok {
		return "", errors.New("invalid collection node")
	}
	for _, key := range []string{"recognition", "param", "custom_recognition_param"} {
		m, ok = m[key].(map[string]any)
		if !ok {
			return "", fmt.Errorf("missing collection %s", key)
		}
	}
	s, ok := m["expression"].(string)
	if !ok {
		return "", errors.New("missing collection expression")
	}
	return s, nil
}

var collectItems = regexp.MustCompile(`\{(item_\w+)\}<\{AutoCollectLimit_(\w+)\}`)

// collectRouteItems accepts the upstream single-item and shared two-item forms.
func collectRouteItems(expr, route string) ([]string, error) {
	pairs := collectItems.FindAllStringSubmatch(expr, -1)
	if len(pairs) < 1 || len(pairs) > 2 {
		return nil, errors.New("changed collection expression shape")
	}
	var ids, limits, clauses []string
	for _, pair := range pairs {
		if !strings.HasPrefix(pair[2], route) {
			return nil, errors.New("collection limit/route mismatch")
		}
		ids = append(ids, pair[1])
		limit := "{AutoCollectLimit_" + pair[2] + "}"
		limits = append(limits, limit)
		clauses = append(clauses, fmt.Sprintf("(%s>0 && {%s}<%s)", limit, pair[1], limit))
	}
	expected := limits[0] + "==0 || {" + ids[0] + "}<" + limits[0]
	if len(pairs) == 2 {
		expected = "(" + limits[0] + "==0 && " + limits[1] + "==0) || " + strings.Join(clauses, " || ")
	}
	if expr != expected {
		return nil, errors.New("changed collection expression contract")
	}
	return ids, nil
}

// BuildModules maps one plan to the existing modules without submitting a task.
// CloudADB exclusions and pending review remain blockers in exported data.
func BuildModules(plan Plan, protocol, collect, essence, weapons TaskDocument) (ModulePreview, error) {
	goals, unsupported, err := BuildTargets(plan)
	if err != nil {
		return ModulePreview{}, err
	}
	rows, err := plan.Selected()
	if err != nil {
		return ModulePreview{}, err
	}
	out := ModulePreview{Source: SourceVersion, Revision: plan.Revision, Controller: "CloudADB", ObservedAt: plan.ReadAt,
		MaterialsReady: true, CompletionState: "materials_missing", Weapon: plan.ChosenWeapon()}
	stock := map[string]int64{}
	for _, row := range rows {
		if spec, ok := ItemTargets[row.ID]; ok {
			if row.Owned > (int64(^uint64(0)>>1)-stock[spec.Key])/spec.Weight {
				return ModulePreview{}, errors.New("stock overflow")
			}
			stock[spec.Key] += row.Owned * spec.Weight
		}
	}
	for _, key := range Targets {
		if goals[key] > 0 {
			out.Rows = append(out.Rows, ProgressRow{key, goals[key], stock[key], "ProtocolSpace"})
		}
	}
	mode, err := optionCase(protocol, "ProtocolSpaceMode", "TargetInventory")
	if err != nil {
		return out, err
	}
	limits := protocol.Option["SupplyPlanLimits"].PipelineOverride
	if len(limits) == 0 {
		return out, errors.New("missing SupplyPlan template")
	}
	raw, err := json.Marshal(limits)
	if err != nil {
		return out, err
	}
	patch, err := RenderOverrides(raw, goals)
	if err != nil {
		return out, err
	}
	for key, value := range patch {
		mode[key] = value
	}
	var met []string
	for _, key := range Targets {
		if goals[key] > 0 {
			expr, err := expression(patch["SupplyPlanRewardItemMet_"+key])
			if err != nil {
				return out, err
			}
			met = append(met, "("+expr+")")
		}
	}
	metExpression := "1==1"
	if len(met) > 0 {
		metExpression = strings.Join(met, "&&")
	}
	// Upstream AllTargetsMet is a DirectHit fall-through. A cultivation receipt
	// needs positive stock comparisons, not merely no matching deficit branch.
	mode["SupplyPlanAllTargetsMet"] = map[string]any{"recognition": map[string]any{"type": "Custom", "param": map[string]any{
		"custom_recognition": "ItemQuantitySatisfied", "custom_recognition_param": map[string]any{"expression": metExpression}}}}
	out.Modules = append(out.Modules, ModulePlan{Name: "ProtocolSpace", Entry: "ProtocolSpaceSchedule", Mode: "TargetInventory",
		Blockers: []string{"formal_review_pending", "device_validation_pending", "consumption_authorization_required"}, Targets: goals, Patch: mode})

	collectPatch, err := optionCase(collect, "AutoCollectMode", "TargetInventory")
	if err != nil {
		return out, err
	}
	// General collection falls back to unconditional selected routes if the
	// warehouse snapshot fails. Goal-directed cultivation must stop instead.
	collectPatch["AutoCollectInventoryFallback"] = map[string]any{"action": map[string]any{"type": "Custom", "param": map[string]any{"custom_action": "FalseAction"}}, "next": []string{}}
	routeByItem := map[string]string{}
	itemsByRoute := map[string][]string{}
	for node, value := range collect.Option["AutoCollectTargetInventory"].PipelineOverride {
		if !strings.HasSuffix(node, "SubGated") {
			continue
		}
		expr, err := expression(value)
		if err != nil {
			return out, err
		}
		route := strings.TrimSuffix(strings.TrimPrefix(node, "AutoCollect"), "SubGated")
		ids, err := collectRouteItems(expr, route)
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			if routeByItem[id] != "" {
				return out, errors.New("ambiguous collection route contract")
			}
			routeByItem[id] = route
		}
		itemsByRoute[route] = ids
	}
	if len(routeByItem) == 0 {
		return out, errors.New("missing collection routes")
	}
	collectTargets := map[string]int64{}
	for _, row := range unsupported {
		if routeByItem[row.ID] != "" {
			if row.Need > 2147483647 {
				return out, errors.New("collection goal exceeds upstream integer range")
			}
			collectTargets[row.ID] = row.Need
			out.Rows = append(out.Rows, ProgressRow{row.ID, row.Need, row.Owned, "AutoCollect"})
		} else {
			out.Manual = append(out.Manual, row)
			out.Rows = append(out.Rows, ProgressRow{row.ID, row.Need, row.Owned, "manual"})
		}
	}
	for route, ids := range itemsByRoute {
		base := "AutoCollect" + route
		var clauses []string
		for _, id := range ids {
			if need := collectTargets[id]; need > 0 {
				clauses = append(clauses, fmt.Sprintf("{%s}<%d", id, need))
			}
		}
		selected := len(clauses) > 0
		expr, skipped := "0==1", "1==1"
		if selected {
			expr = strings.Join(clauses, " || ")
			skipped = "!(" + expr + ")"
		}
		// Upstream zero means UNLIMITED. Disable unused routes explicitly and
		// use a positive-stock comparison, never substitute zero into that OR.
		collectPatch[base+"Sub"] = map[string]any{"enabled": false}
		collectPatch[base+"SubGated"] = map[string]any{"enabled": selected, "recognition": map[string]any{"param": map[string]any{
			"custom_recognition_param": map[string]any{"expression": expr}}}}
		collectPatch[base+"SubSkipped"] = map[string]any{"enabled": true, "recognition": map[string]any{"param": map[string]any{
			"custom_recognition_param": map[string]any{"expression": skipped}}}}
		for _, suffix := range []string{"Dispatch", "Start"} {
			collectPatch[base+suffix] = map[string]any{"enabled": selected}
		}
	}
	out.Modules = append(out.Modules, ModulePlan{Name: "AutoCollect", Entry: "AutoCollectSchedule", Mode: "TargetInventory",
		Blockers: []string{"upstream_target_inventory_excludes_CloudADB", "cloud_depot_sync_validation_pending", "formal_review_pending"}, Targets: collectTargets, Patch: collectPatch})

	essencePatch, err := optionCase(essence, "AutoEssenceMenu", "Target")
	if err != nil {
		return out, err
	}
	// Remove the upstream generic 'targets met OR task ended' focus. It is not
	// evidence of a successful cultivation goal and cannot become our receipt.
	delete(essencePatch, "AutoEssenceOnComplete")
	attach := map[string]any{}
	for _, option := range weapons.Option {
		for _, c := range option.Cases {
			if strings.HasPrefix(c.Name, "wpn_") {
				attach[c.Name] = false
			}
		}
	}
	if len(attach) == 0 {
		return out, errors.New("missing essence weapon options")
	}
	blockers := []string{"beta_excludes_CloudADB", "cloud_essence_validation_pending", "skill_levels_and_equipment_not_verified", "formal_review_pending", "consumption_authorization_required"}
	if weapon := plan.ChosenWeapon(); weapon == nil {
		blockers = append(blockers, "weapon_not_chosen")
	} else if weapon.EssenceSatisfied() {
		// A matching flawless essence is owned or a perfect fit is injected: nothing to farm.
		blockers = append(blockers, "essence_satisfied")
	} else if weapon.ID == "" || len(weapon.CandidateIDs) != 1 || weapon.CandidateIDs[0] != weapon.ID || attach[weapon.ID] == nil {
		blockers = append(blockers, "weapon_identity_confirmation_required")
	} else {
		attach[weapon.ID] = true
	}
	essencePatch["AutoEssenceTargetPlan"] = map[string]any{"attach": attach}
	out.Modules = append(out.Modules, ModulePlan{Name: "AutoEssence", Entry: "AutoEssenceSchedule", Mode: "Target", Blockers: blockers, Patch: essencePatch})
	for _, row := range out.Rows {
		if row.Owned < row.Need {
			out.MaterialsReady = false
		}
	}
	if weapon := plan.ChosenWeapon(); weapon != nil && !weapon.EssenceSatisfied() {
		out.MaterialsReady = false
	}
	if out.MaterialsReady {
		out.CompletionState = "materials_ready_cultivation_unverified"
	}
	if done := plan.Completion; done != nil {
		if done.Method != "user_confirmed" || done.Revision != plan.Revision || done.Header != plan.Header || done.At.Before(plan.ReadAt) {
			return out, errors.New("completion evidence does not bind to this plan")
		}
		out.CultivationDone = true
		out.CompletionState = "user_confirmed_done"
		out.Completion = done
	}
	sort.Slice(out.Rows, func(i, j int) bool { return out.Rows[i].Key < out.Rows[j].Key })
	return out, nil
}
