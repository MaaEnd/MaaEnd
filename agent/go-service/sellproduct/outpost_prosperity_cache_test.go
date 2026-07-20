package sellproduct

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOperatorCacheReadWriteOutpostProsperity(t *testing.T) {
	path := filepath.Join(t.TempDir(), operatorCacheFilePrefix+operatorCacheFileExt)
	updatedAt := "2026-07-20T03:00:00Z"
	if err := writeOperatorCacheFile(path, operatorCacheFile{
		UpdatedAt: updatedAt,
		Accounts: map[string]operatorCacheAccount{
			"abc123": {UpdatedAt: updatedAt, Operators: []string{"狼卫"}},
		},
		OutpostProsperity: map[string]outpostProsperityCacheAccount{
			"abc123": {
				UpdatedAt: updatedAt,
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

	cache, err := readOperatorCache(path)
	if err != nil {
		t.Fatalf("读取统一缓存失败：%v", err)
	}
	if !operatorCacheHasSnapshot(cache, "abc123") {
		t.Fatal("写入据点发展值状态后不应破坏完整干员快照")
	}
	want := map[string]bool{"Full": true, "Open": false}
	if got := outpostProsperityStatusesForUID(cache, "abc123"); !reflect.DeepEqual(got, want) {
		t.Fatalf("据点发展值缓存 = %#v，期望 %#v", got, want)
	}
	maxLocations := outpostProsperityMaxLocationsForUID(cache, "abc123")
	if len(maxLocations) != 1 {
		t.Fatalf("满级据点 = %#v，期望仅包含 Full", maxLocations)
	}
	if _, ok := maxLocations["Full"]; !ok {
		t.Fatal("满级据点缓存中缺少 Full")
	}
}

func TestOutpostProsperityOnlyCacheIsNotAnOperatorSnapshot(t *testing.T) {
	cache := operatorCacheFile{
		OutpostProsperity: map[string]outpostProsperityCacheAccount{
			"abc123": {Locations: map[string]bool{"Full": true}},
		},
	}
	if operatorCacheHasSnapshot(cache, "abc123") {
		t.Fatal("只有据点发展值状态时不应被误判为完整干员快照")
	}
}

func TestUpdateOutpostProsperityCachePreservesOperatorSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), operatorCacheFilePrefix+operatorCacheFileExt)
	first := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	if err := writeOperatorCacheFile(path, operatorCacheFile{
		Accounts: map[string]operatorCacheAccount{
			"account-a": {Operators: []string{"狼卫"}},
			"account-b": {Operators: []string{"秋栗"}},
		},
	}); err != nil {
		t.Fatalf("准备干员缓存失败：%v", err)
	}

	changed, err := updateOutpostProsperityCache(path, "account-a", "Full", true, first)
	if err != nil || !changed {
		t.Fatalf("首次更新结果 = %v, %v，期望写入缓存", changed, err)
	}
	changed, err = updateOutpostProsperityCache(path, "account-b", "Open", false, first.Add(time.Minute))
	if err != nil || !changed {
		t.Fatalf("第二账号更新结果 = %v, %v，期望写入缓存", changed, err)
	}
	changed, err = updateOutpostProsperityCache(path, "account-a", "Full", true, first.Add(2*time.Minute))
	if err != nil || changed {
		t.Fatalf("相同状态更新结果 = %v, %v，期望跳过写盘", changed, err)
	}

	cache, err := readOperatorCache(path)
	if err != nil {
		t.Fatalf("读取更新后的缓存失败：%v", err)
	}
	if want := []string{"狼卫"}; !reflect.DeepEqual(cache.Accounts["account-a"].Operators, want) {
		t.Fatalf("account-a 干员快照 = %#v，期望 %#v", cache.Accounts["account-a"].Operators, want)
	}
	if want := []string{"秋栗"}; !reflect.DeepEqual(cache.Accounts["account-b"].Operators, want) {
		t.Fatalf("account-b 干员快照 = %#v，期望 %#v", cache.Accounts["account-b"].Operators, want)
	}
	if got := cache.OutpostProsperity["account-a"].Locations["Full"]; !got {
		t.Fatal("account-a 的满级状态丢失")
	}
	if got, ok := cache.OutpostProsperity["account-b"].Locations["Open"]; !ok || got {
		t.Fatalf("account-b 的未满状态 = %v, %v，期望明确保存 false", got, ok)
	}
	if got := cache.OutpostProsperity["account-a"].UpdatedAt; got != first.Format(time.RFC3339) {
		t.Fatalf("跳过相同状态后 account-a 更新时间 = %q", got)
	}
}

func TestMergeOperatorCachePreservesOutpostProsperity(t *testing.T) {
	cache := operatorCacheFile{
		OutpostProsperity: map[string]outpostProsperityCacheAccount{
			"abc123": {Locations: map[string]bool{"Full": true, "Open": false}},
		},
	}
	merged := mergeOperatorCache(
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
}

func TestNormalizeOutpostProsperityAccountsMergesNormalizedUIDCollision(t *testing.T) {
	accounts := normalizeOutpostProsperityAccounts(map[string]outpostProsperityCacheAccount{
		"../uid": {
			UpdatedAt: "2026-07-20T03:00:00Z",
			Locations: map[string]bool{"A": true},
		},
		".._uid": {
			UpdatedAt: "2026-07-20T03:01:00Z",
			Locations: map[string]bool{"A": false, "B": true},
		},
	})
	account := accounts[".._uid"]
	want := map[string]bool{"A": false, "B": true}
	if !reflect.DeepEqual(account.Locations, want) {
		t.Fatalf("UID 碰撞合并结果 = %#v，期望 %#v", account.Locations, want)
	}
	if account.UpdatedAt != "2026-07-20T03:01:00Z" {
		t.Fatalf("UID 碰撞合并更新时间 = %q", account.UpdatedAt)
	}
}
