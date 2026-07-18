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

	updatedAt := now.Format(time.RFC3339)
	if err := writeOperatorCacheFile(path, operatorCacheFile{
		UpdatedAt: updatedAt,
		Accounts: map[string]operatorCacheAccount{
			uid: {
				UpdatedAt: updatedAt,
				Operators: []string{"佩丽卡", "陈千语", "佩丽卡", ""},
			},
		},
	}); err != nil {
		t.Fatalf("writeOperatorCacheFile: %v", err)
	}
	cache, err := readOperatorCache(path)
	if err != nil {
		t.Fatalf("readOperatorCache: %v", err)
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

func TestOperatorCacheRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "SellProductOwnedOperators.json")
	if err := os.WriteFile(path, []byte(`{"updated_at":"","accounts":{},"unexpected":true}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readOperatorCache(path); err == nil {
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

func TestOperatorCacheHasSnapshot(t *testing.T) {
	uid := "abc123"
	if operatorCacheHasSnapshot(operatorCacheFile{}, uid) {
		t.Fatal("empty cache should not be treated as a snapshot")
	}
	if !operatorCacheHasSnapshot(operatorCacheFile{
		Accounts: map[string]operatorCacheAccount{
			uid: {Operators: []string{"佩丽卡"}},
		},
	}, uid) {
		t.Fatal("account cache should be treated as a snapshot")
	}
	if operatorCacheHasSnapshot(operatorCacheFile{
		Accounts: map[string]operatorCacheAccount{
			"other": {Operators: []string{"佩丽卡"}},
		},
	}, uid) {
		t.Fatal("cache without this uid should not be treated as a snapshot")
	}
	if !operatorCacheHasSnapshot(operatorCacheFile{
		Accounts: map[string]operatorCacheAccount{
			uid: {Operators: nil},
		},
	}, uid) {
		t.Fatal("an empty account snapshot should still be treated as complete")
	}
}

func TestMergeOperatorCacheReplacesCurrentAccount(t *testing.T) {
	now := time.Date(2026, 6, 14, 1, 2, 3, 0, time.UTC)
	uid := "abc123"
	cache := operatorCacheFile{
		Accounts: map[string]operatorCacheAccount{
			uid:     {Operators: []string{"缓存甲", "缓存乙"}},
			"other": {Operators: []string{"其他账号干员"}},
		},
	}
	got := mergeOperatorCache(
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
