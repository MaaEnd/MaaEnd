package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAmount(t *testing.T) {
	for input, want := range map[string]int64{"33.8万": 338000, "370万": 3700000, "4,909": 4909, "1.2亿": 120000000, "0": 0} {
		t.Run(input, func(t *testing.T) {
			got, err := Amount(input)
			if err != nil || got != want {
				t.Fatalf("%d %v, want %d", got, err, want)
			}
		})
	}
	for _, input := range []string{"LV.90", "拥有40", "40/100", "-1", "0.5", "999999999999999999999亿", ""} {
		t.Run("reject_"+input, func(t *testing.T) {
			if _, err := Amount(input); err == nil {
				t.Fatal("accepted ambiguous number")
			}
		})
	}
}

func TestHeaderRejectsChangedOrAmbiguousTarget(t *testing.T) {
	texts := []Text{{"//培养总览", [4]int{78, 27, 104, 28}, 1}, {"管理员", [4]int{489, 88, 63, 24}, 1}, {"培养期望：卓越", [4]int{1002, 116, 127, 22}, 1}}
	h, err := ReadHeader(texts)
	if err != nil || h.Operator != "管理员" || h.Expectation != "卓越" {
		t.Fatalf("%+v %v", h, err)
	}
	texts = append(texts, Text{"其他干员", [4]int{600, 90, 70, 20}, 1})
	if _, err = ReadHeader(texts); err == nil {
		t.Fatal("accepted ambiguous operator")
	}
}

func TestViewportOverlapAndRepeatedSkillCosts(t *testing.T) {
	a := Material{ID: "item_gold", Name: "折金票", Need: 40, Owned: 30}
	b := Material{ID: "item_char_skill_level_1_6", Name: "协议棱柱", Need: 80, Owned: 20}
	rows := MergePage([]Material{a}, []Material{a, b})
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	rows = MergePage(rows, []Material{a})
	if len(rows) != 3 {
		t.Fatal("later identical cost was lost")
	}
	merged, err := Merge(rows)
	if err != nil || merged[1].Need != 80 {
		t.Fatalf("%+v %v", merged, err)
	}
	bad := a
	bad.Owned = 29
	if _, err = Merge([]Material{a, bad}); err == nil {
		t.Fatal("accepted contradictory account inventory")
	}
}

func plan(rows ...Material) Plan {
	return Plan{Version: 1, Source: SourceVersion, Header: Header{"管理员", "卓越"}, ReadAt: time.Now().UTC(), Materials: rows}
}

func TestSupplyGoalsUseTotalAndWeightedExperience(t *testing.T) {
	p := plan(Material{ID: "item_char_skill_level_1_6", Name: "协议棱柱", Need: 40, Owned: 30}, Material{ID: "item_char_skill_level_1_6", Name: "协议棱柱", Need: 40, Owned: 30}, Material{ID: "item_expcard_stage1_high", Name: "高级作战记录", Need: 2, Owned: 7}, Material{ID: "item_expcard_stage1_low", Name: "基础作战记录", Need: 3, Owned: 8}, Material{ID: "plant_manual", Name: "地图植物", Need: 4, Owned: 1})
	goals, manual, err := BuildTargets(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(goals) != 16 || goals["PROTOPRISM"] != 80 || goals["COMBAT_RECORD_EXP"] != 20600 || goals["T_CREDS"] != 0 || len(manual) != 1 {
		t.Fatalf("%+v %+v", goals, manual)
	}
}

func TestTwoCompletePassesRequired(t *testing.T) {
	h := Header{"管理员", "卓越"}
	a := []Material{{ID: "item_gold", Name: "折金票", Need: 40, Owned: 30}}
	if _, err := Confirm(h, a, a, 5, time.Now()); err != nil {
		t.Fatal(err)
	}
	b := []Material{{ID: "item_gold", Name: "折金票", Need: 41, Owned: 30}}
	if _, err := Confirm(h, a, b, 5, time.Now()); err == nil {
		t.Fatal("accepted inconsistent passes")
	}
	if _, err := Confirm(h, nil, nil, 5, time.Now()); err == nil {
		t.Fatal("empty recognition became completion")
	}
}

func TestRenderRequiresAllGoalsAndRetainsItemPlaceholders(t *testing.T) {
	goals, _, _ := BuildTargets(plan(Material{ID: "item_gold", Name: "折金票", Need: 80, Owned: 30}))
	patch, err := RenderOverrides([]byte(`{"Check":{"expression":"{item_gold}<{SupplyPlanLimit_T_CREDS}"}}`), goals)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(patch)
	if !strings.Contains(string(raw), "{item_gold}") || !strings.Contains(string(raw), "80") {
		t.Fatal(string(raw))
	}
	delete(goals, "WEAPON_EXP")
	if _, err = RenderOverrides([]byte(`{}`), goals); err == nil {
		t.Fatal("accepted partial goals")
	}
}

func TestUnknownMaterialStaysManual(t *testing.T) {
	unknown := Material{ID: UnknownID(0x1234), Need: 5, Owned: 1, Unknown: true}
	goals, manual, err := BuildTargets(plan(Material{ID: "item_gold", Name: "折金票", Need: 40, Owned: 30}, unknown))
	if err != nil || goals["T_CREDS"] != 40 || len(manual) != 1 || manual[0].ID != unknown.ID || !manual[0].Unknown {
		t.Fatalf("goals=%v manual=%v err=%v", goals, manual, err)
	}
}

func TestSplitOwnedMergedLabels(t *testing.T) {
	got := SplitOwned([]Text{{Text: "拥有2.73万 拥有4173", Box: [4]int{823, 359, 164, 17}, Score: .97}, {Text: "拥有251", Box: [4]int{835, 489, 60, 17}, Score: .9}})
	if len(got) != 3 || got[0].Text != "拥有2.73万" || got[1].Text != "拥有4173" || got[2].Text != "拥有251" {
		t.Fatalf("split = %+v", got)
	}
	if c := got[1].Box[0] + got[1].Box[2]/2; c < 940 || c > 965 {
		t.Fatalf("second label centre %d", c)
	}
}
