package creditshopping

import (
	"encoding/json"
	"testing"
)

func TestSlotRecord_UnmarshalJSON_legacyItemID(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"slot":3,"item_id":"Protohedron","discount":"-95%"}`)
	var slot SlotRecord
	if err := json.Unmarshal(raw, &slot); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if slot.Slot != 3 {
		t.Fatalf("slot = %d, want 3", slot.Slot)
	}
	if slot.ID != "Protohedron" {
		t.Fatalf("id = %q, want Protohedron", slot.ID)
	}
	if slot.Discount != "-95%" {
		t.Fatalf("discount = %q", slot.Discount)
	}
}

func TestSlotRecord_UnmarshalJSON_idPreferredOverItemID(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"slot":0,"id":"TCreds","item_id":"Protohedron","name":"折金票"}`)
	var slot SlotRecord
	if err := json.Unmarshal(raw, &slot); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if slot.ID != "TCreds" {
		t.Fatalf("id = %q, want TCreds", slot.ID)
	}
	if slot.Name != "折金票" {
		t.Fatalf("name = %q", slot.Name)
	}
}

func TestMigrateSnapshotFile_v1PreservesIDAndFillsName(t *testing.T) {
	t.Parallel()
	s := snapshotFile{
		SchemaVersion: 1,
		Records: []snapshotEntry{{
			Slots: []SlotRecord{{
				Slot:     0,
				ID:       "Protohedron",
				Discount: "-95%",
			}},
		}},
	}
	migrateSnapshotFile(&s)
	if s.SchemaVersion != schemaVersion {
		t.Fatalf("schema_version = %d, want %d", s.SchemaVersion, schemaVersion)
	}
	slot := s.Records[0].Slots[0]
	if slot.ID != "Protohedron" {
		t.Fatalf("id = %q", slot.ID)
	}
	if slot.Name == "" {
		t.Fatal("expected name filled from item map")
	}
}

func TestReadSnapshotFile_legacyJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := dir + "/snap.json"
	content := []byte(`{
    "schema_version": 1,
    "records": [{
        "uid": "u1",
        "game_date": "2026-05-20",
        "refresh_index": 1,
        "utc_time": "2026-05-20T00:00:00Z",
        "slots": [{"slot": 0, "item_id": "TCreds", "discount": "-75%"}]
    }]
}`)
	if err := writeFileAtomic(path, content, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readSnapshotFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SchemaVersion != schemaVersion {
		t.Fatalf("schema_version = %d", got.SchemaVersion)
	}
	slot := got.Records[0].Slots[0]
	if slot.ID != "TCreds" {
		t.Fatalf("id = %q", slot.ID)
	}
	if slot.Name == "" {
		t.Fatal("name should be filled on read migration")
	}
}
