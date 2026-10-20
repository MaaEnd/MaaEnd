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
	if _, err := Confirm(h, a, a, 5, time.Now(), false); err != nil {
		t.Fatal(err)
	}
	b := []Material{{ID: "item_gold", Name: "折金票", Need: 41, Owned: 30}}
	if _, err := Confirm(h, a, b, 5, time.Now(), false); err == nil {
		t.Fatal("accepted inconsistent passes")
	}
	if _, err := Confirm(h, nil, nil, 5, time.Now(), false); err == nil {
		t.Fatal("empty recognition became completion")
	}
	// A fully trained operator: every tab said 已达上限, so nothing is needed.
	if plan, err := Confirm(h, nil, nil, 5, time.Now(), true); err != nil || len(plan.Materials) != 0 {
		t.Fatalf("stated-empty plan rejected: %v", err)
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

func TestWeaponChoiceSelectsOneSection(t *testing.T) {
	p := plan(
		Material{ID: "item_gold", Name: "折金票", Need: 100, Owned: 500},
		Material{ID: "item_gold", Name: "折金票", Need: 40, Owned: 500, Weapon: "落草"},
		Material{ID: "item_weapon_break_low", Name: "轻黯石", Need: 23, Owned: 199, Weapon: "落草"},
		Material{ID: "item_gold", Name: "折金票", Need: 50, Owned: 500, Weapon: "望乡"},
		Material{ID: "item_weapon_break_high", Name: "重黯石", Need: 50, Owned: 99, Weapon: "望乡"},
	)
	p.Weapons = []Weapon{{Name: "落草"}, {Name: "望乡"}}
	goals, _, err := BuildTargets(p)
	if err != nil || goals["T_CREDS"] != 100 || goals["CAST_DIE"] != 0 || goals["HEAVY_CAST_DIE"] != 0 {
		t.Fatalf("no weapon: %v %v", goals, err)
	}
	p.WeaponChoice = "望乡"
	goals, _, err = BuildTargets(p)
	if err != nil || goals["T_CREDS"] != 150 || goals["CAST_DIE"] != 0 || goals["HEAVY_CAST_DIE"] != 50 {
		t.Fatalf("望乡: %v %v", goals, err)
	}
	p.WeaponChoice = "不存在"
	if _, _, err = BuildTargets(p); err == nil {
		t.Fatal("unknown weapon choice accepted")
	}
}

func TestReadEssencePanelsAcrossPages(t *testing.T) {
	// Real OCR of a page: the end of 落草's panel (merged count line), then 望乡.
	texts := []Text{
		{Text: "暂无适配基质", Box: [4]int{566, 281, 123, 21}, Score: 1},
		{Text: "无瑕基质0高纯基质", Box: [4]int{573, 308, 237, 20}, Score: .99},
		{Text: "0", Box: [4]int{823, 312, 13, 12}, Score: .74},
		{Text: "基质适配", Box: [4]int{436, 391, 76, 21}, Score: 1},
		{Text: "望乡", Box: [4]int{500, 424, 37, 20}, Score: 1},
		{Text: "敏捷提升|寒冷伤害提升|压制", Box: [4]int{500, 451, 233, 21}, Score: .99},
		{Text: "暂无适配基质", Box: [4]int{565, 549, 125, 27}, Score: 1},
		{Text: "无瑕基质", Box: [4]int{574, 578, 93, 18}, Score: 1},
		{Text: "高纯基质", Box: [4]int{716, 579, 93, 18}, Score: 1},
	}
	lines, section, err := ReadEssencePanels(texts, []Text{texts[4]}, "落草")
	if err != nil || section != "望乡" || len(lines) != 2 {
		t.Fatalf("lines=%+v section=%q err=%v", lines, section, err)
	}
	if lines[0].Weapon != "落草" || lines[0].Y != 308 || lines[0].Adapted != "" || lines[1].Weapon != "望乡" || lines[1].Y != 578 {
		t.Fatalf("lines = %+v", lines)
	}
}

func TestReadEssencePanelsLeadingIconDigit(t *testing.T) {
	// Real OCR at the top of the tab: the dot icon before 无瑕基质 was read as "0".
	texts := []Text{
		{Text: "基质适配", Box: [4]int{436, 268, 76, 21}, Score: 1},
		{Text: "落草", Box: [4]int{498, 300, 41, 23}, Score: 1},
		{Text: "暂无适配基质", Box: [4]int{567, 428, 123, 21}, Score: 1},
		{Text: "0 无瑕基质0", Box: [4]int{573, 453, 125, 21}, Score: .84},
		{Text: "高纯基质", Box: [4]int{700, 456, 108, 17}, Score: 1},
	}
	lines, _, err := ReadEssencePanels(texts, []Text{texts[1]}, "")
	if err != nil || len(lines) != 1 || lines[0].Weapon != "落草" || lines[0].Y != 453 {
		t.Fatalf("lines=%+v err=%v", lines, err)
	}
}

func TestNothingNeededOnMaxedTab(t *testing.T) {
	if !NothingNeeded([]Text{{Text: "干员等级已达上限", Box: [4]int{752, 505, 166, 21}, Score: 1}}) {
		t.Fatal("maxed operator level not recognised")
	}
	if !NothingNeeded([]Text{{Text: "干员技能与天赋培养已满级", Box: [4]int{745, 498, 190, 20}, Score: 1}}) {
		t.Fatal("maxed skills and talents not recognised")
	}
	if NothingNeeded([]Text{{Text: "干员等级提升", Box: [4]int{443, 270, 103, 20}, Score: 1}}) {
		t.Fatal("section title taken as no demand")
	}
}

func TestReadEssencePanelsInjected(t *testing.T) {
	// 莱万汀 on CloudADB: the equipped weapon already carries a matching essence.
	texts := []Text{
		{Text: "基质注入", Box: [4]int{443, 270, 70, 20}, Score: 1},
		{Text: "熔铸火焰", Box: [4]int{503, 303, 80, 20}, Score: 1},
		{Text: "当前注入基质：", Box: [4]int{561, 401, 140, 20}, Score: 1},
		{Text: "完全适配", Box: [4]int{707, 401, 80, 20}, Score: 1},
		{Text: "蚀刻已达上限", Box: [4]int{1062, 516, 104, 20}, Score: 1},
	}
	lines, _, err := ReadEssencePanels(texts, []Text{texts[1]}, "")
	if err != nil || len(lines) != 1 || lines[0].Weapon != "熔铸火焰" || lines[0].Injected != "完全适配" {
		t.Fatalf("lines=%+v err=%v", lines, err)
	}
}

func TestEssenceSatisfied(t *testing.T) {
	cases := []struct {
		essence *Essence
		want    bool
	}{
		{nil, false},
		{&Essence{Flawless: 0}, false},
		{&Essence{Flawless: 1}, true},
		{&Essence{Injected: "完全适配"}, true},
		{&Essence{Injected: "部分适配"}, false},
		{&Essence{Injected: "部分适配", Flawless: 1}, true},
	}
	for _, c := range cases {
		if got := (Weapon{Name: "望乡", Essence: c.essence}).EssenceSatisfied(); got != c.want {
			t.Fatalf("%+v: got %v", c.essence, got)
		}
	}
}

func TestOwnedRowsKeepsWholeEdgeRow(t *testing.T) {
	// 祀 on CloudADB: the bottom row's labels straddle the limit by a few pixels.
	in := []Text{{"拥有9", [4]int{747, 635, 49, 19}, 1}, {"拥有0", [4]int{661, 637, 48, 19}, 1}, {"拥有93", [4]int{944, 636, 59, 21}, 1}}
	if got := OwnedRows(in, false); len(got) != 3 || got[0].Text != "拥有0" {
		t.Fatalf("row starting inside the limit must be kept whole: %+v", got)
	}
	in = []Text{{"拥有9", [4]int{747, 640, 49, 19}, 1}, {"拥有0", [4]int{661, 636, 48, 19}, 1}}
	if got := OwnedRows(in, false); len(got) != 0 {
		t.Fatalf("row starting below the limit must be dropped whole: %+v", got)
	}
}

func TestOwnedRowsSkipsFadedTopRowWhenScrolled(t *testing.T) {
	// 祀 on CloudADB: the 稀有采集物 icon under the top fade read as unknown.
	in := []Text{{"拥有94", [4]int{922, 346, 50, 19}, 1}, {"拥有0", [4]int{661, 476, 48, 19}, 1}}
	if got := OwnedRows(in, false); len(got) != 2 {
		t.Fatalf("list top keeps the first row: %+v", got)
	}
	if got := OwnedRows(in, true); len(got) != 1 || got[0].Text != "拥有0" {
		t.Fatalf("scrolled page skips the faded top row: %+v", got)
	}
}
