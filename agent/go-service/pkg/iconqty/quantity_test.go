package iconqty

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
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

func TestItemDisplayNameFallback(t *testing.T) {
	if got := ItemDisplayName("UNKNOWN_ITEM_XYZ"); got != "UNKNOWN_ITEM_XYZ" {
		t.Fatalf("unknown fallback=%q", got)
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

func TestEmptyMatches(t *testing.T) {
	noMatch := iconrecognition.Detail{
		Matched: false,
		Error:   &iconrecognition.DetailError{Code: iconrecognition.ErrorCodeNoMatch, Message: "No item reached the configured threshold"},
	}
	empty, err := emptyMatches(noMatch, false)
	if err != nil || !empty {
		t.Fatalf("no_match: empty=%v err=%v", empty, err)
	}

	gridMiss := iconrecognition.Detail{
		Matched: false,
		Error:   &iconrecognition.DetailError{Code: iconrecognition.ErrorCodeGridDetectionFailed, Message: "rewards ROI contains no card candidates"},
	}
	empty, err = emptyMatches(gridMiss, false)
	if err == nil || empty {
		t.Fatalf("grid miss without tolerate: empty=%v err=%v", empty, err)
	}
	empty, err = emptyMatches(gridMiss, true)
	if err != nil || !empty {
		t.Fatalf("grid miss with tolerate: empty=%v err=%v", empty, err)
	}

	invalid := iconrecognition.Detail{
		Matched: false,
		Error:   &iconrecognition.DetailError{Code: iconrecognition.ErrorCodeInvalidArgument, Message: "bad roi"},
	}
	empty, err = emptyMatches(invalid, true)
	if err == nil || empty {
		t.Fatalf("invalid_argument must stay hard: empty=%v err=%v", empty, err)
	}
}
