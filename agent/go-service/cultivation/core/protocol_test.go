package core

import (
	"encoding/json"
	"testing"
)

func TestProtocolSpaceOverride(t *testing.T) {
	// The shape of the upstream ProtocolSpace task: a checkbox schedule, a mode
	// with nested options, and the medication choice under the claim mode.
	var doc TaskDocument
	err := json.Unmarshal([]byte(`{
		"task": [{"name": "ProtocolSpace", "entry": "ProtocolSpaceSchedule", "option": ["Schedule", "Mode"]}],
		"option": {
			"Schedule": {"type": "checkbox", "default_case": ["Mon", "Tue"], "cases": [
				{"name": "Mon", "pipeline_override": {"Enabled": {"attach": {"monday": true}}}},
				{"name": "Tue", "pipeline_override": {"Enabled": {"attach": {"tuesday": true}}}}]},
			"Mode": {"type": "select", "default_case": "ByCount", "cases": [
				{"name": "ByCount", "pipeline_override": {"Enabled": {"next": ["ByCount"]}}},
				{"name": "TargetInventory", "option": ["Claim", "Limits"], "pipeline_override": {"Enabled": {"next": ["SupplyPlanMain"]}}}]},
			"Claim": {"type": "select", "default_case": "Double", "cases": [
				{"name": "Double", "option": ["Medication"], "pipeline_override": {"Permit": {"enabled": true}}},
				{"name": "Single", "option": ["Medication"], "pipeline_override": {"Permit": {"enabled": false}}}]},
			"Medication": {"type": "select", "default_case": "Use", "cases": [
				{"name": "End", "pipeline_override": {"Medicine": {"enabled": false}}},
				{"name": "Use", "pipeline_override": {"Medicine": {"enabled": true}}}]},
			"Limits": {"type": "input"}
		}}`), &doc)
	if err != nil {
		t.Fatal(err)
	}
	module := ModulePlan{Entry: "ProtocolSpaceSchedule", Patch: map[string]any{"Limit_T_CREDS": map[string]any{"expression": "x"}}}
	got, err := ProtocolSpaceOverride(doc, module, map[string]string{"Mode": "TargetInventory", "Claim": "Single", "Medication": "End"})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(got)
	want := `{"Enabled":{"attach":{"monday":true,"tuesday":true},"next":["SupplyPlanMain"]},"Limit_T_CREDS":{"expression":"x"},"Medicine":{"enabled":false},"Permit":{"enabled":false}}`
	if string(data) != want {
		t.Fatalf("got  %s\nwant %s", data, want)
	}
	if _, err = ProtocolSpaceOverride(doc, module, map[string]string{"Mode": "TargetInventory", "Claim": "Triple"}); err == nil {
		t.Fatal("an unknown case must be rejected")
	}
}
