package essencefilter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/captureuid"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func inventoryTestEngine(t *testing.T) *matchapi.Engine {
	t.Helper()
	dir, err := filepath.Abs("../../../assets/data/EssenceFilter")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := matchapi.NewEngineFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return engine
}

func testInventoryAccount(dir, id string) *inventoryAccount {
	return &inventoryAccount{id: id, dir: dir, generation: captureuid.CacheGeneration()}
}

func TestWarehouseObservesEveryPosition(t *testing.T) {
	engine := inventoryTestEngine(t)
	scan := newWarehouseScan()
	exact := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 2, 3}}
	repeated := matchapi.OCRInput{Skills: [3]string{"力量", "力量", "意志"}, Levels: [3]int{4, 5, 6}}
	for i, input := range []matchapi.OCRInput{exact, exact, exact, repeated} {
		scan.current = gridSelection{Row: 0, Col: i, Quality: gold, Mark: unmarked}
		if i == 1 {
			scan.current.Mark = lockedMark
		}
		if i == 2 {
			scan.current.Mark = discarded
		}
		if i == 3 {
			scan.current.Quality = purple
		}
		if _, err := scan.observe(engine, input); err != nil {
			t.Fatal(err)
		}
		if _, err := scan.observe(engine, input); err != nil {
			t.Fatal(err)
		}
	}
	if len(scan.items) != 4 || len(scan.items[3].weaponIDs) != 0 || scan.items[3].resolved[2].Pool != 1 || scan.items[3].Skills[2].Level != 6 {
		t.Fatal(scan.items)
	}
	if err := scan.verify(gridSelection{Expected: 5, Issued: 5, Resolved: 5}); err == nil || scan.verified {
		t.Fatal("missing OCR marked complete")
	}
	if err := scan.verify(gridSelection{Expected: 4, Issued: 4, Resolved: 3}); err == nil {
		t.Fatal("stalled viewport marked complete")
	}
	if err := scan.verify(gridSelection{Expected: 4, Issued: 4, Resolved: 4}); err != nil {
		t.Fatal(err)
	}
	s := newSnapshot("a")
	s.Items = scan.items
	groups := s.groups(engine.Weapons())
	count, locked := inventoryTotals(groups)
	if count != 1 || locked != 1 || stockFromGroups(groups).Count([]string{"wpn_sword_0021"}) != 1 {
		t.Fatal(groups)
	}
	if _, err := scan.observe(engine, exact); err == nil {
		t.Fatal("same coordinate accepted different OCR")
	}
}

func TestInventoryTransactionCoverage(t *testing.T) {
	engine := inventoryTestEngine(t)
	a := testInventoryAccount(t.TempDir(), "aaaaaaaaaaaaaaaa")
	observer := testInventoryAccount(a.dir, a.id)
	s := newSnapshot(a.id)
	s.Coverage[gold] = tierCoverage{CompleteAt: &s.UpdatedAt}
	s.Coverage[purple] = s.Coverage[gold]
	if err := a.save(s); err != nil {
		t.Fatal(err)
	}
	begin := func() {
		t.Helper()
		if err := a.begin(engine); err != nil {
			t.Fatal(err)
		}
		disk, err := observer.load(engine)
		if err != nil || len(disk.Coverage) != 0 || !reflect.DeepEqual(a.base.Coverage, s.Coverage) {
			t.Fatalf("coverage during update: disk %+v, base %+v, err %v", disk, a.base, err)
		}
	}
	begin()
	if err := observer.begin(engine); err == nil {
		t.Fatal("concurrent writer admitted")
	}
	if err := a.cancelClaim(a.id); err != nil {
		t.Fatal(err)
	}
	cancelled, err := observer.load(engine)
	if err != nil || !reflect.DeepEqual(cancelled, s) {
		t.Fatal("cancelled claim did not restore the baseline", err)
	}
	begin()
	if err := a.commit(newRewardInventory().appendTo(a.base), false, a.id); err != nil {
		t.Fatal(err)
	}
	committed, err := observer.load(engine)
	if err != nil || !reflect.DeepEqual(committed.Coverage, s.Coverage) {
		t.Fatal("commit lost complete scan timestamps", err)
	}
	begin()
	a.release() // Process stopped after claiming the rewards.
	if err := observer.begin(engine); err != nil {
		t.Fatal(err)
	}
	defer observer.release()
	if observer.base.targetReady() {
		t.Fatal("interrupted claim restored gold coverage")
	}
}

func TestCommitLegacyInventoryCleanup(t *testing.T) {
	engine := inventoryTestEngine(t)
	const accountID = "aaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name       string
		id         string
		blockFirst bool
	}{
		{name: "confirmed", id: accountID},
		{name: "anonymous"},
		{name: "removal fails", id: accountID, blockFirst: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			other := filepath.Join(dir, "other")
			if err := os.Mkdir(other, 0755); err != nil {
				t.Fatal(err)
			}
			names := []string{"EssenceInventory.json", "EssenceInventoryFull.json"}
			for i, name := range names {
				path := filepath.Join(dir, name)
				if tc.blockFirst && i == 0 {
					if err := os.Mkdir(path, 0755); err != nil {
						t.Fatal(err)
					}
					path = filepath.Join(path, "keep")
				}
				for _, file := range []string{path, filepath.Join(other, name)} {
					if err := os.WriteFile(file, []byte("legacy"), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			a := testInventoryAccount(dir, tc.id)
			if err := a.begin(engine); err != nil {
				t.Fatal(err)
			}
			defer a.release()
			for _, name := range names {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Fatalf("before commit: %v", err)
				}
			}
			if err := a.commit(a.base, true, tc.id); err != nil {
				t.Fatal(err)
			}
			if _, err := os.ReadFile(a.path()); err != nil {
				t.Fatal(err)
			}
			for i, name := range names {
				path := filepath.Join(dir, name)
				preserved := []string{filepath.Join(other, name)}
				switch {
				case tc.id == "":
					preserved = append(preserved, path)
				case tc.blockFirst && i == 0:
					preserved = append(preserved, filepath.Join(path, "keep"))
				default:
					if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("expected %s to be removed, stat returned %v", path, err)
					}
				}
				for _, file := range preserved {
					if data, err := os.ReadFile(file); err != nil || string(data) != "legacy" {
						t.Errorf("expected %s to retain its contents, got %q, %v", file, data, err)
					}
				}
			}
		})
	}
}

func TestAccountUIDReuseAndAnonymousLifetime(t *testing.T) {
	engine := inventoryTestEngine(t)
	dir := t.TempDir()
	const firstID, secondID = "aaaaaaaaaaaaaaaa", "bbbbbbbbbbbbbbbb"
	uid := firstID
	samples := 0
	sample := func() string { samples++; return uid }
	for _, id := range []int64{10, 11, 12} {
		t.Cleanup(func() { releaseInventoryTask(id) })
	}
	known, err := bindInventoryAccount(10, dir, sample)
	if err != nil || known.id != firstID {
		t.Fatal("failed to bind first account", err)
	}
	if got, err := bindInventoryAccount(10, dir, sample); err != nil || got != known || samples != 1 {
		t.Fatal("did not reuse verified identity within task", err)
	}

	uid = ""
	anonymous, err := bindInventoryAccount(11, dir, sample)
	if err != nil || anonymous.id != "" {
		t.Fatal("unverified account reused a previous UID", err)
	}
	if err := anonymous.begin(engine); err != nil {
		t.Fatal(err)
	}
	s := newSnapshot("")

	s.Coverage[gold] = tierCoverage{CompleteAt: &s.UpdatedAt}
	if err := anonymous.commit(s, false, ""); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatal("persisted anonymous account", entries, err)
	}

	captureuid.ClearCache()
	uid = secondID
	if _, err := bindInventoryAccount(10, dir, sample); !errors.Is(err, errInventoryAccountChanged) {
		t.Fatal("running task rebound to the new account", err)
	}
	if known.matchesUID(firstID) || anonymous.matchesUID("") {
		t.Fatal("account switch preserved an old session")
	}
	next, err := bindInventoryAccount(12, dir, sample)
	if err != nil || next.id != secondID {
		t.Fatal("fresh task failed to bind second account", err)
	}
	s, err = next.load(engine)
	if err != nil || s.targetReady() {
		t.Fatal("new account inherited inventory", err)
	}
}

func TestChangingAccountsCannotPublishOrCancelAnotherAccountsInventory(t *testing.T) {
	dir := t.TempDir()
	a := testInventoryAccount(dir, "aaaaaaaaaaaaaaaa")
	b := testInventoryAccount(dir, "bbbbbbbbbbbbbbbb")
	engine := inventoryTestEngine(t)
	ocr := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 1, 1}}
	item, err := observeItem(engine, ocr, gold, lockedMark)
	if err != nil {
		t.Fatal(err)
	}
	for n, account := range []*inventoryAccount{a, b} {
		s := newSnapshot(account.id)
		for range n + 1 {
			s.Items = append(s.Items, item)
		}
		s.Coverage[gold] = tierCoverage{CompleteAt: &s.UpdatedAt}
		if err := account.begin(engine); err != nil {
			t.Fatal(err)
		}
		if err := account.commit(s, false, account.id); err != nil {
			t.Fatal(err)
		}
	}
	baseline, err := a.load(engine)
	if err != nil || len(baseline.Items) != 1 {
		t.Fatal("second account overwrote the first", err)
	}
	newer := *baseline
	newer.Items = []inventoryItem{}
	for _, observed := range []string{b.id, ""} {
		if err := a.begin(engine); err != nil {
			t.Fatal(err)
		}
		if err := a.commit(&newer, true, observed); !errors.Is(err, errInventoryAccountChanged) {
			t.Fatal("unverified account published the scan", err)
		}
	}
	if err := a.begin(engine); err != nil {
		t.Fatal(err)
	}
	if err := a.cancelClaim(b.id); !errors.Is(err, errInventoryAccountChanged) {
		t.Fatal("other account cleared a pending claim", err)
	}
	savedA, err := a.load(engine)
	if err != nil || savedA.targetReady() || len(savedA.Items) != 1 {
		t.Fatal("first account lost its last inventory", err)
	}
	savedB, err := b.load(engine)
	if err != nil || !savedB.targetReady() || len(savedB.Items) != 2 {
		t.Fatal("second account changed", err)
	}

}

func TestRewardTwoPassCommitsOnce(t *testing.T) {
	a := testInventoryAccount(t.TempDir(), "aaaaaaaaaaaaaaaa")
	engine := inventoryTestEngine(t)
	if err := a.begin(engine); err != nil {
		t.Fatal(err)
	}
	s := newSnapshot(a.id)
	s.Coverage[gold] = tierCoverage{CompleteAt: &s.UpdatedAt}

	if err := a.commit(s, true, a.id); err != nil {
		t.Fatal(err)
	}
	st := &RunState{
		Account: a, MatchEngine: engine, RowBoxes: make([][4]int, 2),
		PipelineOpts:              EssenceFilterOptions{Mode: InitModeAfterBattle, Rarity6Weapon: true, Rarity5Weapon: true},
		MatchedCombinationSummary: make(map[string]*matchapi.SkillCombinationSummary),
	}
	inputs := []matchapi.OCRInput{
		{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 1, 1}},
		{Skills: [3]string{"力量", "力量", "意志"}, Levels: [3]int{1, 1, 1}},
	}
	st.PipelineOpts.AfterBattlePass = AfterBattlePassReadOnly
	if err := prepareReward(st); err != nil {
		t.Fatal(err)
	}
	st.Loot.quality, st.Loot.mark = gold, unmarked
	for i, ocr := range inputs {
		st.RowIndex = i + 1
		if mark, ok := decideRewardMark(nil, st, ocr); !ok || mark != unmarked {
			t.Fatal("read-only pass requested a mark", i, mark)
		}
		if i == 0 && commitAfterBattleInventory(st, a.id) {
			t.Fatal("accepted incomplete first pass")
		}
	}
	if !commitAfterBattleInventory(st, a.id) {
		t.Fatal("staging failed")
	}
	if staged, err := a.load(engine); err != nil || staged.targetReady() || len(staged.Items) != 0 {
		t.Fatal("read-only pass published rewards", staged, err)
	}
	st.PipelineOpts.AfterBattlePass = AfterBattlePassExecute
	if err := prepareReward(st); err != nil {
		t.Fatal(err)
	}
	previous := currentRun
	currentRun = st
	t.Cleanup(func() { currentRun = previous })
	action := EssenceFilterConfirmMarkAction{}
	for i, want := range []string{lockedMark, unmarked} {
		st.RowIndex = i + 1
		if mark, ok := decideRewardMark(nil, st, inputs[i]); !ok || mark != want {
			t.Fatal("unexpected execute decision", i, mark, want)
		}
		if i == 0 {
			if action.Run(nil, &maa.CustomActionArg{TaskID: st.TaskID, CurrentTaskName: "EssenceFilterAfterBattleCheckDiscarded"}) ||
				commitAfterBattleInventory(st, a.id) {
				t.Fatal("accepted the wrong or unconfirmed mark")
			}
			if !action.Run(nil, &maa.CustomActionArg{TaskID: st.TaskID, CurrentTaskName: "EssenceFilterAfterBattleCheckLocked"}) {
				t.Fatal("lock confirmation failed")
			}
		}
	}
	if !commitAfterBattleInventory(st, a.id) || !commitAfterBattleInventory(st, a.id) {
		t.Fatal("commit/retry failed")
	}
	got, err := a.load(engine)
	if err != nil {
		t.Fatal(err)
	}
	count, locks := inventoryTotals(got.groups(engine.Weapons()))
	if len(got.Items) != 2 || count != 1 || locks != 1 || !got.targetReady() {
		t.Fatal(got.Items, count, locks)
	}
}

func TestFourFlawlessRewardsWithTargetQuota(t *testing.T) {
	const exemplar, grandVision = "wpn_claym_0004", "wpn_sword_0021"
	engine := inventoryTestEngine(t)
	drop := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 1, 1}}
	for _, tc := range []struct {
		name     string
		selected []string
		locks    int
	}{
		{"ordinary screening", nil, 4},
		{"quota on a different combination", []string{exemplar}, 4},
		{"one target sharing the drop", []string{grandVision}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testInventoryAccount(t.TempDir(), "aaaaaaaaaaaaaaaa")
			s := newSnapshot(a.id)
			s.Coverage[gold] = tierCoverage{CompleteAt: &s.UpdatedAt}
			if err := a.save(s); err != nil {
				t.Fatal(err)
			}
			st := &RunState{
				Account: a, MatchEngine: engine, RowBoxes: make([][4]int, 4),
				PipelineOpts:              EssenceFilterOptions{Rarity5Weapon: true, Rarity6Weapon: true, LockLimitWeaponIDs: tc.selected},
				MatchedCombinationSummary: make(map[string]*matchapi.SkillCombinationSummary),
			}
			previous := currentRun
			currentRun = st
			t.Cleanup(func() { currentRun = previous })
			passes := []AfterBattlePass{AfterBattlePassExecute}
			if len(tc.selected) > 0 {
				passes = []AfterBattlePass{AfterBattlePassReadOnly, AfterBattlePassExecute}
			}
			for _, pass := range passes {
				st.PipelineOpts.AfterBattlePass = pass
				if err := prepareReward(st); err != nil {
					t.Fatal(err)
				}
				if !prepareLockLimit(nil, st, &st.PipelineOpts) {
					t.Fatal("quota planning failed")
				}
				st.Loot.quality, st.Loot.mark = gold, unmarked
				for i := 1; i <= 4; i++ {
					st.RowIndex = i
					want := unmarked
					if pass == AfterBattlePassExecute && i <= tc.locks {
						want = lockedMark
					}
					mark, ok := decideRewardMark(nil, st, drop)
					if !ok || mark != want {
						t.Fatalf("%s reward %d: got %q, want %q", pass, i, mark, want)
					}
					if mark == lockedMark && !(&EssenceFilterConfirmMarkAction{}).Run(nil, &maa.CustomActionArg{CurrentTaskName: "EssenceFilterAfterBattleCheckLocked"}) {
						t.Fatal("lock confirmation failed", i)
					}
				}
				if !commitAfterBattleInventory(st, a.id) {
					t.Fatal("failed to finish pass", pass)
				}
			}
			saved, err := a.load(engine)
			if err != nil || !saved.targetReady() {
				t.Fatal("next target run cannot reuse the batch", err)
			}
			groups := saved.groups(engine.Weapons())
			count, locks := inventoryTotals(groups)
			stock := stockFromGroups(groups)
			if len(saved.Items) != 4 || count != tc.locks || locks != tc.locks || stock.Count([]string{grandVision}) != tc.locks || len(stock.MissingWeapons([]string{grandVision})) != 0 {
				t.Fatal("incorrect saved stock or target deficit", len(saved.Items), count, locks, groups)
			}
		})
	}
}

func TestSharedTargetsRequireSeparateUsableEssences(t *testing.T) {
	const twelveQuestions, grandVision = "wpn_sword_0018", "wpn_sword_0021"
	ocr := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 1, 1}}
	engine := inventoryTestEngine(t)
	item, err := observeItem(engine, ocr, gold, unmarked)
	if err != nil {
		t.Fatal(err)
	}
	selected := []string{grandVision, twelveQuestions}
	s := newSnapshot("")
	for _, tc := range []struct {
		quality, mark string
		missing       []string
	}{
		{purple, lockedMark, []string{twelveQuestions, grandVision}},
		{gold, discarded, []string{twelveQuestions, grandVision}},
		{gold, unmarked, []string{twelveQuestions, grandVision}},
		{gold, lockedMark, []string{grandVision}},
		{gold, lockedMark, []string{}},
	} {
		item.Quality, item.Mark = tc.quality, tc.mark
		s.Items = append(s.Items, item)
		stock := stockFromGroups(s.groups(engine.Weapons()))
		if tc.quality == gold && tc.mark == lockedMark {
			for _, id := range selected {
				if got := stock.MissingWeapons([]string{id}); len(got) != 0 {
					t.Fatalf("previous selection reserved the shared essence: %v", got)
				}
			}
		}
		if got := stock.MissingWeapons(selected); !reflect.DeepEqual(got, tc.missing) {
			t.Errorf("after %s/%s: got %v missing, want %v", tc.quality, tc.mark, got, tc.missing)
		}
	}
}

func TestWarehouseRefreshAndReportUseConfirmedMarks(t *testing.T) {
	engine := inventoryTestEngine(t)
	ocr := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{1, 2, 1}}
	a := testInventoryAccount(t.TempDir(), "")
	previous := currentRun
	t.Cleanup(func() { currentRun = previous })
	for _, tc := range []struct {
		mode             InitMode
		original, action string
		available        int
	}{
		{InitModeInventory, unmarked, unmarked, 0},
		{InitModeInventory, lockedMark, unmarked, 1},
		{InitModeScreenAndReport, unmarked, lockedMark, 1},
	} {
		if err := a.begin(engine); err != nil {
			t.Fatal(err)
		}
		st := &RunState{
			Account: a, MatchEngine: engine, Scan: newWarehouseScan(),
			PipelineOpts:              EssenceFilterOptions{Mode: tc.mode},
			MatchedCombinationSummary: make(map[string]*matchapi.SkillCombinationSummary),
		}
		currentRun = st
		st.Scan.current = gridSelection{Row: 0, Col: 0, Quality: gold, Mark: tc.original}
		if got, ok := decideWarehouseMark(nil, st, ocr); !ok || got != tc.action {
			t.Fatalf("%s requested %q", tc.mode, got)
		}
		if tc.action == lockedMark && !(&EssenceFilterConfirmMarkAction{}).Run(nil, &maa.CustomActionArg{CurrentTaskName: "EssenceFilterCheckLocked"}) {
			t.Fatal("expected lock verification")
		}
		unread := &EssenceFilterUnreadPurpleAction{}
		st.Scan.current.Col = 1
		for _, quality := range []string{gold, "unknown"} {
			st.Scan.current.Quality = quality
			if unread.Run(nil, &maa.CustomActionArg{}) {
				t.Fatal("accepted missing detail for", quality)
			}
		}
		end := gridSelection{Expected: 2, Issued: 2, Resolved: 2}
		if st.Scan.verify(end) == nil {
			t.Fatal("accepted a missing cell")
		}
		st.Scan.current.Quality = purple
		if !unread.Run(nil, &maa.CustomActionArg{}) || unread.Run(nil, &maa.CustomActionArg{}) {
			t.Fatal("purple cell was not recorded exactly once")
		}
		if err := st.Scan.verify(end); err != nil || !finishWarehouse(nil, st) {
			t.Fatal("warehouse did not finish", err)
		}
		s, err := a.load(engine)
		if err != nil || !s.targetReady() || s.Coverage[purple].CompleteAt != nil || s.Coverage[purple].Unread != 1 || len(s.Items) != 1 {
			t.Fatalf("incorrect coverage: %+v, %v", s, err)
		}
		if got := stockFromGroups(s.groups(engine.Weapons())).Count([]string{"wpn_sword_0021"}); got != tc.available {
			t.Fatalf("%s stock %d, want %d", tc.mode, got, tc.available)
		}
	}
	report, err := os.ReadFile(filepath.Join(a.dir, "EssenceInventory", "unverified.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(report, &fields); err != nil || fields["account_id"] != nil {
		t.Fatal("expected anonymous report without account_id", err)
	}
	data, err := json.Marshal(a.local)
	if err != nil || !reflect.DeepEqual(jsonValue(t, data), jsonValue(t, report)) {
		t.Fatal("report differs from confirmed in-memory inventory", err)
	}
	fresh, err := testInventoryAccount(a.dir, "").load(engine)
	if err != nil || fresh.targetReady() || len(fresh.Items) != 0 {
		t.Fatal("another task inherited the anonymous report", err)
	}
}

func TestRewardUnreadPurpleRetainsGoldCoverage(t *testing.T) {
	r := newRewardInventory()
	r.quality, r.mark = purple, unmarked
	st := &RunState{Loot: r, RowIndex: 1}
	previous := currentRun
	currentRun = st
	t.Cleanup(func() { currentRun = previous })
	fallback := &EssenceFilterUnreadPurpleAction{}
	if !fallback.Run(nil, &maa.CustomActionArg{}) || r.check(1) != nil {
		t.Fatal("confirmed purple position lost")
	}
	clear(r.expectedMarks)
	r.quality = gold
	if fallback.Run(nil, &maa.CustomActionArg{}) || r.check(1) == nil {
		t.Fatal("trusted a missing gold observation")
	}
	r.quality = purple
	if !fallback.Run(nil, &maa.CustomActionArg{}) {
		t.Fatal("failed to reobserve the same purple reward")
	}
	s := newSnapshot("")

	s.Coverage[gold] = tierCoverage{CompleteAt: &s.UpdatedAt}
	s.Coverage[purple] = s.Coverage[gold]
	after := r.appendTo(s)
	if !after.targetReady() || after.Coverage[purple].Unread != 1 || after.Coverage[purple].CompleteAt != nil || len(after.Items) != 0 || s.Coverage[purple].CompleteAt == nil {
		t.Fatalf("incorrect reward coverage: %+v", after)
	}
}

func TestTargetQuotaUsesCurrentDropsAndExistingLocks(t *testing.T) {
	engine := inventoryTestEngine(t)
	ocr := matchapi.OCRInput{Skills: [3]string{"敏捷", "攻击", "附术"}, Levels: [3]int{3, 3, 2}}
	drop, err := observeItem(engine, ocr, gold, unmarked)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mark    string
		levels  [3]int
		lockNew bool
	}{
		{unmarked, [3]int{6, 6, 3}, true},
		{lockedMark, [3]int{1, 1, 1}, false},
	} {
		base := newSnapshot("")
		base.Coverage[gold] = tierCoverage{CompleteAt: &base.UpdatedAt}
		old, err := observeItem(engine, matchapi.OCRInput{Skills: ocr.Skills, Levels: tc.levels}, gold, tc.mark)
		if err != nil {
			t.Fatal(err)
		}
		base.Items = append(base.Items, old)
		st := &RunState{
			Account: &inventoryAccount{base: base}, MatchEngine: engine, Loot: newRewardInventory(), RowIndex: 1,
			PipelineOpts:              EssenceFilterOptions{Mode: InitModeAfterBattle, AfterBattlePass: AfterBattlePassExecute, LockLimitWeaponIDs: []string{"wpn_sword_0021"}},
			MatchedCombinationSummary: make(map[string]*matchapi.SkillCombinationSummary),
		}
		st.Loot.staged, st.Loot.items[1] = true, drop
		st.Loot.quality, st.Loot.mark = gold, unmarked
		if !prepareLockLimit(nil, st, &st.PipelineOpts) {
			t.Fatal("quota setup failed")
		}
		got, ok := decideRewardMark(nil, st, ocr)
		if !ok || (got == lockedMark) != tc.lockNew {
			t.Fatalf("existing %s: got %q", tc.mark, got)
		}
	}
}
