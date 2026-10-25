package supplyplan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/cultivation"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"
)

func readJSONC(t *testing.T, path string, out any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(jsonclean.Clean(raw), out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// The rebuilt expressions must equal the shipped SupplyPlanLimits overrides
// character for character, so an upstream formula change fails here first.
func TestExpressionsMatchSupplyPlanLimits(t *testing.T) {
	var task struct {
		Option map[string]struct {
			Inputs []struct {
				Name string `json:"name"`
			} `json:"inputs"`
			PipelineOverride map[string]struct {
				Recognition struct {
					Param struct {
						Custom struct {
							Expression string `json:"expression"`
						} `json:"custom_recognition_param"`
					} `json:"param"`
				} `json:"recognition"`
			} `json:"pipeline_override"`
		} `json:"option"`
	}
	readJSONC(t, "../../../assets/tasks/ProtocolSpace.json", &task)
	option := task.Option["SupplyPlanLimits"]
	if len(option.Inputs) != len(routes) {
		t.Fatalf("SupplyPlanLimits has %d inputs, planner knows %d routes", len(option.Inputs), len(routes))
	}
	placeholders := map[string]string{}
	for i, r := range routes {
		if want := "SupplyPlanLimit_" + r.Key; option.Inputs[i].Name != want {
			t.Fatalf("input %d is %s, want %s", i, option.Inputs[i].Name, want)
		}
		placeholders[r.Key] = "{SupplyPlanLimit_" + r.Key + "}"
	}
	got := expressions(placeholders)
	if len(got) != len(option.PipelineOverride) {
		t.Fatalf("rebuilt %d nodes, option overrides %d", len(got), len(option.PipelineOverride))
	}
	for name, node := range option.PipelineOverride {
		if got[name] != node.Recognition.Param.Custom.Expression {
			t.Errorf("%s differs:\n got %s\nwant %s", name, got[name], node.Recognition.Param.Custom.Expression)
		}
	}
}

// Every generated operator must only use routes the planner can override.
func TestGeneratedTargetsUseKnownRoutes(t *testing.T) {
	var pipeline struct {
		Plan struct {
			Action struct {
				Param struct {
					Custom planParam `json:"custom_action_param"`
				} `json:"param"`
			} `json:"action"`
		} `json:"SupplyPlanOperatorPlan"`
	}
	readJSONC(t, "../../../assets/resource/pipeline/SupplyPlan/SupplyPlanOperator.json", &pipeline)
	targets := pipeline.Plan.Action.Param.Custom.Targets
	if len(targets) == 0 {
		t.Fatal("no generated operator targets")
	}
	for name := range targets {
		if _, err := sumTargets(targets, []string{name}); err != nil {
			t.Error(err)
		}
	}
}

func TestSumTargetsAddsOperatorsAndRejectsUnknown(t *testing.T) {
	targets := map[string]map[string]int64{
		"Si":        {"PROTOPRISM": 461, "TACHYON_SCREENING_LATTICE": 116},
		"Laevatain": {"PROTOPRISM": 461, "COMBAT_RECORD_EXP": 747110},
	}
	got, err := sumTargets(targets, []string{"Laevatain", "Si"})
	if err != nil {
		t.Fatal(err)
	}
	if got["PROTOPRISM"] != 922 || got["TACHYON_SCREENING_LATTICE"] != 116 || got["COMBAT_RECORD_EXP"] != 747110 || got["T_CREDS"] != 0 {
		t.Fatalf("unexpected totals %v", got)
	}
	if _, err := sumTargets(targets, []string{"Nobody"}); err == nil {
		t.Fatal("accepted an operator without generated targets")
	}
	if _, err := sumTargets(map[string]map[string]int64{"X": {"PLANT": 1}}, []string{"X"}); err == nil {
		t.Fatal("accepted a route the target-inventory mode cannot farm")
	}
	if _, err := sumTargets(map[string]map[string]int64{"A": {"T_CREDS": maxLimit}, "B": {"T_CREDS": 1}}, []string{"A", "B"}); err == nil {
		t.Fatal("accepted a total above the input cap")
	}
}

func TestSelectedOperatorsReadsCheckedCasesOnly(t *testing.T) {
	attach := map[string]json.RawMessage{"Si": json.RawMessage("true"), "Laevatain": json.RawMessage("false"), "note": json.RawMessage(`"x"`)}
	got := selectedOperators(attach)
	if len(got) != 1 || got[0] != "Si" {
		t.Fatalf("selected %v", got)
	}
}

func TestOwnedUsesUpstreamExperienceWeights(t *testing.T) {
	items := map[string]int{"item_expcard_stage1_high": 2, "item_expcard_stage1_mid": 3, "item_expcard_stage1_low": 5}
	for _, r := range routes {
		if r.Key == "COMBAT_RECORD_EXP" && r.owned(items) != 2*10000+3*1000+5*200 {
			t.Fatalf("owned %d", r.owned(items))
		}
	}
}

func TestApplyRedChecksKeepsRedRoutesAndExperience(t *testing.T) {
	targets := map[string]map[string]int64{
		"Si":        {"PROTOPRISM": 461, "TACHYON_SCREENING_LATTICE": 116, "COMBAT_RECORD_EXP": 747110},
		"Laevatain": {"PROTOPRISM": 461, "PROTOSET": 60},
		"Rossi":     {"PROTOPRISM": 461},
	}
	checks := map[string]cultivation.Red{
		"Si":        {Routes: []string{"TACHYON_SCREENING_LATTICE"}},
		"Laevatain": {Failed: true},
	}
	got := applyRedChecks(targets, checks)
	if len(got["Si"]) != 2 || got["Si"]["TACHYON_SCREENING_LATTICE"] != 116 || got["Si"]["COMBAT_RECORD_EXP"] != 747110 {
		t.Fatalf("Si should keep its red route and experience only: %v", got["Si"])
	}
	if len(got["Laevatain"]) != 2 || len(got["Rossi"]) != 1 {
		t.Fatalf("failed or unread operators must keep full totals: %v", got)
	}
	if len(targets["Si"]) != 3 {
		t.Fatal("generated targets were modified in place")
	}
}
