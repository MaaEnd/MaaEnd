package creditshopping

import (
	"path/filepath"
	"testing"
)

func TestShelfSnapshotKeepsFirstRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), shelfSnapshotFileName)
	first := snapshotEntry{
		UID:          "uid",
		GameDate:     "2026-10-01",
		RefreshIndex: 1,
		RefreshCost:  80,
		UTCTime:      "2026-10-01T00:00:00Z",
		Slots: []SlotRecord{
			{Slot: 0, Name: "折金票", ID: "TCreds", Discount: "75"},
		},
	}
	n, err := upsertShelfSnapshots(path, []snapshotEntry{first})
	if err != nil {
		t.Fatalf("append first: %v", err)
	}
	if n != 1 {
		t.Fatalf("upserted = %d, want 1", n)
	}

	exists, err := shelfSnapshotExists(path, first.UID, first.GameDate, first.RefreshIndex)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("expected existing snapshot")
	}

	second := first
	second.UTCTime = "2026-10-01T00:10:00Z"
	second.Slots = []SlotRecord{
		{Slot: 0, Name: "协议棱柱", ID: "Protoprism", Discount: "50"},
	}
	n, err = upsertShelfSnapshots(path, []snapshotEntry{second})
	if err != nil {
		t.Fatalf("keep first: %v", err)
	}
	if n != 0 {
		t.Fatalf("upserted = %d, want 0", n)
	}

	storage, err := readSnapshotFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(storage.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(storage.Records))
	}
	got := storage.Records[0]
	if got.UTCTime != first.UTCTime || len(got.Slots) != 1 || got.Slots[0].ID != "TCreds" {
		t.Fatalf("snapshot replaced: %+v", got)
	}

	other := first
	other.RefreshIndex = 2
	other.RefreshCost = 120
	exists, err = shelfSnapshotExists(path, other.UID, other.GameDate, other.RefreshIndex)
	if err != nil {
		t.Fatalf("other exists: %v", err)
	}
	if exists {
		t.Fatal("different refresh index should be absent")
	}
}
