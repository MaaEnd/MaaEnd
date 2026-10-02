package creditshopping

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestRecordAssembleSlotRecordsUnknown(t *testing.T) {
	slots := []maa.Rect{
		box(100, 200),
		box(200, 200),
		box(300, 200),
	}
	items := []itemPositionHit{
		{Box: box(102, 205), Name: "A", ID: "A"},
	}
	matched := recordMatchItemsToSlots(slots, items)
	got := recordAssembleSlotRecords(slots, matched, func(maa.Rect) string { return recordDiscountNone })
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].ID != "A" || got[0].Name != "A" {
		t.Fatalf("slot 0 = %+v", got[0])
	}
	for i := 1; i < 3; i++ {
		if got[i].ID != recordSlotItemIDUnknown || got[i].Name != recordSlotNameUnknown {
			t.Fatalf("slot %d = %+v, want unknown", i, got[i])
		}
		if got[i].Slot != i {
			t.Fatalf("slot index = %d, want %d", got[i].Slot, i)
		}
	}
}
