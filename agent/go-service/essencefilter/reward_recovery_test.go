package essencefilter

import (
	"bytes"
	"os"
	"slices"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestDetailMarkFromVisibleButtons(t *testing.T) {
	for _, tc := range []struct {
		name    string
		visible []string
		want    string
	}{
		{"locked with unread discard button", []string{"CheckLocked"}, lockedMark},
		{"discarded with unread lock button", []string{"CheckDiscarded"}, discarded},
		{"both inactive", []string{"ClickLockItem", "ClickDiscardItem"}, unmarked},
		{"only inactive lock", []string{"ClickLockItem"}, ""},
		{"only inactive discard", []string{"ClickDiscardItem"}, ""},
		{"neither visible", nil, ""},
		{"contradictory active marks", []string{"CheckLocked", "CheckDiscarded"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := recognizeDetailMark(func(node string) (bool, error) {
				return slices.Contains(tc.visible, node), nil
			})
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got mark %q, error %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestRewardGridRequiresEveryCell(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		count int
		valid bool
	}{
		{`{"complete":true,"cells":[{"box":[0,0,96,96],"quality":"flawless_gold"},{"box":[117,0,96,96],"quality":"flawless_gold"}]}`, 2, true},
		{`{"complete":true,"cells":[{"box":[0,0,96,96],"quality":"other"}]}`, 0, true},
		{`{"complete":false,"cells":[{"box":[0,0,96,96],"quality":"flawless_gold"},{"box":[117,0,96,96],"quality":"unknown"}]}`, 0, false},
		{`{"complete":true,"cells":[{"box":[0,0,96,96],"quality":"unknown"}]}`, 0, false},
		{`{"complete":true,"cells":[{"box":[0,0,96,96],"quality":"flawless_gold"},{"box":[0,0,96,96],"quality":"high_purity_purple"}]}`, 0, false},
		{`{"complete":true,"cells":[{"box":[0,0,96,96],"quality":"flawless_gold"},{"box":[1,0,96,96],"quality":"high_purity_purple"}]}`, 0, false},
		{`{"complete":true,"cells":[]}`, 0, false},
	} {
		boxes, qualities, err := parseRewardGrid(tc.raw)
		if (err == nil) != tc.valid || err == nil && (len(boxes) != tc.count || len(qualities) != tc.count) {
			t.Fatalf("%s: %v %v", tc.raw, boxes, err)
		}
	}
}

func TestUnconfirmedRewardKeepsStockForNextRound(t *testing.T) {
	engine := inventoryTestEngine(t)
	a := testInventoryAccount(t.TempDir(), "aaaaaaaaaaaaaaaa")
	t.Cleanup(a.release)
	baseline := newSnapshot(a.id)
	baseline.Coverage[gold] = tierCoverage{CompleteAt: &baseline.UpdatedAt}
	item, err := observeItem(engine, matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 1, 1}}, gold, lockedMark)
	if err != nil {
		t.Fatal(err)
	}
	baseline.appendLockedMatch(item)
	if err := a.save(baseline); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(a.path())
	if err != nil {
		t.Fatal(err)
	}
	st := &RunState{TaskID: 987654, Account: a, MatchEngine: engine}
	if err := prepareReward(st); err != nil {
		t.Fatal(err)
	}
	if err := st.Loot.observe(1, item); err != nil {
		t.Fatal(err)
	}
	previous := currentRun
	currentRun = st
	t.Cleanup(func() { currentRun = previous })
	inventoryTasks.byID[987654] = a
	t.Cleanup(func() { delete(inventoryTasks.byID, 987654) })
	if !(&EssenceRewardFailedAction{}).Run(nil, &maa.CustomActionArg{TaskID: 987654}) {
		t.Fatal("recovery failed")
	}
	after, err := os.ReadFile(a.path())
	if err != nil || !bytes.Equal(before, after) || st.Loot != nil {
		t.Fatal("unconfirmed batch changed the last published inventory", err)
	}
	if err := prepareReward(st); err != nil {
		t.Fatal(err)
	}
	st.Loot.gridVerified = true
	st.RowBoxes = make([][4]int, 1)
	if err := st.Loot.observe(1, item); err != nil {
		t.Fatal(err)
	}
	st.Loot.expectedMarks[1] = lockedMark
	if !commitAfterBattleInventory(st, a.id) {
		t.Fatal("confirmed reward batch rejected")
	}
	got, err := a.load(engine)
	if err != nil || !got.targetReady() || len(got.Items) != 2 {
		t.Fatal("next target round lost its previous stock", err)
	}
}

func TestCustomFilterUnreadItemKeepsRunning(t *testing.T) {
	previous := currentRun
	t.Cleanup(func() { currentRun = previous })
	currentRun = &RunState{TaskID: 123, PipelineOpts: EssenceFilterOptions{Mode: InitModeFilter}}
	if !(&EssenceFilterUnreadPurpleAction{}).Run(nil, &maa.CustomActionArg{TaskID: 123}) {
		t.Fatal("custom filter aborted on unread affixes")
	}
	currentRun.PipelineOpts.Mode = InitModeScreenAndReport
	if (&EssenceFilterUnreadPurpleAction{}).Run(nil, &maa.CustomActionArg{TaskID: 123}) {
		t.Fatal("inventory accepted an unverified item")
	}
}
