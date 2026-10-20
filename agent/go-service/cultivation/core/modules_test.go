package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/jsonclean"
)

func upstreamDocuments(t *testing.T) [4]TaskDocument {
	t.Helper()
	var docs [4]TaskDocument
	for index, path := range []string{"ProtocolSpace.json", "AutoCollect.json", "AutoEssence/AutoEssence.json", "AutoEssence/Target/Target.json"} {
		raw, err := os.ReadFile(filepath.Join("../../../..", "assets/tasks", path))
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(jsonclean.Clean(raw), &docs[index]); err != nil {
			t.Fatal(err)
		}
	}
	return docs
}

func exampleModulePlan() Plan {
	return Plan{Version: 1, Source: SourceVersion, Revision: 7, ReadAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
		Header: Header{"管理员", "卓越"}, Materials: []Material{
			{"item_char_skill_level_1_6", "协议棱柱", 80, 30},
			{"item_plant_crylplant_2_1", "采集材料", 6, 1},
			{"unmapped", "人工材料", 2, 0},
		}, Weapon: &Weapon{Name: "宏愿", CandidateIDs: []string{"wpn_sword_0021", "wpn_sword_0021_ink"}}}
}

func TestModuleAdaptersAgainstUpstream(t *testing.T) {
	docs := upstreamDocuments(t)
	plan := exampleModulePlan()
	got, err := BuildModules(plan, docs[0], docs[1], docs[2], docs[3])
	if err != nil {
		t.Fatal(err)
	}
	if got.Executable || got.CultivationDone || got.MaterialsReady || len(got.Modules) != 3 || len(got.Manual) != 1 || got.Revision != 7 {
		t.Fatalf("unexpected receipt: %+v", got)
	}
	if got.Modules[0].Targets["PROTOPRISM"] != 80 || len(got.Modules[0].Targets) != 16 {
		t.Fatal("protocol requires full total-stock goals")
	}
	met, err := expression(got.Modules[0].Patch["SupplyPlanAllTargetsMet"])
	if err != nil || !strings.Contains(met, "item_char_skill_level_1_6") {
		t.Fatal("protocol completion retained an unconditional fall-through")
	}
	collect := got.Modules[1]
	fallback := collect.Patch["AutoCollectInventoryFallback"].(map[string]any)
	if fallback["action"].(map[string]any)["param"].(map[string]any)["custom_action"] != "FalseAction" {
		t.Fatal("stock sync failure can fall back to unconditional farming")
	}
	if collect.Targets["item_plant_crylplant_2_1"] != 6 || !slices.Contains(collect.Blockers, "upstream_target_inventory_excludes_CloudADB") {
		t.Fatal("collection target/controller boundary lost")
	}
	selected := 0
	for node, patch := range collect.Patch {
		if strings.HasSuffix(node, "SubGated") {
			m := patch.(map[string]any)
			if m["enabled"] == true {
				selected++
				if node != "AutoCollectRoute1SubGated" {
					t.Fatalf("unrequested route active: %s", node)
				}
			}
			expr, err := expression(patch)
			if err != nil || strings.Contains(expr, "||") {
				t.Fatalf("unlimited-zero semantics leaked: %s %v", expr, err)
			}
		}
	}
	if selected != 1 {
		t.Fatalf("selected %d collection routes", selected)
	}
	essence := got.Modules[2]
	if !slices.Contains(essence.Blockers, "weapon_identity_confirmation_required") {
		t.Fatal("ambiguous weapon silently selected")
	}
	for _, enabled := range essence.Patch["AutoEssenceTargetPlan"].(map[string]any)["attach"].(map[string]any) {
		if enabled != false {
			t.Fatal("inherited weapon selection")
		}
	}
	if _, ok := essence.Patch["AutoEssenceOnComplete"]; ok {
		t.Fatal("upstream task-ended focus retained as success evidence")
	}
}

func TestMaterialsReadyDoesNotCompleteCultivation(t *testing.T) {
	docs := upstreamDocuments(t)
	plan := exampleModulePlan()
	plan.Materials = []Material{{"item_expcard_stage1_high", "高级作战记录", 2, 3}, {"item_expcard_stage1_low", "基础作战记录", 5, 0}}
	plan.Weapon.ID = "wpn_sword_0021"
	plan.Weapon.CandidateIDs = []string{plan.Weapon.ID}
	got, err := BuildModules(plan, docs[0], docs[1], docs[2], docs[3])
	if err != nil {
		t.Fatal(err)
	}
	if !got.MaterialsReady || got.CultivationDone || got.CompletionState != "materials_ready_cultivation_unverified" || len(got.Rows) != 1 || got.Rows[0].Need != 21000 || got.Rows[0].Owned != 30000 {
		t.Fatalf("wrong pooled experience/progress: %+v", got)
	}
	for _, module := range got.Modules {
		if module.Executable || len(module.Blockers) == 0 {
			t.Fatal("preview became execution authorization")
		}
	}
	for node, patch := range got.Modules[1].Patch {
		if strings.HasSuffix(node, "SubGated") && patch.(map[string]any)["enabled"] != false {
			t.Fatal("no collection requirements must disable every route")
		}
	}
}

func TestSharedCollectionRouteStopsOnlyWhenAllSelectedItemsReady(t *testing.T) {
	docs := upstreamDocuments(t)
	plan := exampleModulePlan()
	plan.Materials = []Material{{"item_plant_mushroom_2_1", "血菌", 10, 10}, {"item_plant_spcstone_2_2", "武陵石", 20, 0}}
	got, err := BuildModules(plan, docs[0], docs[1], docs[2], docs[3])
	if err != nil {
		t.Fatal(err)
	}
	patch := got.Modules[1].Patch
	expr, err := expression(patch["AutoCollectRoute7SubGated"])
	if err != nil || expr != "{item_plant_mushroom_2_1}<10 || {item_plant_spcstone_2_2}<20" || got.MaterialsReady {
		t.Fatalf("shared route lost a material: %s %v", expr, err)
	}
	skipped, err := expression(patch["AutoCollectRoute7SubSkipped"])
	if err != nil || skipped != "!("+expr+")" {
		t.Fatal("shared route skip is not the complement")
	}
	plan.Materials = plan.Materials[:1]
	got, err = BuildModules(plan, docs[0], docs[1], docs[2], docs[3])
	if err != nil {
		t.Fatal(err)
	}
	expr, err = expression(got.Modules[1].Patch["AutoCollectRoute7SubGated"])
	if err != nil || expr != "{item_plant_mushroom_2_1}<10" {
		t.Fatal("unrequested shared-route item became a goal")
	}
}

func TestModuleAdapterRejectsChangedContract(t *testing.T) {
	docs := upstreamDocuments(t)
	delete(docs[0].Option, "SupplyPlanLimits")
	if _, err := BuildModules(exampleModulePlan(), docs[0], docs[1], docs[2], docs[3]); err == nil {
		t.Fatal("accepted missing upstream target template")
	}
	docs = upstreamDocuments(t)
	delete(docs[1].Option, "AutoCollectTargetInventory")
	if _, err := BuildModules(exampleModulePlan(), docs[0], docs[1], docs[2], docs[3]); err == nil {
		t.Fatal("accepted missing collection routes")
	}
}

func TestUserConfirmationNeverBecomesAutomaticVerification(t *testing.T) {
	docs := upstreamDocuments(t)
	plan := exampleModulePlan()
	plan.Completion = &Completion{Method: "user_confirmed", Revision: plan.Revision, Header: plan.Header, At: plan.ReadAt.Add(time.Hour)}
	got, err := BuildModules(plan, docs[0], docs[1], docs[2], docs[3])
	if err != nil || !got.CultivationDone || got.MaterialsReady || got.CompletionState != "user_confirmed_done" || got.Executable {
		t.Fatalf("manual completion semantics: %+v %v", got, err)
	}
	plan.Completion.Revision--
	if _, err = BuildModules(plan, docs[0], docs[1], docs[2], docs[3]); err == nil {
		t.Fatal("accepted another revision's completion")
	}
	plan.Completion.Revision = plan.Revision
	plan.Completion.Method = "module_succeeded"
	if _, err = BuildModules(plan, docs[0], docs[1], docs[2], docs[3]); err == nil {
		t.Fatal("task success became cultivation completion")
	}
}
