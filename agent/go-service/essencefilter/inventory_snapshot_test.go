package essencefilter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
)

func withTempInventorySnapshot(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "EssenceFilterInventory.json")
	oldPath := resolveInventorySnapshotPathFunc
	oldNow := inventorySnapshotNowFunc
	resolveInventorySnapshotPathFunc = func() string { return path }
	inventorySnapshotNowFunc = func() time.Time {
		return time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC)
	}
	t.Cleanup(func() {
		resolveInventorySnapshotPathFunc = oldPath
		inventorySnapshotNowFunc = oldNow
	})
	return path
}

func mustReadInventorySnapshot(t *testing.T, path string) inventorySnapshotFile {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var storage inventorySnapshotFile
	if err := json.Unmarshal(raw, &storage); err != nil {
		t.Fatal(err)
	}
	return storage
}

func TestInventorySnapshotClearsOnInitAndSavesOnFinish(t *testing.T) {
	path := withTempInventorySnapshot(t)
	st := &RunState{
		PersistInventorySnapshot: true,
		MatchedCombinationSummary: map[string]*matchapi.SkillCombinationSummary{
			"2-2-6": {
				SkillIDs:      []int{2, 2, 6},
				SkillsChinese: []string{"力量", "攻击", "强攻"},
				OCRSkills:     []string{"力量", "攻击", "强攻"},
				Count:         2,
				Weapons:       []matchapi.WeaponData{{InternalID: "wpn_claym_0003", ChineseName: "测试武器", Rarity: 6}},
			},
		},
	}

	resetInventorySnapshot()
	cleared := mustReadInventorySnapshot(t, path)
	if cleared.Complete || len(cleared.Records) != 0 {
		t.Fatalf("after clear complete=%v records=%d", cleared.Complete, len(cleared.Records))
	}

	saveInventorySnapshot(st)
	saved := mustReadInventorySnapshot(t, path)
	if !saved.Complete {
		t.Fatal("finish snapshot should be complete")
	}
	if saved.UpdatedAt != "2026-09-04T13:00:00Z" {
		t.Fatalf("updated_at=%s", saved.UpdatedAt)
	}
	if len(saved.Records) != 1 || saved.Records[0].Count != 2 {
		t.Fatalf("records=%v", saved.Records)
	}
	if saved.Records[0].OCRSkills[2] != "强攻" || saved.Records[0].Weapons[0].ID != "wpn_claym_0003" || saved.Records[0].Weapons[0].Rarity != 6 {
		t.Fatalf("row=%v", saved.Records[0])
	}

	resetInventorySnapshot()
	again := mustReadInventorySnapshot(t, path)
	if again.Complete || len(again.Records) != 0 {
		t.Fatalf("second init should clear previous finish data: %+v", again)
	}
}

func TestInventorySnapshotFinishEmptyIsComplete(t *testing.T) {
	path := withTempInventorySnapshot(t)
	resetInventorySnapshot()
	saveInventorySnapshot(&RunState{MatchedCombinationSummary: map[string]*matchapi.SkillCombinationSummary{}})
	got := mustReadInventorySnapshot(t, path)
	if !got.Complete {
		t.Fatal("empty finish must still be complete")
	}
	if got.Records == nil || len(got.Records) != 0 {
		t.Fatalf("records=%v", got.Records)
	}
}
