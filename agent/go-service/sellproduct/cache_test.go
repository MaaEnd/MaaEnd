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
				Operators: []string{"Perlica", "ChenQianyu", "Perlica"},
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
	if got := cachedUpdatedAtForUID(cache, uid); got != updatedAt {
		t.Fatalf("cachedUpdatedAtForUID = %q, want %q", got, updatedAt)
	}
	want := []string{"ChenQianyu", "Perlica"}
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

func TestSellProductCacheIgnoresLegacyFileName(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "SellProductOwnedOperators.json")
	newPath := filepath.Join(dir, sellProductCacheFileName)
	if err := writeSellProductCache(legacyPath, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"abc123": {
				Operators: []string{"Wulfgard"},
				Locations: map[string]bool{"RefugeeCamp": true},
			},
		},
	}); err != nil {
		t.Fatalf("准备旧版缓存失败：%v", err)
	}

	cache, err := readSellProductCache(newPath)
	if err != nil {
		t.Fatalf("读取新缓存失败：%v", err)
	}
	if len(cache.Accounts) != 0 {
		t.Fatalf("旧文件名不应被读取：%#v", cache.Accounts)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("旧缓存文件不应被迁移或删除：%v", err)
	}
}

func TestSellProductCacheDiscardsInvalidIDs(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "中文干员名",
			content: `{"accounts":{"abc123":{"operators":["狼卫"],"locations":{"RefugeeCamp":true}}}}`,
		},
		{
			name:    "中文据点名",
			content: `{"accounts":{"abc123":{"operators":["Wulfgard"],"locations":{"难民暂居处":true}}}}`,
		},
		{
			name:    "未知干员 ID",
			content: `{"accounts":{"abc123":{"operators":["UnknownOperator"],"locations":{"RefugeeCamp":true}}}}`,
		},
		{
			name:    "未知据点 ID",
			content: `{"accounts":{"abc123":{"operators":["Wulfgard"],"locations":{"UnknownLocation":true}}}}`,
		},
		{
			name:    "非精确 ID",
			content: `{"accounts":{"abc123":{"operators":["Wulfgard"],"locations":{" RefugeeCamp ":true}}}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), sellProductCacheFileName)
			if err := os.WriteFile(path, []byte(test.content), 0644); err != nil {
				t.Fatal(err)
			}
			cache, err := readSellProductCache(path)
			if err != nil {
				t.Fatalf("读取无效 ID 缓存失败：%v", err)
			}
			if len(cache.Accounts) != 0 {
				t.Fatalf("无效 ID 缓存应视为不存在：%#v", cache.Accounts)
			}
		})
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

func TestSellProductCacheDiscardsInvalidStructure(t *testing.T) {
	contents := []string{
		`{"updated_at":"","accounts":{},"unexpected":true}`,
		`{"accounts":[]}`,
		`{"accounts":{`,
		`{"accounts":{}} {}`,
	}
	for _, content := range contents {
		path := filepath.Join(t.TempDir(), sellProductCacheFileName)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		cache, err := readSellProductCache(path)
		if err != nil {
			t.Fatalf("结构无效的缓存应直接视为不存在：%v", err)
		}
		if len(cache.Accounts) != 0 {
			t.Fatalf("结构无效的缓存不应保留数据：%#v", cache.Accounts)
		}
	}
}

func TestNormalizeOperatorCandidates(t *testing.T) {
	got := normalizeOperatorCandidates([]operatorCandidate{
		{Name: "Beta", Expected: []string{"贝塔"}, Priority: 2},
		{Name: "", Expected: []string{"忽略"}, Priority: 0},
		{Name: "Alpha", Expected: []string{"阿尔法", "阿尔法", ""}, Priority: 1},
		{Name: "Beta", Expected: []string{"重复"}, Priority: 0},
	})
	want := []operatorCandidate{
		{Name: "Alpha", Expected: []string{"阿尔法"}, Priority: 1},
		{Name: "Beta", Expected: []string{"贝塔"}, Priority: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeOperatorCandidates = %#v, want %#v", got, want)
	}
}

func TestFilterOwnedCandidatesUsesOperatorID(t *testing.T) {
	candidates := []operatorCandidate{
		{Name: "Both", Priority: 0},
		{Name: "Money", Priority: 1},
		{Name: "Exp", Priority: 2},
	}
	owned := operatorNameSet([]string{"Exp", "Both"})
	got := filterOwnedCandidates(candidates, owned)
	want := []operatorCandidate{
		{Name: "Both", Priority: 0},
		{Name: "Exp", Priority: 2},
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
			uid: {Operators: []string{"Perlica"}},
		},
	}, uid) {
		t.Fatal("account cache should be treated as a snapshot")
	}
	if sellProductCacheHasOperatorSnapshot(sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"other": {Operators: []string{"Perlica"}},
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
				Locations: map[string]bool{"RefugeeCamp": true},
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
		[]operatorCandidate{{Name: "CandidateA"}, {Name: "CandidateB"}},
		[]string{"CandidateB"},
		now,
	)
	if got.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("updated_at = %q", got.UpdatedAt)
	}
	if want := []string{"CandidateB"}; !reflect.DeepEqual(got.Accounts[uid].Operators, want) {
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
				Operators: []string{"Wulfgard"},
				Locations: map[string]bool{
					"RefugeeCamp":      true,
					"ReconstructionHQ": false,
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
	want := map[string]bool{"RefugeeCamp": true, "ReconstructionHQ": false}
	if got := outpostProsperityStatusesForUID(cache, "abc123"); !reflect.DeepEqual(got, want) {
		t.Fatalf("据点发展值缓存 = %#v，期望 %#v", got, want)
	}
}

func TestLocationsOnlyAccountIsNotAnOperatorSnapshot(t *testing.T) {
	cache := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"abc123": {Locations: map[string]bool{"RefugeeCamp": true}},
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
			"account-a": {Operators: []string{"Wulfgard"}},
			"account-b": {Operators: []string{"Akekuri"}},
		},
	}); err != nil {
		t.Fatalf("准备干员缓存失败：%v", err)
	}

	changed, err := updateCachedOutpostProsperity(path, "account-a", "RefugeeCamp", true, first)
	if err != nil || !changed {
		t.Fatalf("首次更新结果 = %v, %v，期望写入缓存", changed, err)
	}
	changed, err = updateCachedOutpostProsperity(path, "account-b", "ReconstructionHQ", false, first.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("第二账号更新结果 = %v, %v，期望写入缓存", changed, err)
	}
	changed, err = updateCachedOutpostProsperity(path, "account-a", "RefugeeCamp", true, first.Add(2*time.Minute))
	if err != nil || changed {
		t.Fatalf("相同状态更新结果 = %v, %v，期望跳过写盘", changed, err)
	}

	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("读取更新后的缓存失败：%v", err)
	}
	if want := []string{"Wulfgard"}; !reflect.DeepEqual(cache.Accounts["account-a"].Operators, want) {
		t.Fatalf("account-a 干员快照 = %#v，期望 %#v", cache.Accounts["account-a"].Operators, want)
	}
	if want := []string{"Akekuri"}; !reflect.DeepEqual(cache.Accounts["account-b"].Operators, want) {
		t.Fatalf("account-b 干员快照 = %#v，期望 %#v", cache.Accounts["account-b"].Operators, want)
	}
	if got := cache.Accounts["account-a"].Locations["RefugeeCamp"]; !got {
		t.Fatal("account-a 的满级状态丢失")
	}
	if got, ok := cache.Accounts["account-b"].Locations["ReconstructionHQ"]; !ok || got {
		t.Fatalf("account-b 的未满状态 = %v, %v，期望明确保存 false", got, ok)
	}
	if got := cache.Accounts["account-a"].UpdatedAt; got != first.Format(time.RFC3339) {
		t.Fatalf("跳过相同状态后 account-a 更新时间 = %q", got)
	}
}

func TestMergeOperatorCachePreservesLocations(t *testing.T) {
	cache := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"abc123": {Locations: map[string]bool{"RefugeeCamp": true, "ReconstructionHQ": false}},
		},
	}
	merged := mergeOperatorSnapshot(
		cache,
		"abc123",
		[]operatorCandidate{{Name: "Wulfgard"}},
		[]string{"Wulfgard"},
		time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC),
	)
	want := map[string]bool{"RefugeeCamp": true, "ReconstructionHQ": false}
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
