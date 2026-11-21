package essencefilter

import (
	"encoding/json"
	"strconv"
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
		discard    bool
	}{
		{"custom can discard a five-star match", InitModeFilter, five, gold, unmarked, discarded, 0, true},
		{"custom can lock a six-star match", InitModeFilter, six, gold, unmarked, lockedMark, 0, true},
		{"report leaves five-star match unmarked", InitModeScreenAndReport, five, gold, unmarked, unmarked, 0, true},
		{"report leaves six-star match unmarked", InitModeScreenAndReport, six, gold, unmarked, unmarked, 0, true},
		{"report keeps existing discard", InitModeScreenAndReport, five, gold, discarded, unmarked, 0, true},
		{"report counts existing lock", InitModeScreenAndReport, five, gold, lockedMark, unmarked, 1, true},
		{"report skips four-star match", InitModeScreenAndReport, four, gold, unmarked, unmarked, 0, true},
		{"report excludes locked four-star match", InitModeScreenAndReport, four, gold, lockedMark, unmarked, 0, true},
		{"report skips purple match", InitModeScreenAndReport, six, purple, unmarked, unmarked, 0, true},
		{"read-only sees candidate", InitModeInventory, five, gold, unmarked, unmarked, 0, true},
		{"read-only sees manual lock", InitModeInventory, five, gold, lockedMark, unmarked, 1, true},
		{"battle locks five-star match", InitModeAfterBattle, five, gold, unmarked, lockedMark, 1, true},
		{"battle locks six-star match", InitModeAfterBattle, six, gold, unmarked, lockedMark, 1, true},
		{"battle discards unmatched gold", InitModeAfterBattle, four, gold, unmarked, discarded, 0, true},
		{"battle discards unmatched purple", InitModeAfterBattle, four, purple, unmarked, discarded, 0, true},
		{"battle keeps locked unmatched purple", InitModeAfterBattle, four, purple, lockedMark, unmarked, 0, true},
		{"battle keeps discarded unmatched gold", InitModeAfterBattle, four, gold, discarded, unmarked, 0, true},
		{"battle discards five-star purple match", InitModeAfterBattle, five, purple, unmarked, discarded, 0, true},
		{"battle discards Nexus purple match", InitModeAfterBattle, [3]string{"智识提升", "终结技充能效率提升", "效益"}, purple, unmarked, discarded, 0, true},
		{"battle keeps locked purple match", InitModeAfterBattle, six, purple, lockedMark, unmarked, 0, true},
		{"battle keeps discarded purple match", InitModeAfterBattle, six, purple, discarded, unmarked, 0, true},
		{"battle skips unmatched gold when discard disabled", InitModeAfterBattle, four, gold, unmarked, unmarked, 0, false},
		{"battle skips purple match when discard disabled", InitModeAfterBattle, six, purple, unmarked, unmarked, 0, false},
		{"battle skips purple without OCR when discard disabled", InitModeAfterBattle, [3]string{}, purple, unmarked, unmarked, 0, false},
		{"battle locks gold when discard disabled", InitModeAfterBattle, six, gold, unmarked, lockedMark, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parseFilterOptions(`{"attach":{"mode":"` + string(tc.mode) + `","rarity5_weapon":false,"rarity6_weapon":true,"rarity4_weapon":true,"pure_essence":true,"discard_unmatched":` + strconv.FormatBool(tc.discard) + `}}`)
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
				if !(&EssenceFilterConfirmMarkAction{}).Run(nil, &maa.CustomActionArg{CurrentTaskName: confirmed}) {
					t.Fatal("failed to confirm", action)
				}
			}
			s := newSnapshot("")
			switch {
			case st.Scan != nil:
				for _, item := range st.Scan.items {
					s.appendLockedMatch(item)
				}
			case st.Loot != nil:
				if err := st.Loot.check(1); err != nil {
					t.Fatal("reward decision left the batch incomplete", err)
				}
				s = st.Loot.appendTo(s)
			}
			if tc.mode != InitModeFilter {
				want := tc.mark
				if action != unmarked {
					want = action
				}
				if len(s.Items) != tc.wantStock || (len(s.Items) > 0 && (s.Items[0].Mark != want || len(s.Items[0].weaponIDs) == 0)) {
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
		{purple, unmarked, false, discarded, discarded},
		{purple, unmarked, true, discarded, discarded},
	} {
		st := &RunState{
			MatchEngine: engine, Loot: newRewardInventory(), RowIndex: 1,
			// Old overrides could disable flawless screening or enable only purple.
			PipelineOpts:              EssenceFilterOptions{Rarity6Weapon: true, PureEssence: tc.pure, FlawlessEssence: false, DiscardUnmatched: true},
			MatchedCombinationSummary: make(map[string]*matchapi.SkillCombinationSummary),
		}
		st.Loot.quality, st.Loot.mark = tc.quality, tc.initial
		got, ok := decideRewardMark(nil, st, ocr)
		if !ok || got != tc.action || st.Loot.expectedMarks[1] != tc.final || st.Loot.items[1].Quality != tc.quality {
			t.Errorf("%+v: got action %q, inventory %+v, expected marks %v", tc, got, st.Loot.items, st.Loot.expectedMarks)
		}
	}
}
