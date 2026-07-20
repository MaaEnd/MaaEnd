package sellproduct

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSellProductCacheReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"

	updatedAt := now.Format(time.RFC3339)
	if err := writeSellProductCache(path, sellProductCache{
		UpdatedAt: updatedAt,
		Accounts: map[string]sellProductCacheAccount{
			uid: {
				UpdatedAt: updatedAt,
				Operators: []string{"佩丽卡", "陈千语", "佩丽卡", ""},
			},
		},
	}); err != nil {
		t.Fatalf("writeSellProductCache: %v", err)
	}
	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("readSellProductCache: %v", err)
	}
	if cache.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("updated_at = %q", cache.UpdatedAt)
	}
	account := cache.Accounts[uid]
	if account.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("account updated_at = %q", account.UpdatedAt)
	}
	want := []string{"佩丽卡", "陈千语"}
	if !reflect.DeepEqual(account.Operators, want) {
		t.Fatalf("operators = %#v, want %#v", account.Operators, want)
	}
}

func TestDefaultSellProductCachePathIsSingleFile(t *testing.T) {
	tests := []struct {
		name string
		uid  string
		want string
	}{
		{
			name: "hashed uid",
			uid:  "abc123",
			want: filepath.Join("debug", "record", sellProductCacheFileName),
		},
		{
			name: "empty uid",
			uid:  "",
			want: filepath.Join("debug", "record", sellProductCacheFileName),
		},
		{
			name: "unsafe uid",
			uid:  "../uid value",
			want: filepath.Join("debug", "record", sellProductCacheFileName),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultSellProductCachePath(tt.uid); got != tt.want {
				t.Fatalf("defaultSellProductCachePath(%q) = %q, want %q", tt.uid, got, tt.want)
			}
		})
	}
}

func TestSellProductCacheMigratesLegacyFileName(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, legacySellProductCacheFileName)
	newPath := filepath.Join(dir, sellProductCacheFileName)
	if err := writeSellProductCache(legacyPath, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"abc123": {
				Operators: []string{"狼卫"},
				Locations: map[string]bool{"Full": true},
			},
		},
	}); err != nil {
		t.Fatalf("准备旧版缓存失败：%v", err)
	}

	cache, err := readSellProductCache(newPath)
	if err != nil {
		t.Fatalf("迁移旧版缓存失败：%v", err)
	}
	if !sellProductCacheHasOperatorSnapshot(cache, "abc123") {
		t.Fatal("迁移后完整干员快照丢失")
	}
	if reached := cache.Accounts["abc123"].Locations["Full"]; !reached {
		t.Fatal("迁移后据点发展值状态丢失")
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("迁移后缺少新缓存文件：%v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("迁移后旧缓存文件仍存在：%v", err)
	}
}

func TestSellProductCacheMissingAndEmpty(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	cache, err := readSellProductCache(missing)
	if err != nil {
		t.Fatalf("missing cache should not error: %v", err)
	}
	if len(cache.Accounts) != 0 {
		t.Fatalf("missing cache accounts = %#v", cache.Accounts)
	}

	empty := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(empty, nil, 0644); err != nil {
		t.Fatal(err)
	}
	cache, err = readSellProductCache(empty)
	if err != nil {
		t.Fatalf("empty cache should not error: %v", err)
	}
	if len(cache.Accounts) != 0 {
		t.Fatalf("empty cache accounts = %#v", cache.Accounts)
	}
}

func TestSellProductCacheRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	if err := os.WriteFile(path, []byte(`{"updated_at":"","accounts":{},"unexpected":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSellProductCache(path); err == nil {
		t.Fatal("cache with unknown fields should be rejected")
	}
}

func TestNormalizeOperatorCandidates(t *testing.T) {
	got := normalizeOperatorCandidates([]operatorCandidate{
		{Name: "Beta", CacheName: "贝塔", Expected: []string{"贝塔"}, Priority: 2},
		{Name: "", CacheName: "忽略", Expected: []string{"忽略"}, Priority: 0},
		{Name: "Alpha", CacheName: "阿尔法", Expected: []string{"阿尔法", "阿尔法", ""}, Priority: 1},
		{Name: "Beta", Expected: []string{"重复"}, Priority: 0},
	})
	want := []operatorCandidate{
		{Name: "Alpha", CacheName: "阿尔法", Expected: []string{"阿尔法"}, Priority: 1},
		{Name: "Beta", CacheName: "贝塔", Expected: []string{"贝塔"}, Priority: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeOperatorCandidates = %#v, want %#v", got, want)
	}
}

func TestFilterOwnedCandidatesUsesCacheName(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Both", CacheName: "双加成", Priority: 0},
		{Name: "Money", CacheName: "收益", Priority: 1},
		{Name: "Exp", CacheName: "经验", Priority: 2},
	}
	owned := operatorNameSet([]string{"经验", "双加成"})
	got := filterOwnedCandidates(candidates, owned)
	want := []operatorCandidate{
		{Name: "Both", CacheName: "双加成", Priority: 0},
		{Name: "Exp", CacheName: "经验", Priority: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterOwnedCandidates = %#v, want %#v", got, want)
	}
}

func TestSellProductCacheHasOperatorSnapshot(t *testing.T) {
	uid := "abc123"
	if sellProductCacheHasOperatorSnapshot(sellProductCache{}, uid) {
		t.Fatal("empty cache should not be treated as a snapshot")
	}
	if !sellProductCacheHasOperatorSnapshot(sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid: {Operators: []string{"佩丽卡"}},
		},
	}, uid) {
		t.Fatal("account cache should be treated as a snapshot")
	}
	if sellProductCacheHasOperatorSnapshot(sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"other": {Operators: []string{"佩丽卡"}},
		},
	}, uid) {
		t.Fatal("cache without this uid should not be treated as a snapshot")
	}
	if sellProductCacheHasOperatorSnapshot(sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid: {Operators: nil},
		},
	}, uid) {
		t.Fatal("account without an operator scan should not be treated as complete")
	}
	if !sellProductCacheHasOperatorSnapshot(sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid: {Operators: []string{}},
		},
	}, uid) {
		t.Fatal("an explicitly empty operator snapshot should still be treated as complete")
	}
}

func TestSellProductCachePersistsOperatorSnapshotPresence(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	if err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"locations-only": {
				Locations: map[string]bool{"Full": true},
			},
			"empty-snapshot": {
				Operators: []string{},
			},
		},
	}); err != nil {
		t.Fatalf("写入缓存失败：%v", err)
	}
	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("读取缓存失败：%v", err)
	}
	if sellProductCacheHasOperatorSnapshot(cache, "locations-only") {
		t.Fatal("operators: null 落盘后不应变成完整快照")
	}
	if !sellProductCacheHasOperatorSnapshot(cache, "empty-snapshot") {
		t.Fatal("operators: [] 落盘后应保持完整空快照语义")
	}
}

func TestMergeOperatorSnapshotReplacesCurrentAccount(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"
	cache := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid:     {Operators: []string{"缓存甲", "缓存乙"}},
			"other": {Operators: []string{"其他账号干员"}},
		},
	}
	got := mergeOperatorSnapshot(
		cache,
		uid,
		[]operatorCandidate{{Name: "CandidateA", CacheName: "候选甲"}, {Name: "CandidateB", CacheName: "候选乙"}},
		[]string{"候选乙"},
		now,
	)
	if got.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("updated_at = %q", got.UpdatedAt)
	}
	if want := []string{"候选乙"}; !reflect.DeepEqual(got.Accounts[uid].Operators, want) {
		t.Fatalf("operators = %#v, want %#v", got.Accounts[uid].Operators, want)
	}
	if want := []string{"其他账号干员"}; !reflect.DeepEqual(got.Accounts["other"].Operators, want) {
		t.Fatalf("other account operators = %#v, want %#v", got.Accounts["other"].Operators, want)
	}
}

func TestSellProductCacheReadWriteLocationsBesideOperators(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	updatedAt := "2026-07-20T03:00:00Z"
	if err := writeSellProductCache(path, sellProductCache{
		UpdatedAt: updatedAt,
		Accounts: map[string]sellProductCacheAccount{
			"abc123": {
				UpdatedAt: updatedAt,
				Operators: []string{"狼卫"},
				Locations: map[string]bool{
					" Full ": true,
					"Open":   false,
					"":       true,
				},
			},
		},
	}); err != nil {
		t.Fatalf("写入统一缓存失败：%v", err)
	}

	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("读取统一缓存失败：%v", err)
	}
	if !sellProductCacheHasOperatorSnapshot(cache, "abc123") {
		t.Fatal("写入据点发展值状态后不应破坏完整干员快照")
	}
	want := map[string]bool{"Full": true, "Open": false}
	if got := outpostProsperityStatusesForUID(cache, "abc123"); !reflect.DeepEqual(got, want) {
		t.Fatalf("据点发展值缓存 = %#v，期望 %#v", got, want)
	}
}

func TestLocationsOnlyAccountIsNotAnOperatorSnapshot(t *testing.T) {
	cache := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"abc123": {Locations: map[string]bool{"Full": true}},
		},
	}
	if sellProductCacheHasOperatorSnapshot(cache, "abc123") {
		t.Fatal("只有 locations 时不应被误判为完整干员快照")
	}
}

func TestUpdateLocationsPreservesOperatorSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	first := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	if err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"account-a": {Operators: []string{"狼卫"}},
			"account-b": {Operators: []string{"秋栗"}},
		},
	}); err != nil {
		t.Fatalf("准备干员缓存失败：%v", err)
	}

	changed, err := updateCachedOutpostProsperity(path, "account-a", "Full", true, first)
	if err != nil || !changed {
		t.Fatalf("首次更新结果 = %v, %v，期望写入缓存", changed, err)
	}
	changed, err = updateCachedOutpostProsperity(path, "account-b", "Open", false, first.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("第二账号更新结果 = %v, %v，期望写入缓存", changed, err)
	}
	changed, err = updateCachedOutpostProsperity(path, "account-a", "Full", true, first.Add(2*time.Minute))
	if err != nil || changed {
		t.Fatalf("相同状态更新结果 = %v, %v，期望跳过写盘", changed, err)
	}

	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("读取更新后的缓存失败：%v", err)
	}
	if want := []string{"狼卫"}; !reflect.DeepEqual(cache.Accounts["account-a"].Operators, want) {
		t.Fatalf("account-a 干员快照 = %#v，期望 %#v", cache.Accounts["account-a"].Operators, want)
	}
	if want := []string{"秋栗"}; !reflect.DeepEqual(cache.Accounts["account-b"].Operators, want) {
		t.Fatalf("account-b 干员快照 = %#v，期望 %#v", cache.Accounts["account-b"].Operators, want)
	}
	if got := cache.Accounts["account-a"].Locations["Full"]; !got {
		t.Fatal("account-a 的满级状态丢失")
	}
	if got, ok := cache.Accounts["account-b"].Locations["Open"]; !ok || got {
		t.Fatalf("account-b 的未满状态 = %v, %v，期望明确保存 false", got, ok)
	}
	if got := cache.Accounts["account-a"].UpdatedAt; got != first.Format(time.RFC3339) {
		t.Fatalf("跳过相同状态后 account-a 更新时间 = %q", got)
	}
}

func TestMergeOperatorCachePreservesLocations(t *testing.T) {
	cache := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"abc123": {Locations: map[string]bool{"Full": true, "Open": false}},
		},
	}
	merged := mergeOperatorSnapshot(
		cache,
		"abc123",
		[]operatorCandidate{{Name: "Wulfgard", CacheName: "狼卫"}},
		[]string{"狼卫"},
		time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC),
	)
	want := map[string]bool{"Full": true, "Open": false}
	if got := outpostProsperityStatusesForUID(merged, "abc123"); !reflect.DeepEqual(got, want) {
		t.Fatalf("刷新干员快照后的据点发展值状态 = %#v，期望 %#v", got, want)
	}
	if !sellProductCacheHasOperatorSnapshot(merged, "abc123") {
		t.Fatal("刷新后应建立完整干员快照")
	}
}

func TestNormalizeSellProductCacheMergesLocationsOnUIDCollision(t *testing.T) {
	cache := normalizeSellProductCache(sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"../uid": {
				UpdatedAt: "2026-07-20T03:00:00Z",
				Locations: map[string]bool{"A": true},
			},
			".._uid": {
				UpdatedAt: "2026-07-20T03:01:00Z",
				Locations: map[string]bool{"A": false, "B": true},
			},
		},
	})
	account := cache.Accounts[".._uid"]
	want := map[string]bool{"A": false, "B": true}
	if !reflect.DeepEqual(account.Locations, want) {
		t.Fatalf("UID 碰撞合并结果 = %#v，期望 %#v", account.Locations, want)
	}
	if account.UpdatedAt != "2026-07-20T03:01:00Z" {
		t.Fatalf("UID 碰撞合并更新时间 = %q", account.UpdatedAt)
	}
}
