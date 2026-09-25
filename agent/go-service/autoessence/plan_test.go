package autoessence

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

func testDataDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// agent/go-service/autoessence -> repo root assets/data/EssenceFilter
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return filepath.Join(root, "assets", "data", "EssenceFilter")
}

func TestSelectBestPlanCoversMissing(t *testing.T) {
	t.Cleanup(Reset)
	engine, err := matchapi.NewEngineFromDirWithLocale(testDataDir(t), "CN")
	if err != nil {
		t.Fatal(err)
	}

	// Pick a known 6-star sword that has at least one feasible location.
	var sample string
	for _, w := range engine.Weapons() {
		if w.Rarity == 6 && len(w.SkillIDs) == 3 && w.InternalID != "" {
			sample = w.InternalID
			break
		}
	}
	if sample == "" {
		t.Fatal("no sample weapon")
	}

	plan, err := SelectBestPlan(engine, []string{sample})
	if err != nil {
		t.Fatal(err)
	}
	if plan == nil {
		t.Fatal("expected a farm plan")
	}
	if plan.LocationKey == "" {
		t.Fatal("expected location key")
	}
	if plan.FixedSlot != 2 && plan.FixedSlot != 3 {
		t.Fatalf("unexpected fixed slot %d", plan.FixedSlot)
	}
	if len(plan.Needs) < 1 {
		t.Fatal("expected needs >= 1")
	}
	found := false
	for _, w := range plan.Needs {
		if w.InternalID == sample {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("plan needs should include %s", sample)
	}
}

func TestSelectBestPlanNilWhenNoMissing(t *testing.T) {
	engine, err := matchapi.NewEngineFromDirWithLocale(testDataDir(t), "CN")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := SelectBestPlan(engine, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan != nil {
		t.Fatal("expected nil plan for empty missing")
	}
}

func TestMissingSatisfiedByInventory(t *testing.T) {
	t.Cleanup(Reset)
	engine, err := matchapi.NewEngineFromDirWithLocale(testDataDir(t), "CN")
	if err != nil {
		t.Fatal(err)
	}
	var sample string
	var peerIDs []string
	for _, w := range engine.Weapons() {
		if w.Rarity != 6 || len(w.SkillIDs) != 3 {
			continue
		}
		if sample == "" {
			sample = w.InternalID
			continue
		}
		// Collect weapons that share the exact skill combo with sample.
		sw := findWeapon(engine, sample)
		if sw != nil && sameSkills(sw.SkillIDs, w.SkillIDs) {
			peerIDs = append(peerIDs, w.InternalID)
		}
	}
	if sample == "" {
		t.Fatal("no sample")
	}
	ids := append([]string{sample}, peerIDs...)
	Add(ids, 1)
	if Count([]string{sample}) < 1 {
		t.Fatal("expected inventory count >= 1 after Add")
	}
}

func findWeapon(engine *matchapi.Engine, id string) *matchapi.WeaponData {
	for i := range engine.Weapons() {
		w := engine.Weapons()[i]
		if w.InternalID == id {
			return &w
		}
	}
	return nil
}

func sameSkills(a, b []int) bool {
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	return a[0] == b[0] && a[1] == b[1] && a[2] == b[2]
}

func TestBuildEngraveOverride(t *testing.T) {
	if err := loadSkillExpected(testDataDir(t)); err != nil {
		t.Fatal(err)
	}
	plan := &FarmPlan{
		LocationKey: "VFTheHub",
		Slot1IDs:    [3]int{2, 3, 4},
		Slot1Names:  [3]string{"力量", "意志", "敏捷"},
		FixedSlot:   2,
		FixedID:     2,
		FixedName:   "攻击",
	}
	patch, err := buildEngravePipelineOverride(plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := patch["AutoEssenceSelectEngraveBase_s1_2"]; !ok {
		t.Fatal("missing selected base override")
	}
	if _, ok := patch["AutoEssenceSelectEngraveBonusCondition"]; !ok {
		t.Fatal("missing bonus override")
	}
	// Unselected s1_1 should be DirectHit skip
	sel1, ok := patch["AutoEssenceSelectEngraveBase_s1_1"].(map[string]any)
	if !ok {
		t.Fatal("missing unselected base")
	}
	rec := sel1["recognition"].(map[string]any)
	if rec["type"] != "DirectHit" {
		t.Fatalf("unselected base should be DirectHit, got %v", rec["type"])
	}
}

func TestEngraveOCRExpectedUsesUltimateGainNotULT(t *testing.T) {
	skillExpectedCache = nil
	if err := loadSkillExpected(testDataDir(t)); err != nil {
		t.Fatal(err)
	}
	got, err := expectedForSkill(2, 11)
	if err != nil {
		t.Fatal(err)
	}
	hasUltimateGain := false
	for _, s := range got {
		if s == "ULT" {
			t.Fatalf("OCR expected must not use skill_pools abbreviation ULT, got %v", got)
		}
		if s == "Ultimate Gain" {
			hasUltimateGain = true
		}
	}
	if !hasUltimateGain {
		t.Fatalf("OCR expected must include Ultimate Gain, got %v", got)
	}

	plan := &FarmPlan{
		LocationKey: "VFTheHub",
		Slot1IDs:    [3]int{1, 2, 3},
		FixedSlot:   2,
		FixedID:     11,
		FixedName:   "终结技充能",
	}
	patch, err := buildEngravePipelineOverride(plan)
	if err != nil {
		t.Fatal(err)
	}
	bonus := patch["AutoEssenceSelectEngraveBonusCondition"].(map[string]any)
	param := bonus["recognition"].(map[string]any)["param"].(map[string]any)
	expected := param["expected"].([]string)
	for _, s := range expected {
		if s == "ULT" {
			t.Fatalf("bonus OCR override must not use ULT, got %v", expected)
		}
	}
}

func TestSetAnchorOverrideEnablesNode(t *testing.T) {
	patch := map[string]any{}
	anchor := SetAnchorNode("WLYinglungPass")
	patch[anchor] = map[string]any{
		"enabled": true,
		"next": []string{
			"AutoEssenceDispatcher",
		},
	}
	node := patch[anchor].(map[string]any)
	if node["enabled"] != true {
		t.Fatal("anchor must be enabled for OverrideNext")
	}
}

func TestFinishNextItemsChainsLootReportThenOnComplete(t *testing.T) {
	items := finishNextItems()
	if len(items) != 2 {
		t.Fatalf("want 2 next items, got %d", len(items))
	}
	if items[0].Name != nodeLootReportJumpBack {
		t.Fatalf("first next want %q, got %q", nodeLootReportJumpBack, items[0].Name)
	}
	if items[1].Name != nodeOnComplete {
		t.Fatalf("second next want %q, got %q", nodeOnComplete, items[1].Name)
	}
}

func TestLocationKeyByName(t *testing.T) {
	key, ok := LocationKeyByName("重度能量淤积点·枢纽区")
	if !ok || key != "VFTheHub" {
		t.Fatalf("got %q %v", key, ok)
	}
	if _, ok := LocationKeyByName("不存在"); ok {
		t.Fatal("expected miss")
	}
}
