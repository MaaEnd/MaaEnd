package iconqty

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestQuantityROIFromCellBox(t *testing.T) {
	// The band is 18px at each grid's base cell height and scales with the
	// actual cell, so the valuables/rewards results must stay bit-identical to
	// the historical roi_offset calibration (Win32 [0,78,0,-78] on a 96px cell,
	// ADB [0,98,0,-98] on a 120px cell).
	cases := []struct {
		name     string
		gridType string
		cell     maa.Rect
		want     maa.Rect
	}{
		{"valuables win32", GridValuables, maa.Rect{100, 200, 96, 96}, maa.Rect{100, 278, 96, 18}},
		{"valuables adb", GridValuables, maa.Rect{10, 20, 120, 120}, maa.Rect{10, 118, 120, 22}},
		{"rewards win32", GridRewards, maa.Rect{100, 200, 96, 96}, maa.Rect{100, 278, 96, 18}},
		{"rewards adb", GridRewards, maa.Rect{10, 20, 120, 120}, maa.Rect{10, 118, 120, 22}},
		{"transfer win32", GridTransfer, maa.Rect{10, 20, 64, 64}, maa.Rect{10, 66, 64, 18}},
		{"transfer adb", GridTransfer, maa.Rect{10, 20, 80, 80}, maa.Rect{10, 78, 80, 22}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := QuantityROIFromCellBox(tc.gridType, tc.cell)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("roi=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestQuantityROIFromCellBoxErrors(t *testing.T) {
	// Grids with no calibrated band are a config error, not a per-cell miss.
	cell := maa.Rect{0, 0, 96, 96}
	for _, gridType := range []string{"", "trade", "port_storager", "single_roi"} {
		if _, err := QuantityROIFromCellBox(gridType, cell); err == nil {
			t.Fatalf("grid_type %q should be unsupported", gridType)
		}
	}

	// Degenerate cells: cell[3]*18/96 truncates to 0 below 6px.
	for _, h := range []int{1, 5} {
		if _, err := QuantityROIFromCellBox(GridValuables, maa.Rect{0, 0, 96, h}); err == nil {
			t.Fatalf("cell height %d should have no quantity band", h)
		}
	}
	if _, err := QuantityROIFromCellBox(GridValuables, maa.Rect{0, 0, 96, 6}); err != nil {
		t.Fatalf("cell height 6 should still yield a band: %v", err)
	}

	if _, err := QuantityROIFromCellBox(GridValuables, maa.Rect{0, 0, 0, 96}); err == nil {
		t.Fatal("zero width cell_box should fail")
	}
	if _, err := QuantityROIFromCellBox(GridValuables, maa.Rect{0, 0, 96, 0}); err == nil {
		t.Fatal("zero height cell_box should fail")
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
