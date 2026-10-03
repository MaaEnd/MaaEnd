package creditshopping

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func box(x, y int) maa.Rect {
	return maa.Rect{x, y, 10, 10}
}

func TestRecordOrderSlotBoxesByLayout(t *testing.T) {
	t.Run("adb 5+5", func(t *testing.T) {
		var boxes []maa.Rect
		for i := 0; i < 5; i++ {
			boxes = append(boxes, box(100+i*80, 200))
		}
		for i := 0; i < 5; i++ {
			boxes = append(boxes, box(100+i*80, 400))
		}
		got := recordOrderSlotBoxesByLayout(boxes, recordShelfLayout{5, 5})
		if len(got) != 10 {
			t.Fatalf("len = %d, want 10", len(got))
		}
		if recordRectCenterY(got[4]) >= recordRectCenterY(got[5]) {
			t.Fatal("expected top row before bottom row")
		}
	})

	t.Run("win32 7+3", func(t *testing.T) {
		var boxes []maa.Rect
		for i := 0; i < 7; i++ {
			boxes = append(boxes, box(80+i*60, 180))
		}
		for i := 0; i < 3; i++ {
			boxes = append(boxes, box(200+i*80, 380))
		}
		got := recordOrderSlotBoxesByLayout(boxes, recordShelfLayout{7, 3})
		if len(got) != 10 {
			t.Fatalf("len = %d, want 10", len(got))
		}
		if recordRectCenterY(got[6]) >= recordRectCenterY(got[7]) {
			t.Fatal("expected 7 top slots before 3 bottom slots")
		}
	})
}

func TestRecordMatchItemsToSlots(t *testing.T) {
	t.Run("aligned pair", func(t *testing.T) {
		slots := []maa.Rect{
			box(100, 200),
			box(200, 200),
		}
		items := []itemPositionHit{
			{Box: box(102, 205), Name: "A", ID: "A"},
			{Box: box(198, 198), Name: "B", ID: "B"},
		}
		got := recordMatchItemsToSlots(slots, items)
		if got[0].ID != "A" || got[1].ID != "B" {
			t.Fatalf("match = %+v", got)
		}
	})

	t.Run("sparse single middle hit", func(t *testing.T) {
		var slots []maa.Rect
		for i := 0; i < 5; i++ {
			slots = append(slots, box(100+i*80, 200))
		}
		items := []itemPositionHit{
			{Box: box(100+2*80+2, 205), Name: "M", ID: "M"},
		}
		got := recordMatchItemsToSlots(slots, items)
		for i, want := range []string{"", "", "M", "", ""} {
			if got[i].ID != want {
				t.Fatalf("slot %d ID = %q, want %q; full=%+v", i, got[i].ID, want, got)
			}
		}
	})
}

func TestRecordLayoutFromControlType(t *testing.T) {
	adb := recordLayoutFromControlType("adb")
	if adb.topRow != 5 || adb.bottomRow != 5 {
		t.Fatalf("adb layout = %+v", adb)
	}
	pc := recordLayoutFromControlType("win32")
	if pc.topRow != 7 || pc.bottomRow != 3 {
		t.Fatalf("win32 layout = %+v", pc)
	}
}
