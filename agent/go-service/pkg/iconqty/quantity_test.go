package iconqty

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestQuantityROIFromCellBox(t *testing.T) {
	cell := maa.Rect{100, 200, 96, 96}
	roi, ok := ApplyROIOffset(cell, QuantityROIOffsetWin32)
	if !ok {
		t.Fatal("expected valid win32 quantity roi")
	}
	want := maa.Rect{100, 278, 96, 18}
	if roi != want {
		t.Fatalf("win32 roi=%v want=%v", roi, want)
	}

	adbCell := maa.Rect{10, 20, 120, 120}
	adbROI, ok := ApplyROIOffset(adbCell, QuantityROIOffsetADB)
	if !ok {
		t.Fatal("expected valid adb quantity roi")
	}
	wantADB := maa.Rect{10, 118, 120, 22}
	if adbROI != wantADB {
		t.Fatalf("adb roi=%v want=%v", adbROI, wantADB)
	}

	if _, ok := ApplyROIOffset(maa.Rect{0, 0, 96, 96}, QuantityROIOffsetADB); ok {
		t.Fatal("adb offset on 96-tall cell should be invalid")
	}
}

func TestDefaultItemFilters(t *testing.T) {
	if got := DefaultItemFilters(GridValuables); len(got) != 1 || got[0] != "ValuableDepot:*" {
		t.Fatalf("valuables=%v", got)
	}
	if got := DefaultItemFilters(GridRewards); len(got) != 2 {
		t.Fatalf("rewards=%v", got)
	}
}
