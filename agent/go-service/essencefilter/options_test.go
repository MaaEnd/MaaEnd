package essencefilter

import (
	"encoding/json"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestWarehouseAndRewardRarityRules(t *testing.T) {
	engine := inventoryTestEngine(t)
	previous := currentRun
	t.Cleanup(func() { currentRun = previous })
	five := [3]string{"力量", "生命", "效益"}
	six := [3]string{"意志", "生命", "效益"}
	four := [3]string{"力量", "攻击", "压制"}
	for _, tc := range []struct {
		name       string
		mode       InitMode
		skills     [3]string
		tier, mark string
		wantAction string
		wantStock  int
	}{
		{"custom can discard a five-star match", InitModeFilter, five, gold, unmarked, discarded, 0},
		{"custom can lock a six-star match", InitModeFilter, six, gold, unmarked, lockedMark, 0},
		{"report locks five-star match", InitModeScreenAndReport, five, gold, unmarked, lockedMark, 1},
		{"report locks six-star match", InitModeScreenAndReport, six, gold, unmarked, lockedMark, 1},
		{"report keeps existing discard", InitModeScreenAndReport, five, gold, discarded, unmarked, 0},
		{"report counts existing lock", InitModeScreenAndReport, five, gold, lockedMark, unmarked, 1},
		{"report skips four-star match", InitModeScreenAndReport, four, gold, unmarked, unmarked, 0},
		{"report excludes locked four-star match", InitModeScreenAndReport, four, gold, lockedMark, unmarked, 0},
		{"report skips purple match", InitModeScreenAndReport, six, purple, unmarked, unmarked, 0},
		{"read-only sees candidate", InitModeInventory, five, gold, unmarked, unmarked, 0},
		{"read-only sees manual lock", InitModeInventory, five, gold, lockedMark, unmarked, 1},
		{"battle locks five-star match", InitModeAfterBattle, five, gold, unmarked, lockedMark, 1},
		{"battle locks six-star match", InitModeAfterBattle, six, gold, unmarked, lockedMark, 1},
		{"battle skips four-star match", InitModeAfterBattle, four, gold, unmarked, unmarked, 0},
		{"battle skips purple match", InitModeAfterBattle, six, purple, unmarked, unmarked, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseFilterOptions(`{"attach":{"mode":"` + string(tc.mode) + `","rarity5_weapon":false,"rarity6_weapon":true,"rarity4_weapon":true,"pure_essence":true,"discard_unmatched":true}}`)
			if err != nil {
				t.Fatal(err)
			}
			st := &RunState{PipelineOpts: *opts, MatchEngine: engine, MatchedCombinationSummary: make(map[string]*matchapi.SkillCombinationSummary)}
			currentRun = st
			ocr := matchapi.OCRInput{Skills: tc.skills, Levels: [3]int{1, 1, 1}}
			var action string
			var ok bool
			prefix := "EssenceFilter"
			if tc.mode == InitModeAfterBattle {
				prefix += "AfterBattle"
				st.Loot, st.RowIndex = newRewardInventory(), 1
				st.Loot.quality, st.Loot.mark = tc.tier, tc.mark
				action, ok = decideRewardMark(nil, st, ocr)
			} else {
				if tc.mode != InitModeFilter {
					st.Scan = newWarehouseScan()
					st.Scan.current = gridSelection{Row: 0, Col: 0, Quality: tc.tier, Mark: tc.mark}
				}
				action, ok = decideWarehouseMark(nil, st, ocr)
			}
			if !ok || action != tc.wantAction {
				t.Fatalf("action %q, want %q", action, tc.wantAction)
			}
			if action != unmarked {
				confirmed := prefix + "CheckLocked"
				if action == discarded {
					confirmed = prefix + "CheckDiscarded"
				}
				if st.Scan != nil {
					end := gridSelection{Expected: 1, Resolved: 1, Issued: 1}
					if err := st.Scan.verify(end); err == nil || (&EssenceFilterConfirmMarkAction{}).Run(nil, &maa.CustomActionArg{CurrentTaskName: prefix + "CheckDiscarded"}) {
						t.Fatal("warehouse accepted an unconfirmed lock")
					}
				}
				if !(&EssenceFilterConfirmMarkAction{}).Run(nil, &maa.CustomActionArg{CurrentTaskName: confirmed}) {
					t.Fatal("failed to confirm", action)
				}
			}
			s := newSnapshot("")
			switch {
			case st.Scan != nil:
				s.Items = st.Scan.items
			case st.Loot != nil:
				s = st.Loot.appendTo(s)
			}
			if tc.mode != InitModeFilter {
				want := tc.mark
				if action != unmarked {
					want = action
				}
				if len(s.Items) != 1 || s.Items[0].Mark != want || len(s.Items[0].weaponIDs) == 0 {
					t.Fatal("item detail", s.Items)
				}
			}
			count, locks := inventoryTotals(s.groups(engine.Weapons()))
			if count != tc.wantStock || locks != count {
				t.Fatal("stock", count, locks, "want", tc.wantStock)
			}
		})
	}
}

func TestLegacyMinimumCannotDiscardExactMatch(t *testing.T) {
	const saved = `{"rarity6_weapon":true,"perfect_min_level_total":15,"discard_unmatched":true}`
	var opts EssenceFilterOptions
	if err := json.Unmarshal([]byte(saved), &opts); err != nil {
		t.Fatal(err)
	}
	engine := inventoryTestEngine(t)
	for _, tc := range []struct {
		skills [3]string
		want   matchapi.MatchKind
	}{
		{[3]string{"敏捷", "攻击", "附术"}, matchapi.MatchExact},
		{[3]string{"力量", "力量", "意志"}, matchapi.MatchNone},
	} {
		ocr := matchapi.OCRInput{Skills: tc.skills, Levels: [3]int{1, 1, 1}}
		got, err := engine.MatchOCR(ocr, matchOptsFromPipeline(&opts))
		if err != nil {
			t.Fatal(err)
		}
		if got.Kind != tc.want || got.ShouldLock != (tc.want == matchapi.MatchExact) || got.ShouldDiscard != (tc.want == matchapi.MatchNone) {
			t.Errorf("%v: got kind=%v lock=%t discard=%t, want kind=%v", tc.skills, got.Kind, got.ShouldLock, got.ShouldDiscard, tc.want)
		}
	}
}

func TestRewardScreeningByQualityAndExistingMark(t *testing.T) {
	engine := inventoryTestEngine(t)
	ocr := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 1, 1}}
	for _, tc := range []struct {
		quality, initial string
		pure             bool
		action, final    string
	}{
		{gold, unmarked, false, lockedMark, lockedMark},
		{gold, lockedMark, false, unmarked, lockedMark},
		{gold, discarded, false, unmarked, discarded},
		{purple, unmarked, false, unmarked, unmarked},
		{purple, unmarked, true, unmarked, unmarked},
	} {
		st := &RunState{
			MatchEngine: engine, Loot: newRewardInventory(), RowIndex: 1,
			// Old overrides could disable flawless screening or enable only purple.
			PipelineOpts:              EssenceFilterOptions{Rarity6Weapon: true, PureEssence: tc.pure, FlawlessEssence: false},
			MatchedCombinationSummary: make(map[string]*matchapi.SkillCombinationSummary),
		}
		st.Loot.quality, st.Loot.mark = tc.quality, tc.initial
		got, ok := decideRewardMark(nil, st, ocr)
		if !ok || got != tc.action || st.Loot.expectedMarks[1] != tc.final || st.Loot.items[1].Quality != tc.quality {
			t.Errorf("%+v: got action %q, inventory %+v, expected marks %v", tc, got, st.Loot.items, st.Loot.expectedMarks)
		}
	}
}
