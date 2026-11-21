package essencefilter

import (
	"reflect"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestTargetProgressCountsSharedEssencesOnce(t *testing.T) {
	engine := inventoryTestEngine(t)
	selected := []string{"wpn_sword_0018", "wpn_sword_0021"}
	item, err := observeItem(engine, matchapi.OCRInput{
		Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 1, 1},
	}, gold, lockedMark)
	if err != nil {
		t.Fatal(err)
	}
	base := newSnapshot("")
	base.appendLockedMatch(item)
	stock := stockFromGroups(base.groups(engine.Weapons()))
	groups := stock.TargetProgress(engine.Weapons(), selected)
	if len(groups) != 1 || groups[0].Have != 1 || groups[0].Need != 2 || len(groups[0].Names) != 2 ||
		len(stock.MissingWeapons(selected)) != 1 {
		t.Fatal("shared combination must show one of two copies", groups)
	}
	groups = stock.TargetProgress(engine.Weapons(), selected[1:])
	if len(groups) != 1 || groups[0].Have != 1 || groups[0].Need != 1 {
		t.Fatal("a single selected weapon must show one of one copies", groups)
	}
}

func TestRewardReportUsesConfirmedChanges(t *testing.T) {
	engine := inventoryTestEngine(t)
	exact := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 1, 1}}
	incidental := matchapi.OCRInput{Skills: [3]string{"意志", "生命", "效益"}, Levels: [3]int{1, 1, 1}}
	unmatched := matchapi.OCRInput{Skills: [3]string{"力量", "攻击", "压制"}, Levels: [3]int{1, 1, 1}}
	base := newSnapshot("")
	old, err := observeItem(engine, exact, gold, lockedMark)
	if err != nil {
		t.Fatal(err)
	}
	base.appendLockedMatch(old)
	base.Coverage[lockedGold] = tierCoverage{CompleteAt: &base.UpdatedAt}
	selected := []string{"wpn_sword_0018", "wpn_sword_0021"}
	st := &RunState{
		Account: &inventoryAccount{base: base}, MatchEngine: engine, Loot: newRewardInventory(),
		PipelineOpts:              EssenceFilterOptions{Mode: InitModeAfterBattle, LockLimitWeaponIDs: selected, DiscardUnmatched: true},
		MatchedCombinationSummary: make(map[string]*matchapi.SkillCombinationSummary),
	}
	previous := currentRun
	currentRun = st
	t.Cleanup(func() { currentRun = previous })
	if !prepareLockLimit(nil, st, &st.PipelineOpts) {
		t.Fatal("quota preparation failed")
	}
	drops := []struct {
		ocr           matchapi.OCRInput
		quality, mark string
	}{
		{exact, gold, unmarked},
		{exact, gold, unmarked},
		{matchapi.OCRInput{}, purple, unmarked},
		{matchapi.OCRInput{}, purple, unmarked},
		{unmatched, gold, unmarked},
		{incidental, gold, unmarked},
		{incidental, gold, lockedMark},
		{unmatched, gold, discarded},
	}
	for i, drop := range drops {
		st.RowIndex = i + 1
		st.Loot.quality, st.Loot.mark = drop.quality, drop.mark
		mark, ok := decideRewardMark(nil, st, drop.ocr)
		if !ok {
			t.Fatal("decision failed", i)
		}
		if mark != unmarked {
			node := "EssenceFilterAfterBattleCheckLocked"
			if mark == discarded {
				node = "EssenceFilterAfterBattleCheckDiscarded"
			}
			if !(&EssenceFilterConfirmMarkAction{}).Run(nil, &maa.CustomActionArg{CurrentTaskName: node}) {
				t.Fatal("confirmation failed", i)
			}
		}
	}
	if err := st.Loot.check(len(drops)); err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	for _, row := range st.Loot.reportRows(selected) {
		counts[row.kind] += row.count
	}
	want := map[string]int{
		"locked": 1, "quota_full": 1, "discarded": 3,
		"incidental": 1, "already_locked": 1, "already_discarded": 1,
	}
	if !reflect.DeepEqual(counts, want) {
		t.Fatal("incorrect per-round counts", counts)
	}
	after := st.Loot.appendTo(base)
	if stock := stockFromGroups(after.groups(engine.Weapons())); len(stock.MissingWeapons(selected)) != 0 ||
		stock.Count(selected) != 2 {
		t.Fatal("the shared target should have exactly two copies")
	}
}
