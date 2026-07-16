package sellproduct

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOperatorCacheReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SellProductOwnedOperators.json")
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"

	if err := writeOperatorCache(
		path,
		uid,
		[]string{"佩丽卡", "陈千语", "佩丽卡", ""},
		now,
	); err != nil {
		t.Fatalf("writeOperatorCache: %v", err)
	}
	cache, err := readOperatorCache(path)
	if err != nil {
		t.Fatalf("readOperatorCache: %v", err)
	}
	if cache.SchemaVersion != operatorCacheSchemaVersion {
		t.Fatalf("schema version = %d, want %d", cache.SchemaVersion, operatorCacheSchemaVersion)
	}
	if cache.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("updated_at = %q", cache.UpdatedAt)
	}
	account := cache.Accounts[uid]
	if account.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("account updated_at = %q", account.UpdatedAt)
	}
	if !account.Complete {
		t.Fatal("writeOperatorCache should create a complete snapshot")
	}
	want := []string{"佩丽卡", "陈千语"}
	if !reflect.DeepEqual(account.Operators, want) {
		t.Fatalf("operators = %#v, want %#v", account.Operators, want)
	}
}

func TestDefaultOperatorCachePathIsSingleFile(t *testing.T) {
	tests := []struct {
		name string
		uid  string
		want string
	}{
		{
			name: "hashed uid",
			uid:  "abc123",
			want: filepath.Join("debug", "record", "SellProductOwnedOperators.json"),
		},
		{
			name: "empty uid",
			uid:  "",
			want: filepath.Join("debug", "record", "SellProductOwnedOperators.json"),
		},
		{
			name: "unsafe uid",
			uid:  "../uid value",
			want: filepath.Join("debug", "record", "SellProductOwnedOperators.json"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultOperatorCachePath(tt.uid); got != tt.want {
				t.Fatalf("defaultOperatorCachePath(%q) = %q, want %q", tt.uid, got, tt.want)
			}
		})
	}
}

func TestOperatorCacheMissingAndEmpty(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.json")
	cache, err := readOperatorCache(missing)
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
	cache, err = readOperatorCache(empty)
	if err != nil {
		t.Fatalf("empty cache should not error: %v", err)
	}
	if len(cache.Accounts) != 0 {
		t.Fatalf("empty cache accounts = %#v", cache.Accounts)
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

func TestOperatorCacheHasSnapshot(t *testing.T) {
	uid := "abc123"
	if operatorCacheHasSnapshot(operatorCacheFile{}, uid) {
		t.Fatal("empty cache should not be treated as a snapshot")
	}
	if !operatorCacheHasSnapshot(operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		Accounts: map[string]operatorCacheAccount{
			uid: {Operators: []string{"佩丽卡"}, Complete: true},
		},
	}, uid) {
		t.Fatal("versioned cache should be treated as a snapshot")
	}
	if operatorCacheHasSnapshot(operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		Accounts: map[string]operatorCacheAccount{
			"other": {Operators: []string{"佩丽卡"}},
		},
	}, uid) {
		t.Fatal("cache without this uid should not be treated as a snapshot")
	}
	if operatorCacheHasSnapshot(operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		Accounts: map[string]operatorCacheAccount{
			uid: {Operators: []string{"佩丽卡"}},
		},
	}, uid) {
		t.Fatal("partial observations must not be treated as a complete snapshot")
	}
}

func TestNormalizeOperatorCacheMigratesV1Snapshot(t *testing.T) {
	uid := "abc123"
	cache := normalizeOperatorCacheFile(operatorCacheFile{
		SchemaVersion: 1,
		Accounts: map[string]operatorCacheAccount{
			uid: {UpdatedAt: "2026-06-14T01:02:03Z", Operators: []string{"佩丽卡"}},
		},
	})
	if cache.SchemaVersion != operatorCacheSchemaVersion {
		t.Fatalf("schema version = %d, want %d", cache.SchemaVersion, operatorCacheSchemaVersion)
	}
	if !cache.Accounts[uid].Complete {
		t.Fatal("v1 snapshot should remain authoritative after migration")
	}
}

func TestMergeOperatorCacheUpdatesOnlyCurrentAccount(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"
	cache := operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		Accounts: map[string]operatorCacheAccount{
			uid:     {Operators: []string{"旧干员", "保留干员"}},
			"other": {Operators: []string{"其他账号干员"}},
		},
	}
	got := mergeOperatorCache(
		cache,
		uid,
		[]operatorCandidate{{Name: "Old", CacheName: "旧干员"}, {Name: "New", CacheName: "新干员"}},
		[]string{"新干员"},
		now,
	)
	if got.SchemaVersion != operatorCacheSchemaVersion {
		t.Fatalf("schema version = %d, want %d", got.SchemaVersion, operatorCacheSchemaVersion)
	}
	if got.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("updated_at = %q", got.UpdatedAt)
	}
	if want := []string{"保留干员", "新干员"}; !reflect.DeepEqual(got.Accounts[uid].Operators, want) {
		t.Fatalf("operators = %#v, want %#v", got.Accounts[uid].Operators, want)
	}
	if !got.Accounts[uid].Complete {
		t.Fatal("full scan merge should mark the account complete")
	}
	if want := []string{"其他账号干员"}; !reflect.DeepEqual(got.Accounts["other"].Operators, want) {
		t.Fatalf("other account operators = %#v, want %#v", got.Accounts["other"].Operators, want)
	}
}

func TestMergeObservedOperatorCacheOnlyAddsObservedOperators(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"
	cache := operatorCacheFile{
		SchemaVersion: operatorCacheSchemaVersion,
		Accounts: map[string]operatorCacheAccount{
			uid:     {Operators: []string{"保留干员"}},
			"other": {Operators: []string{"其他账号干员"}},
		},
	}
	got := mergeObservedOperatorCache(cache, uid, []string{"新干员", "保留干员", ""}, now)
	if got.SchemaVersion != operatorCacheSchemaVersion {
		t.Fatalf("schema version = %d, want %d", got.SchemaVersion, operatorCacheSchemaVersion)
	}
	if got.UpdatedAt != "2026-06-14T01:02:03Z" {
		t.Fatalf("updated_at = %q", got.UpdatedAt)
	}
	if want := []string{"保留干员", "新干员"}; !reflect.DeepEqual(got.Accounts[uid].Operators, want) {
		t.Fatalf("operators = %#v, want %#v", got.Accounts[uid].Operators, want)
	}
	if got.Accounts[uid].Complete {
		t.Fatal("partial observation must not create a complete snapshot")
	}
	if want := []string{"其他账号干员"}; !reflect.DeepEqual(got.Accounts["other"].Operators, want) {
		t.Fatalf("other account operators = %#v, want %#v", got.Accounts["other"].Operators, want)
	}
}
