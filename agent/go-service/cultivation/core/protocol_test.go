package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"
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

func TestProtocolSpaceOverrideUpstream(t *testing.T) {
	// The real upstream task and its shared options, resolved like the client.
	read := func(path string, out any) {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "assets", path))
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(jsonclean.Clean(data), out); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	var doc TaskDocument
	read("tasks/ProtocolSpace.json", &doc)
	var iface struct {
		Import []string `json:"import"`
	}
	read("interface.json", &iface)
	for _, path := range iface.Import {
		var other TaskDocument
		read(path, &other)
		for name, option := range other.Option {
			if _, ok := doc.Option[name]; !ok {
				doc.Option[name] = option
			}
		}
	}
	module := ModulePlan{Entry: "ProtocolSpaceSchedule", Patch: map[string]any{}}
	got, err := ProtocolSpaceOverride(doc, module, map[string]string{
		"ProtocolSpaceMode": "TargetInventory", "ProtocolSpaceObtainModeClaim": "ObtainScaling1", "ProtocolSpaceUseSpMedication": "EndTask"})
	if err != nil {
		t.Fatal(err)
	}
	field := func(node, key string) any {
		m, _ := got[node].(map[string]any)
		return m[key]
	}
	attach, _ := field("ProtocolSpaceScheduleEnabled", "attach").(map[string]any)
	if len(attach) != 7 {
		t.Fatalf("all weekdays must stay scheduled: %v", attach)
	}
	if next, _ := field("ProtocolSpaceScheduleEnabled", "next").([]any); len(next) != 1 || next[0] != "SupplyPlanMain" {
		t.Fatalf("TargetInventory must route to SupplyPlan: %v", next)
	}
	for _, node := range []string{"ProtocolSpacePopupSanityLowUseSpMedicationPrepare", "ProtocolSpaceRewardClaimRestoreSanityUseMedication", "ProtocolSpaceRewardUsePermit"} {
		if field(node, "enabled") != false {
			t.Fatalf("%s must be disabled by the safe defaults", node)
		}
	}
}
