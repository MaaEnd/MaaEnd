package essencefilter

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

func TestExpectedEnhanceAttempts(t *testing.T) {
	cases := []struct {
		name   string
		levels [3]int
		want   float64
		wantOK bool
	}{
		{"maxed", [3]int{6, 6, 3}, 0, true},
		{"fresh", [3]int{1, 1, 1}, 177.073, true},
		{"slot3_target_only", [3]int{6, 6, 2}, 23.810, true},
		{"slots_1_2_to_6_slot3_to_3", [3]int{5, 5, 2}, 97.884, true},
		{"slot1_out_of_range_low", [3]int{0, 1, 1}, 0, false},
		{"slot2_out_of_range_high", [3]int{1, 7, 1}, 0, false},
		{"slot3_out_of_range_high", [3]int{1, 1, 4}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ExpectedEnhanceAttempts(tc.levels)
			if ok != tc.wantOK {
				t.Fatalf("ExpectedEnhanceAttempts(%v) ok = %v, want %v", tc.levels, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if math.Abs(got-tc.want) > 1e-3 {
				t.Fatalf("ExpectedEnhanceAttempts(%v) = %.4f, want %.3f", tc.levels, got, tc.want)
			}
		})
	}
}

func testWeapon(internalID string, rarity int, ids [3]int) matchapi.WeaponData {
	return matchapi.WeaponData{
		InternalID:    internalID,
		ChineseName:   internalID,
		Rarity:        rarity,
		SkillIDs:      []int{ids[0], ids[1], ids[2]},
		SkillsChinese: []string{"s1", "s2", "s3"},
	}
}

func testGroup(weaponIDs []string, essences ...inventoryEssence) inventoryGroup {
	return inventoryGroup{WeaponIDs: weaponIDs, Essences: essences}
}

type lockLimitScan struct {
	skillIDs []int
	levels   [3]int
	locked   bool
}

func TestLockLimitPlannerRankingByExpectedAttempts(t *testing.T) {
	combo := [3]int{1, 2, 3}
	weapons := []matchapi.WeaponData{
		testWeapon("w6", 6, combo),
		testWeapon("w5", 5, combo),
		testWeapon("w4", 4, combo),
	}
	groups := []inventoryGroup{
		testGroup([]string{"w4", "w5", "w6"},
			inventoryEssence{Levels: [3]int{1, 1, 1}, Count: 3},
			inventoryEssence{Levels: [3]int{6, 6, 3}, Count: 1},
		),
	}
	planner := newLockLimitPlanner(groups, weapons)

	ids := []int{combo[0], combo[1], combo[2]}
	scan := []lockLimitScan{
		{skillIDs: ids, levels: [3]int{1, 1, 1}},
		{skillIDs: ids, levels: [3]int{1, 1, 1}},
		{skillIDs: ids, levels: [3]int{6, 6, 3}},
		{skillIDs: ids, levels: [3]int{1, 1, 1}},
	}
	var got []bool
	for _, item := range scan {
		got = append(got, planner.decide(item.skillIDs, item.levels, item.locked).Lock)
	}
	want := []bool{true, false, true, false}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("decision %d: lock = %v, want %v (all: %v)", i, got[i], want[i], got)
		}
	}
}

func TestLockLimitPlannerUnlockNeeded(t *testing.T) {
	combo := [3]int{1, 2, 3}
	weapons := []matchapi.WeaponData{
		testWeapon("w6", 6, combo),
		testWeapon("w5", 5, combo),
	}
	groups := []inventoryGroup{
		testGroup([]string{"w5", "w6"},
			inventoryEssence{Levels: [3]int{1, 1, 1}, Count: 1, Locked: 1},
			inventoryEssence{Levels: [3]int{5, 5, 2}, Count: 1},
			inventoryEssence{Levels: [3]int{6, 6, 3}, Count: 1},
		),
	}
	planner := newLockLimitPlanner(groups, weapons)
	ids := []int{combo[0], combo[1], combo[2]}

	type scanStep struct {
		levels [3]int
		locked bool
	}
	steps := []scanStep{
		{levels: [3]int{1, 1, 1}, locked: true},
		{levels: [3]int{5, 5, 2}},
		{levels: [3]int{6, 6, 3}},
		{levels: [3]int{1, 1, 1}, locked: true},
	}
	wantLock := []bool{false, true, true, false}
	wantUnlock := []bool{true, false, false, true}
	for i, step := range steps {
		got := planner.decide(ids, step.levels, step.locked)
		if got.Lock != wantLock[i] || got.NeedsUnlock != wantUnlock[i] {
			t.Fatalf("step %d %+v: got lock=%v unlock=%v, want lock=%v unlock=%v",
				i, step, got.Lock, got.NeedsUnlock, wantLock[i], wantUnlock[i])
		}
	}

	summaries := planner.overLimitSummaries()
	if len(summaries) != 1 {
		t.Fatalf("summary count = %d, want 1", len(summaries))
	}
	got := summaries[0]
	if got.Quota != 2 || got.Locked != 3 || got.Excess != 1 {
		t.Fatalf("summary = %+v, want quota 2, locked 3, excess 1", got)
	}
	if len(got.Rows) != 1 || got.Rows[0].Levels != [3]int{1, 1, 1} || got.Rows[0].Count != 1 {
		t.Fatalf("summary rows = %+v, want one +1/+1/+1 row with count 1", got.Rows)
	}
}

func TestLockLimitPlannerLockedCopiesConsumeQuota(t *testing.T) {
	solo := [3]int{1, 2, 3}
	duo := [3]int{4, 5, 6}
	weapons := []matchapi.WeaponData{
		testWeapon("a6", 6, solo),
		testWeapon("b6", 6, duo),
		testWeapon("b5", 5, duo),
	}
	groups := []inventoryGroup{
		testGroup([]string{"a6"},
			inventoryEssence{Levels: [3]int{6, 6, 3}, Count: 2, Locked: 1},
		),
		testGroup([]string{"b5", "b6"},
			inventoryEssence{Levels: [3]int{6, 6, 3}, Count: 3, Locked: 1},
		),
	}
	planner := newLockLimitPlanner(groups, weapons)
	soloIDs := []int{solo[0], solo[1], solo[2]}
	duoIDs := []int{duo[0], duo[1], duo[2]}

	if got := planner.decide(soloIDs, [3]int{6, 6, 3}, false); got.Lock {
		t.Fatalf("quota filled by the locked copy: got lock, want skip")
	}

	want := []bool{true, false, false}
	for i := range want {
		got := planner.decide(duoIDs, [3]int{6, 6, 3}, false)
		if got.Lock != want[i] {
			t.Fatalf("duo scan %d: lock = %v, want %v", i, got.Lock, want[i])
		}
	}
}

func TestLockLimitPlannerCombinationsAreIndependent(t *testing.T) {
	first := [3]int{1, 2, 3}
	second := [3]int{4, 5, 6}
	weapons := []matchapi.WeaponData{
		testWeapon("a6", 6, first),
		testWeapon("b6", 6, second),
	}
	groups := []inventoryGroup{
		testGroup([]string{"a6"}, inventoryEssence{Levels: [3]int{1, 1, 1}, Count: 2}),
		testGroup([]string{"b6"}, inventoryEssence{Levels: [3]int{1, 1, 1}, Count: 2}),
	}
	planner := newLockLimitPlanner(groups, weapons)
	firstIDs := []int{first[0], first[1], first[2]}
	secondIDs := []int{second[0], second[1], second[2]}

	scans := []struct {
		ids []int
	}{
		{firstIDs}, {firstIDs}, {secondIDs}, {secondIDs},
	}
	want := []bool{true, false, true, false}
	for i, scan := range scans {
		if got := planner.decide(scan.ids, [3]int{1, 1, 1}, false); got.Lock != want[i] {
			t.Fatalf("scan %d: lock = %v, want %v", i, got.Lock, want[i])
		}
	}
}

func TestLockLimitPlannerMissingInventoryAndFourStarOnly(t *testing.T) {
	known := [3]int{1, 2, 3}
	absent := [3]int{2, 3, 4}
	fourStarOnly := [3]int{7, 8, 9}
	weapons := []matchapi.WeaponData{
		testWeapon("w6", 6, known),
		testWeapon("w5", 5, absent),
		testWeapon("w4", 4, fourStarOnly),
	}
	// The group also carries an illegal level row, which must stay out of the ranking.
	groups := []inventoryGroup{
		testGroup([]string{"w6"},
			inventoryEssence{Levels: [3]int{6, 6, 3}, Count: 1},
			inventoryEssence{Levels: [3]int{1, 1, 4}, Count: 5, Locked: 5},
		),
	}
	planner := newLockLimitPlanner(groups, weapons)

	knownIDs := []int{known[0], known[1], known[2]}
	absentIDs := []int{absent[0], absent[1], absent[2]}
	fourIDs := []int{fourStarOnly[0], fourStarOnly[1], fourStarOnly[2]}

	if got := planner.decide(knownIDs, [3]int{5, 5, 5}, false); !got.MissingRow || got.Lock {
		t.Fatalf("missing level row: got %+v, want MissingRow without lock", got)
	}
	if got := planner.decide(knownIDs, [3]int{1, 1, 4}, false); !got.MissingRow || got.Lock {
		t.Fatalf("illegal level row: got %+v, want MissingRow without lock", got)
	}
	if got := planner.decide(absentIDs, [3]int{6, 6, 3}, false); !got.MissingGroup || got.Lock {
		t.Fatalf("missing group: got %+v, want MissingGroup without lock", got)
	}
	if got := planner.decide(fourIDs, [3]int{1, 1, 1}, false); got.MissingGroup || got.MissingRow || got.Lock {
		t.Fatalf("four-star-only combination: got %+v, want silent skip", got)
	}
	if got := planner.decide(knownIDs, [3]int{6, 6, 3}, false); !got.Lock || got.Quota != 1 {
		t.Fatalf("legal optimal row: got %+v, want lock with quota 1", got)
	}
	if summaries := planner.overLimitSummaries(); len(summaries) != 0 {
		t.Fatalf("illegal rows leaked into over-limit accounting: %+v", summaries)
	}
}

func TestLockLimitPlannerQuotaFromEngineWeapons(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dataDir := filepath.Join(filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..")), "assets", "data", "EssenceFilter")
	engine, err := matchapi.NewEngineFromDirWithLocale(dataDir, "CN")
	if err != nil {
		t.Fatal(err)
	}

	planner := newLockLimitPlanner(nil, engine.Weapons())
	highRarity := make(map[[3]int]bool)
	for _, weapon := range engine.Weapons() {
		ids, ok := skillIDsTriple(weapon.SkillIDs)
		if !ok || (weapon.Rarity != 5 && weapon.Rarity != 6) {
			continue
		}
		highRarity[ids] = true
	}

	var sixStar, fourStarOnly [3]int
	var hasSixStar, hasFourStarOnly bool
	for _, weapon := range engine.Weapons() {
		ids, ok := skillIDsTriple(weapon.SkillIDs)
		if !ok {
			continue
		}
		if !hasSixStar && weapon.Rarity == 6 {
			sixStar, hasSixStar = ids, true
		}
		if !hasFourStarOnly && weapon.Rarity == 4 && !highRarity[ids] {
			fourStarOnly, hasFourStarOnly = ids, true
		}
	}
	if !hasSixStar || !hasFourStarOnly {
		t.Fatalf("unexpected engine data: six-star=%v four-star-only=%v", hasSixStar, hasFourStarOnly)
	}

	if got := planner.decide([]int{sixStar[0], sixStar[1], sixStar[2]}, [3]int{1, 1, 1}, false); !got.MissingGroup || got.Quota < 1 {
		t.Fatalf("six-star combination absent from an empty inventory: got %+v, want MissingGroup", got)
	}
	if got := planner.decide([]int{fourStarOnly[0], fourStarOnly[1], fourStarOnly[2]}, [3]int{1, 1, 1}, false); got.MissingGroup || got.Lock {
		t.Fatalf("four-star-only combination: got %+v, want silent skip", got)
	}
}

func TestLoadInventoryGroupsRequired(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	empty := filepath.Join(dir, "empty.json")
	broken := filepath.Join(dir, "broken.json")
	valid := filepath.Join(dir, "valid.json")
	null := filepath.Join(dir, "null.json")
	for path, content := range map[string]string{empty: "  \n", null: "null", broken: "{", valid: `[{"weapon_ids":["w6"],"essences":[{"levels":[6,6,3],"count":1,"locked":0}]}]`} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for name, path := range map[string]string{"missing": missing, "empty": empty, "null": null, "broken": broken} {
		if _, err := loadInventoryGroupsRequired(path); err == nil {
			t.Fatalf("%s inventory: want error, got nil", name)
		}
	}
	for name, path := range map[string]string{"missing": missing, "empty": empty, "null": null} {
		if groups, err := loadInventoryGroups(path); err != nil || groups != nil {
			t.Fatalf("%s inventory (tolerant read): got (%v, %v), want (nil, nil)", name, groups, err)
		}
	}
	if _, err := loadInventoryGroups(broken); err == nil {
		t.Fatalf("broken inventory (tolerant read): want error, got nil")
	}

	groups, err := loadInventoryGroupsRequired(valid)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Essences[0].Count != 1 {
		t.Fatalf("valid inventory: got %+v", groups)
	}
}
