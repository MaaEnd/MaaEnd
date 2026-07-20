package sellproduct

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const (
	testCacheUID                  = "0123456789abcdef"
	testOtherCacheUID             = "fedcba9876543210"
	testOperatorSnapshotUpdatedAt = "2026-07-20T03:00:00Z"
)

func testOperatorSnapshot(ids ...string) *sellProductOperatorSnapshot {
	return testOperatorSnapshotAt(testOperatorSnapshotUpdatedAt, ids...)
}

func testOperatorSnapshotAt(updatedAt string, ids ...string) *sellProductOperatorSnapshot {
	if ids == nil {
		ids = []string{}
	}
	parsed, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		panic(err)
	}
	return &sellProductOperatorSnapshot{
		UpdatedAt: parsed,
		IDs:       ids,
	}
}

func TestSellProductCacheReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := testCacheUID

	updatedAt := now.Format(time.RFC3339)
	if err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid: {
				Operators: testOperatorSnapshotAt(updatedAt, "Perlica", "ChenQianyu", "Perlica"),
			},
		},
	}); err != nil {
		t.Fatalf("writeSellProductCache: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read serialized cache: %v", err)
	}
	wantRaw := `{
    "accounts": {
        "0123456789abcdef": {
            "operators": {
                "updated_at": "2026-06-14T01:02:03Z",
                "ids": [
                    "ChenQianyu",
                    "Perlica"
                ]
            }
        }
    }
}
`
	if string(raw) != wantRaw {
		t.Fatalf("serialized cache = %s, want %s", raw, wantRaw)
	}
	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("readSellProductCache: %v", err)
	}
	account := cache.Accounts[uid]
	if got := cachedOperatorUpdatedAtForUID(cache, uid); got.Format(time.RFC3339) != updatedAt {
		t.Fatalf("cachedOperatorUpdatedAtForUID = %q, want %q", got, updatedAt)
	}
	want := []string{"ChenQianyu", "Perlica"}
	if !reflect.DeepEqual(account.Operators.IDs, want) {
		t.Fatalf("operators = %#v, want %#v", account.Operators.IDs, want)
	}
}

func TestDefaultSellProductCachePathIsSingleFile(t *testing.T) {
	want := filepath.Join("debug", "record", sellProductCacheFileName)
	if got := defaultSellProductCachePath(); got != want {
		t.Fatalf("defaultSellProductCachePath() = %q, want %q", got, want)
	}
}

func TestIsValidSellProductCacheUID(t *testing.T) {
	tests := map[string]bool{
		"unknown":          true,
		"0123456789abcdef": true,
		"0123456789ABCDEF": false,
		"abc123":           false,
		" account-cache ":  false,
		"../account-cache": false,
		"":                 false,
	}
	for uid, expected := range tests {
		if got := isValidSellProductCacheUID(uid); got != expected {
			t.Fatalf("isValidSellProductCacheUID(%q) = %v，期望 %v", uid, got, expected)
		}
	}
}

func TestSellProductCacheIgnoresLegacyFileName(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "SellProductOwnedOperators.json")
	newPath := filepath.Join(dir, sellProductCacheFileName)
	if err := writeSellProductCache(legacyPath, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			testCacheUID: {
				Operators: testOperatorSnapshot("Wulfgard"),
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

func TestSellProductCacheDiscardsAccountWithInvalidIDs(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name:    "中文干员名",
			content: `{"accounts":{"0123456789abcdef":{"operators":{"updated_at":"2026-07-20T03:00:00Z","ids":["狼卫"]},"locations":{"RefugeeCamp":true}}}}`,
		},
		{
			name:    "中文据点名",
			content: `{"accounts":{"0123456789abcdef":{"operators":{"updated_at":"2026-07-20T03:00:00Z","ids":["Wulfgard"]},"locations":{"难民暂居处":true}}}}`,
		},
		{
			name:    "未知干员 ID",
			content: `{"accounts":{"0123456789abcdef":{"operators":{"updated_at":"2026-07-20T03:00:00Z","ids":["UnknownOperator"]},"locations":{"RefugeeCamp":true}}}}`,
		},
		{
			name:    "未知据点 ID",
			content: `{"accounts":{"0123456789abcdef":{"operators":{"updated_at":"2026-07-20T03:00:00Z","ids":["Wulfgard"]},"locations":{"UnknownLocation":true}}}}`,
		},
		{
			name:    "非精确 ID",
			content: `{"accounts":{"0123456789abcdef":{"operators":{"updated_at":"2026-07-20T03:00:00Z","ids":["Wulfgard"]},"locations":{" RefugeeCamp ":true}}}}`,
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
				t.Fatalf("包含无效 ID 的账号应视为不存在：%#v", cache.Accounts)
			}
		})
	}
}

func TestSellProductCachePreservesValidAccountsWhenOneAccountIsInvalid(t *testing.T) {
	tests := []struct {
		name         string
		invalidEntry string
	}{
		{
			name:         "非规范 UID",
			invalidEntry: `"not-a-hash":{"locations":{"RefugeeCamp":true}}`,
		},
		{
			name:         "账号对象类型错误",
			invalidEntry: `"fedcba9876543210":[]`,
		},
		{
			name:         "账号包含未知字段",
			invalidEntry: `"fedcba9876543210":{"unexpected":true}`,
		},
		{
			name:         "干员快照时间无效",
			invalidEntry: `"fedcba9876543210":{"operators":{"updated_at":"invalid","ids":["Wulfgard"]}}`,
		},
		{
			name:         "干员 ID 无效",
			invalidEntry: `"fedcba9876543210":{"operators":{"updated_at":"2026-07-20T03:00:00Z","ids":["UnknownOperator"]}}`,
		},
		{
			name:         "据点 ID 无效",
			invalidEntry: `"fedcba9876543210":{"locations":{"UnknownLocation":true}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), sellProductCacheFileName)
			content := `{"accounts":{"0123456789abcdef":{"operators":{"updated_at":"2026-07-20T03:00:00Z","ids":["Wulfgard"]},"locations":{"RefugeeCamp":true}},` + test.invalidEntry + `}}`
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}

			cache, err := readSellProductCache(path)
			if err != nil {
				t.Fatalf("读取混合账号缓存失败：%v", err)
			}
			if len(cache.Accounts) != 1 {
				t.Fatalf("有效账号数量 = %d，期望 1：%#v", len(cache.Accounts), cache.Accounts)
			}
			account, ok := cache.Accounts[testCacheUID]
			if !ok {
				t.Fatalf("有效账号不应被无效账号连带丢弃：%#v", cache.Accounts)
			}
			if want := []string{"Wulfgard"}; !reflect.DeepEqual(account.Operators.IDs, want) {
				t.Fatalf("有效账号干员 = %#v，期望 %#v", account.Operators.IDs, want)
			}
			if !account.Locations["RefugeeCamp"] {
				t.Fatal("有效账号据点状态不应被无效账号连带丢弃")
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

func TestSellProductCacheDiscardsWholeFileWithInvalidTopLevelStructure(t *testing.T) {
	contents := []string{
		`{"accounts":{"0123456789abcdef":{"locations":{"RefugeeCamp":true}}},"unexpected":true}`,
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

func TestWriteSellProductCacheRejectsInvalidStructure(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"not-a-hash": {Operators: testOperatorSnapshot("Wulfgard")},
		},
	})
	if err == nil {
		t.Fatal("写入无效缓存结构应失败")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("无效缓存不应落盘：%v", statErr)
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
	owned := operatorIDSet([]string{"Exp", "Both"})
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
	uid := testCacheUID
	if sellProductCacheHasOperatorSnapshot(sellProductCache{}, uid) {
		t.Fatal("empty cache should not be treated as a snapshot")
	}
	if !sellProductCacheHasOperatorSnapshot(sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid: {Operators: testOperatorSnapshot("Perlica")},
		},
	}, uid) {
		t.Fatal("account cache should be treated as a snapshot")
	}
	if sellProductCacheHasOperatorSnapshot(sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			testOtherCacheUID: {Operators: testOperatorSnapshot("Perlica")},
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
			uid: {Operators: testOperatorSnapshot()},
		},
	}, uid) {
		t.Fatal("an explicitly empty operator snapshot should still be treated as complete")
	}
}

func TestSellProductCachePersistsOperatorSnapshotPresence(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	if err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"1111111111111111": {
				Locations: map[string]bool{"RefugeeCamp": true},
			},
			"2222222222222222": {
				Operators: testOperatorSnapshot(),
			},
		},
	}); err != nil {
		t.Fatalf("写入缓存失败：%v", err)
	}
	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("读取缓存失败：%v", err)
	}
	if sellProductCacheHasOperatorSnapshot(cache, "1111111111111111") {
		t.Fatal("operators: null 落盘后不应变成完整快照")
	}
	if !sellProductCacheHasOperatorSnapshot(cache, "2222222222222222") {
		t.Fatal("operators: [] 落盘后应保持完整空快照语义")
	}
}

func TestMergeOperatorSnapshotReplacesCurrentAccount(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := testCacheUID
	cache := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			uid:               {Operators: testOperatorSnapshot("缓存甲", "缓存乙")},
			testOtherCacheUID: {Operators: testOperatorSnapshot("其他账号干员")},
		},
	}
	got := mergeOperatorSnapshot(
		cache,
		uid,
		[]operatorCandidate{{Name: "CandidateA"}, {Name: "CandidateB"}},
		[]string{"CandidateB"},
		now,
	)
	if got.Accounts[uid].Operators.UpdatedAt.Format(time.RFC3339) != "2026-06-14T01:02:03Z" {
		t.Fatalf("operator updated_at = %q", got.Accounts[uid].Operators.UpdatedAt)
	}
	if want := []string{"CandidateB"}; !reflect.DeepEqual(got.Accounts[uid].Operators.IDs, want) {
		t.Fatalf("operators = %#v, want %#v", got.Accounts[uid].Operators.IDs, want)
	}
	if want := []string{"其他账号干员"}; !reflect.DeepEqual(got.Accounts[testOtherCacheUID].Operators.IDs, want) {
		t.Fatalf("other account operators = %#v, want %#v", got.Accounts[testOtherCacheUID].Operators.IDs, want)
	}
}

func TestSellProductCacheReadWriteLocationsBesideOperators(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	if err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			testCacheUID: {
				Operators: testOperatorSnapshot("Wulfgard"),
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
	if !sellProductCacheHasOperatorSnapshot(cache, testCacheUID) {
		t.Fatal("写入据点发展值状态后不应破坏完整干员快照")
	}
	want := map[string]bool{"RefugeeCamp": true, "ReconstructionHQ": false}
	if got := outpostProsperityStatusesForUID(cache, testCacheUID); !reflect.DeepEqual(got, want) {
		t.Fatalf("据点发展值缓存 = %#v，期望 %#v", got, want)
	}
}

func TestLocationsOnlyAccountIsNotAnOperatorSnapshot(t *testing.T) {
	cache := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			testCacheUID: {Locations: map[string]bool{"RefugeeCamp": true}},
		},
	}
	if sellProductCacheHasOperatorSnapshot(cache, testCacheUID) {
		t.Fatal("只有 locations 时不应被误判为完整干员快照")
	}
}

func TestUpdateLocationsPreservesOperatorSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), sellProductCacheFileName)
	if err := writeSellProductCache(path, sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			"aaaaaaaaaaaaaaaa": {Operators: testOperatorSnapshot("Wulfgard")},
			"bbbbbbbbbbbbbbbb": {Operators: testOperatorSnapshot("Akekuri")},
		},
	}); err != nil {
		t.Fatalf("准备干员缓存失败：%v", err)
	}

	changed, err := updateCachedOutpostProsperity(path, "aaaaaaaaaaaaaaaa", "RefugeeCamp", true)
	if err != nil || !changed {
		t.Fatalf("首次更新结果 = %v, %v，期望写入缓存", changed, err)
	}
	changed, err = updateCachedOutpostProsperity(path, "bbbbbbbbbbbbbbbb", "ReconstructionHQ", false)
	if err != nil || !changed {
		t.Fatalf("第二账号更新结果 = %v, %v，期望写入缓存", changed, err)
	}
	changed, err = updateCachedOutpostProsperity(path, "aaaaaaaaaaaaaaaa", "RefugeeCamp", true)
	if err != nil || changed {
		t.Fatalf("相同状态更新结果 = %v, %v，期望跳过写盘", changed, err)
	}

	cache, err := readSellProductCache(path)
	if err != nil {
		t.Fatalf("读取更新后的缓存失败：%v", err)
	}
	if want := []string{"Wulfgard"}; !reflect.DeepEqual(cache.Accounts["aaaaaaaaaaaaaaaa"].Operators.IDs, want) {
		t.Fatalf("account-a 干员快照 = %#v，期望 %#v", cache.Accounts["aaaaaaaaaaaaaaaa"].Operators.IDs, want)
	}
	if want := []string{"Akekuri"}; !reflect.DeepEqual(cache.Accounts["bbbbbbbbbbbbbbbb"].Operators.IDs, want) {
		t.Fatalf("account-b 干员快照 = %#v，期望 %#v", cache.Accounts["bbbbbbbbbbbbbbbb"].Operators.IDs, want)
	}
	if got := cache.Accounts["aaaaaaaaaaaaaaaa"].Locations["RefugeeCamp"]; !got {
		t.Fatal("account-a 的满级状态丢失")
	}
	if got, ok := cache.Accounts["bbbbbbbbbbbbbbbb"].Locations["ReconstructionHQ"]; !ok || got {
		t.Fatalf("account-b 的未满状态 = %v, %v，期望明确保存 false", got, ok)
	}
	if got := cache.Accounts["aaaaaaaaaaaaaaaa"].Operators.UpdatedAt.Format(time.RFC3339); got != testOperatorSnapshotUpdatedAt {
		t.Fatalf("据点状态更新不应改变干员快照时间，实际为 %q", got)
	}
}

func TestMergeOperatorCachePreservesLocations(t *testing.T) {
	cache := sellProductCache{
		Accounts: map[string]sellProductCacheAccount{
			testCacheUID: {Locations: map[string]bool{"RefugeeCamp": true, "ReconstructionHQ": false}},
		},
	}
	merged := mergeOperatorSnapshot(
		cache,
		testCacheUID,
		[]operatorCandidate{{Name: "Wulfgard"}},
		[]string{"Wulfgard"},
		time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC),
	)
	want := map[string]bool{"RefugeeCamp": true, "ReconstructionHQ": false}
	if got := outpostProsperityStatusesForUID(merged, testCacheUID); !reflect.DeepEqual(got, want) {
		t.Fatalf("刷新干员快照后的据点发展值状态 = %#v，期望 %#v", got, want)
	}
	if !sellProductCacheHasOperatorSnapshot(merged, testCacheUID) {
		t.Fatal("刷新后应建立完整干员快照")
	}
}
