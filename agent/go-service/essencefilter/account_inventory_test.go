package essencefilter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

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

func TestWarehouseObservesEveryPosition(t *testing.T) {
	engine := inventoryTestEngine(t)
	scan := newWarehouseScan()
	exact := matchapi.OCRInput{Skills: [3]string{"力量", "攻击", "压制"}, Levels: [3]int{1, 2, 3}}
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
		if err := scan.observe(engine, input); err != nil {
			t.Fatal(err)
		}
		if err := scan.observe(engine, input); err != nil {
			t.Fatal(err)
		}
	}
	if len(scan.items) != 4 || len(scan.items[3].WeaponIDs) != 0 || scan.items[3].Skills[2].Pool != 1 || scan.items[3].Skills[2].Level != 6 {
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
	groups := s.groups()
	count, locked := inventoryTotals(groups)
	if count != 2 || locked != 1 || stockFromGroups(groups).Count([]string{"wpn_claym_0003"}) != 2 {
		t.Fatal(groups)
	}
	if err := scan.observe(engine, exact); err == nil {
		t.Fatal("same coordinate accepted different OCR")
	}
}

func TestInventoryTransactionsAndPortableExports(t *testing.T) {
	dir := t.TempDir()
	a := &inventoryAccount{id: "aaaaaaaaaaaaaaaa", dir: dir}
	b := &inventoryAccount{id: "bbbbbbbbbbbbbbbb", dir: dir}
	if err := a.begin(); err != nil {
		t.Fatal(err)
	}
	other := &inventoryAccount{id: a.id, dir: dir}
	if err := other.begin(); err == nil {
		t.Fatal("concurrent writer admitted")
	}
	s := newSnapshot(a.id)
	s.DataVersion = "v"
	s.ScannedAt[gold] = time.Now().UTC()
	s.ScannedAt[purple] = s.ScannedAt[gold]
	if err := a.commit(s, true); err != nil {
		t.Fatal(err)
	}
	stored, err := other.load()
	if err != nil || !stored.targetReady("v") || stored.targetReady("older") {
		t.Fatalf("resume %+v: %v", stored, err)
	}
	legacy := filepath.Join(dir, inventoryExportPath)
	public := filepath.Join(dir, "EssenceInventoryFull.json")
	before, err := os.ReadFile(public)
	if err != nil {
		t.Fatal(err)
	}
	var full inventorySnapshot
	if err := json.Unmarshal(before, &full); err != nil || !reflect.DeepEqual(&full, stored) {
		t.Fatal("full export differs from account file", err)
	}
	groups, err := loadInventoryGroupsRequired(legacy)
	if err != nil || !reflect.DeepEqual(groups, stored.groups()) {
		t.Fatal("legacy projection differs", err)
	}
	if err := a.begin(); err != nil {
		t.Fatal(err)
	}
	if err := a.cancelClaim(); err != nil {
		t.Fatal(err)
	}
	cancelled, err := a.load()
	if err != nil || !cancelled.targetReady("v") || !cancelled.UpdatedAt.Equal(stored.UpdatedAt) {
		t.Fatal("rejected claim invalidated verified inventory", err)
	}
	if err := a.begin(); err != nil {
		t.Fatal(err)
	}
	a.release() // agent stops after clicking the claim button
	stale, err := other.load()
	if err != nil || stale.targetReady("v") {
		t.Fatalf("interrupted claim trusted: %+v %v", stale, err)
	}
	still, _ := os.ReadFile(public)
	if string(still) != string(before) {
		t.Fatal("published an interrupted scan")
	}
	if err := other.begin(); err != nil {
		t.Fatal(err)
	}
	if len(other.base.ScannedAt) != 0 {
		t.Fatal("next reward restored interrupted coverage")
	}
	other.release()
	unrelated, err := b.load()
	if err != nil || unrelated.targetReady("v") {
		t.Fatal("used another account's portable export", err)
	}
}

func TestAccountUIDReuseAndAnonymousLifetime(t *testing.T) {
	dir := t.TempDir()
	count := 0
	sample := func() string { count++; return "" }
	for _, id := range []int64{10, 11, 12} {
		t.Cleanup(func() { releaseInventoryTask(id) })
	}
	known := bindInventoryAccount(10, dir, "aaaaaaaaaaaaaaaa", sample)
	if count != 0 || known.id == "" {
		t.Fatal("sampled despite cached UID")
	}
	anonymous := bindInventoryAccount(11, dir, "", sample)
	if bindInventoryAccount(11, dir, "", sample) != anonymous || count != 1 {
		t.Fatal("resampled unknown within task")
	}
	if err := anonymous.begin(); err != nil {
		t.Fatal(err)
	}
	s := newSnapshot("")
	s.DataVersion = "v"
	s.ScannedAt[gold] = time.Now().UTC()
	if err := anonymous.commit(s, false); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatal("persisted anonymous account", entries, err)
	}
	releaseInventoryTask(11)
	next := bindInventoryAccount(12, dir, "", sample)
	s, err := next.load()
	if err != nil || count != 2 || s.targetReady("v") {
		t.Fatal("unknown stock survived into another task")
	}
	newAccount := bindInventoryAccount(10, dir, "", func() string { return "bbbbbbbbbbbbbbbb" })
	if newAccount.id == known.id {
		t.Fatal("cleared UID cache reused old account")
	}
}

func TestRewardTwoPassCommitsOnce(t *testing.T) {
	a := &inventoryAccount{dir: t.TempDir(), id: "aaaaaaaaaaaaaaaa"}
	engine := inventoryTestEngine(t)
	if err := a.begin(); err != nil {
		t.Fatal(err)
	}
	s := newSnapshot(a.id)
	s.ScannedAt[gold] = time.Now().UTC()
	s.DataVersion = engine.DataVersion()
	if err := a.commit(s, true); err != nil {
		t.Fatal(err)
	}
	st := &RunState{Account: a, MatchEngine: engine, RowBoxes: make([][4]int, 2)}
	st.PipelineOpts.AfterBattlePass = AfterBattlePassReadOnly
	if err := prepareReward(st); err != nil {
		t.Fatal(err)
	}
	item, err := observeItem(engine, matchapi.OCRInput{Skills: [3]string{"力量", "攻击", "压制"}, Levels: [3]int{1, 1, 1}}, gold, unmarked)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Loot.observe(1, item); err != nil {
		t.Fatal(err)
	}
	if commitAfterBattleInventory(st, true) {
		t.Fatal("accepted incomplete first pass")
	}
	if err := st.Loot.observe(2, item); err != nil {
		t.Fatal(err)
	}
	if !commitAfterBattleInventory(st, true) {
		t.Fatal("staging failed")
	}
	st.PipelineOpts.AfterBattlePass = AfterBattlePassExecute
	if err := prepareReward(st); err != nil {
		t.Fatal(err)
	}
	previous := currentRun
	currentRun = st
	t.Cleanup(func() { currentRun = previous })
	for i, node := range []string{"EssenceFilterAfterBattleCheckLocked", "EssenceFilterAfterBattleCheckDiscarded"} {
		st.RowIndex = i + 1
		if err := st.Loot.observe(st.RowIndex, item); err != nil {
			t.Fatal(err)
		}
		action := EssenceFilterConfirmMarkAction{}
		if !action.Run(nil, &maa.CustomActionArg{TaskID: st.TaskID, CurrentTaskName: node}) {
			t.Fatal(node)
		}
	}
	if !commitAfterBattleInventory(st, false) || !commitAfterBattleInventory(st, false) {
		t.Fatal("commit/retry failed")
	}
	got, err := a.load()
	if err != nil {
		t.Fatal(err)
	}
	count, locks := inventoryTotals(got.groups())
	if len(got.Items) != 2 || count != 1 || locks != 1 || !got.targetReady(engine.DataVersion()) {
		t.Fatal(got.Items, count, locks)
	}
}

func TestFailedPublicationRequiresRefresh(t *testing.T) {
	a := &inventoryAccount{dir: t.TempDir(), id: "aaaaaaaaaaaaaaaa"}
	if err := a.begin(); err != nil {
		t.Fatal(err)
	}
	s := newSnapshot(a.id)
	s.DataVersion = "v"
	s.ScannedAt[gold] = time.Now().UTC()
	// A directory occupying the public export path makes replacement fail on every OS.
	if err := os.Mkdir(filepath.Join(a.dir, inventoryExportPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := a.commit(s, true); err == nil {
		t.Fatal("reported publication complete")
	}
	got, err := a.load()
	if err != nil || !got.Pending || got.targetReady("v") {
		t.Fatal("unpublished cache reused", err)
	}
}
