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
	t.Run("win32 log geometry", func(t *testing.T) {
		slots := []maa.Rect{
			{166, 240, 22, 21},
			{327, 240, 22, 21},
			{488, 240, 22, 21},
			{649, 240, 22, 21},
			{810, 240, 22, 21},
			{970, 240, 22, 21},
			{1131, 240, 22, 21},
			{165, 446, 22, 21},
			{326, 446, 22, 21},
			{487, 446, 22, 21},
		}
		items := []itemPositionHit{
			{Box: maa.Rect{82, 106, 150, 166}, Name: "武库配额", ID: "ArsenalTicket"},
			{Box: maa.Rect{404, 312, 150, 166}, Name: "武器检查装置", ID: "ArmsINSPKit"},
			{Box: maa.Rect{726, 106, 150, 166}, Name: "初级认知载体", ID: "ElementaryCognitiveCarrier"},
		}
		got := recordMatchItemsToSlots(slots, items)
		if got[0].ID != "ArsenalTicket" {
			t.Fatalf("slot 0 = %+v", got[0])
		}
		if got[4].ID != "ElementaryCognitiveCarrier" {
			t.Fatalf("slot 4 = %+v, want ElementaryCognitiveCarrier", got[4])
		}
		if got[9].ID != "ArmsINSPKit" {
			t.Fatalf("slot 9 = %+v, want ArmsINSPKit", got[9])
		}
	})

	t.Run("sparse single middle hit", func(t *testing.T) {
		slots := []maa.Rect{
			{100, 400, 22, 21},
			{180, 400, 22, 21},
			{260, 400, 22, 21},
		}
		items := []itemPositionHit{
			{Box: maa.Rect{230, 280, 150, 166}, Name: "M", ID: "M"},
		}
		got := recordMatchItemsToSlots(slots, items)
		if got[2].ID != "M" {
			t.Fatalf("slot 2 = %+v, want M", got[2])
		}
	})

	t.Run("reject far orphan hit", func(t *testing.T) {
		slots := []maa.Rect{box(100, 200), box(200, 200)}
		items := []itemPositionHit{
			{Box: maa.Rect{900, 500, 150, 166}, Name: "X", ID: "X"},
		}
		got := recordMatchItemsToSlots(slots, items)
		if got[0].ID != "" || got[1].ID != "" {
			t.Fatalf("expected no assignment, got %+v", got)
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
