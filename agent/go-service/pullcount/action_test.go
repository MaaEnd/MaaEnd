package pullcount

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

// TestCalculatePullCount verifies the resource formula and fixed next-pool pulls.
func TestCalculatePullCount(t *testing.T) {
	param, err := parseActionParam("")
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}

	tests := []struct {
		name string
		vals resourceValues
		sum  voucherSummary
		want calculationResult
	}{
		{
			name: "issue resource example",
			vals: resourceValues{ConvertedOriginiumOroberyl: 2925, Oroberyl: 20770},
			sum:  voucherSummary{CurrentOnlyPulls: 2, CarryToNextPulls: 3, NextOnlyPulls: 10},
			want: calculationResult{
				ReservedOriginiumOroberyl: 2175,
				UsableOriginiumOroberyl:   750,
				ResourcePulls:             43,
				CurrentPoolTotal:          48,
				NextPoolTotal:             66,
			},
		},
		{
			name: "reserved originium clamps to zero",
			vals: resourceValues{ConvertedOriginiumOroberyl: 2000, Oroberyl: 499},
			want: calculationResult{
				ReservedOriginiumOroberyl: 2175,
				UsableOriginiumOroberyl:   0,
				ResourcePulls:             0,
				NextPoolTotal:             10,
			},
		},
	}

	for _, tt := range tests {
		got := calculatePullCount(tt.vals, tt.sum, param)
		if got.ReservedOriginiumOroberyl != tt.want.ReservedOriginiumOroberyl ||
			got.UsableOriginiumOroberyl != tt.want.UsableOriginiumOroberyl ||
			got.ResourcePulls != tt.want.ResourcePulls ||
			got.CurrentPoolTotal != tt.want.CurrentPoolTotal ||
			got.NextPoolTotal != tt.want.NextPoolTotal {
			t.Fatalf("%s: calculatePullCount() = %+v, want key fields %+v", tt.name, got, tt.want)
		}
	}
}

// TestAddVoucher verifies Pipeline-classified voucher accumulation and duplicate suppression.
func TestAddVoucher(t *testing.T) {
	session := newTestSession()
	recordPageQuantity(session, 1, 2)
	recordPageQuantity(session, 2, 3)

	if quantity, added, err := addVoucher(session, 1, "current_only", 1); err != nil || !added || quantity != 2 {
		t.Fatalf("addVoucher current = quantity %d added %v err %v, want 2 true nil", quantity, added, err)
	}
	if _, added, err := addVoucher(session, 1, "current_only", 1); err != nil || added {
		t.Fatalf("addVoucher duplicate = added %v err %v, want false nil", added, err)
	}
	if _, _, err := addVoucher(session, 2, "carry_to_next", 1); err != nil {
		t.Fatalf("addVoucher carry error = %v", err)
	}
	if quantity, added, err := addVoucher(session, 3, "next_only", 10); err != nil || !added || quantity != 1 {
		t.Fatalf("addVoucher next default quantity = quantity %d added %v err %v, want 1 true nil", quantity, added, err)
	}
	if session.Vouchers.CurrentOnlyPulls != 2 || session.Vouchers.CarryToNextPulls != 3 || session.Vouchers.NextOnlyPulls != 10 {
		t.Fatalf("voucher summary = %+v, want current/carry/next 2/3/10", session.Vouchers)
	}
}

// TestAddVoucherRejectsInvalidParams verifies Pipeline classification params are validated.
func TestAddVoucherRejectsInvalidParams(t *testing.T) {
	session := newTestSession()
	for _, tt := range []struct {
		cell      int
		scope     string
		pullValue int
	}{
		{0, "current_only", 1},
		{1, "bad_scope", 1},
		{1, "current_only", 2},
	} {
		if _, _, err := addVoucher(session, tt.cell, tt.scope, tt.pullValue); err == nil {
			t.Fatalf("addVoucher(%+v) error = nil, want error", tt)
		}
	}
}

// TestParseIntegerText verifies OCR counter parsing and rejection.
func TestParseIntegerText(t *testing.T) {
	for text, want := range map[string]int{
		" 20,770 |": 20770,
		"20770 1":   20770,
		"x 123 y":   123,
		"abc 456":   456,
		"987654321": 987654321,
	} {
		got, err := parseIntegerText(text)
		if err != nil || got != want {
			t.Fatalf("parseIntegerText(%q) = %d, %v; want %d", text, got, err, want)
		}
	}
	for _, text := range []string{"abc", " | ", ""} {
		if got, err := parseIntegerText(text); err == nil {
			t.Fatalf("parseIntegerText(%q) = %d, want error", text, got)
		}
	}
}

// TestScrollProbeUnchanged verifies bottom detection from Pipeline OCR probes.
func TestScrollProbeUnchanged(t *testing.T) {
	rule := testWarehouseScanConfig().Probe
	tests := []struct {
		name       string
		before     map[int]int
		after      map[int]int
		wantStop   bool
		wantMatch  int
		wantSample int
	}{
		{"exact", map[int]int{1: 30, 2: 80, 3: 80, 4: 135}, map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358}, true, 4, 4},
		{"one noise", map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2, 9: 2}, map[int]int{1: 30, 2: 8, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2, 9: 2}, true, 8, 9},
		{"too noisy", map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2, 9: 2}, map[int]int{1: 30, 2: 8, 3: 8, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2, 9: 2}, false, 7, 9},
		{"weak", map[int]int{1: 30, 2: 80, 3: 80}, map[int]int{1: 30, 2: 80, 3: 80}, false, 3, 3},
	}

	for _, tt := range tests {
		got, comparable, matches := quantityVectorsMostlyUnchanged(rule, tt.before, tt.after)
		if got != tt.wantStop || comparable != tt.wantSample || matches != tt.wantMatch {
			t.Fatalf("%s: quantityVectorsMostlyUnchanged() = %v/%d/%d, want %v/%d/%d", tt.name, got, comparable, matches, tt.wantStop, tt.wantSample, tt.wantMatch)
		}
	}
}

// TestRecordVisiblePage verifies repeated-page and max-page stopping.
func TestRecordVisiblePage(t *testing.T) {
	session := newTestSession()
	for cell, quantity := range map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358, 6: 4, 7: 10, 8: 2} {
		recordPageQuantity(session, cell, quantity)
	}

	if got := recordVisiblePage(session); got != 8 || session.PageCount != 1 {
		t.Fatalf("recordVisiblePage() items/page = %d/%d, want 8/1", got, session.PageCount)
	}
	session.CurrentPageCells = map[int]scannedCell{}
	for cell, quantity := range session.LastPageSignature {
		recordPageQuantity(session, cell, quantity)
	}
	recordVisiblePage(session)
	if !session.StopAfterPageDone {
		t.Fatal("StopAfterPageDone = false, want true for repeated page signature")
	}

	session = newTestSession()
	session.PageCount = session.ScanConfig.ScanMaxPages - 1
	recordVisiblePage(session)
	if !session.StopAfterPageDone {
		t.Fatal("StopAfterPageDone = false, want true at scan max pages")
	}
}

// TestScanConfigFromParams verifies scan thresholds are read from action params.
func TestScanConfigFromParams(t *testing.T) {
	param, err := parseActionParam(`{"scan_max_pages":3,"probe":{"cell_limit":2,"min_comparable":1,"max_mismatches":0,"min_match_ratio":1},"repeat_page":{"cell_limit":4,"min_comparable":2,"max_mismatches":1,"min_match_ratio":0.5}}`)
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}
	config := param.scanConfig()
	if config.ScanMaxPages != 3 || config.Probe.CellLimit != 2 || config.RepeatPage.CellLimit != 4 {
		t.Fatalf("scan config = %+v, want custom thresholds", config)
	}

	if _, err := parseActionParam(`{"probe":{"cell_limit":2,"min_comparable":0},"repeat_page":{"cell_limit":4,"min_comparable":2,"max_mismatches":1,"min_match_ratio":0.5}}`); err == nil {
		t.Fatal("parseActionParam() error = nil, want invalid probe error")
	}
}

// TestCustomRecognitionBranches verifies Pipeline can branch without graph overrides.
func TestCustomRecognitionBranches(t *testing.T) {
	session := newTestSession()
	session.StopAfterPageDone = true
	session.PageStopReason = "repeated page"
	if _, ok := pageShouldFinishResult(&maa.CustomRecognitionArg{}, session); !ok {
		t.Fatal("pageShouldFinishResult() ok = false, want true")
	}

	session.LastHeadProbe = map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358}
	session.CurrentProbe = map[int]int{1: 30, 2: 80, 3: 80, 4: 135, 5: 358}
	if _, ok := scrollProbeUnchangedResult(&maa.CustomRecognitionArg{}, session); !ok {
		t.Fatal("scrollProbeUnchangedResult() ok = false, want true")
	}
	session.CurrentProbe[2] = 8
	session.CurrentProbe[3] = 8
	if _, ok := scrollProbeUnchangedResult(&maa.CustomRecognitionArg{}, session); ok {
		t.Fatal("scrollProbeUnchangedResult() ok = true, want false")
	}
}

// newTestSession builds the minimal state needed by page-decision unit tests.
func newTestSession() *runSession {
	config := testWarehouseScanConfig()
	return &runSession{
		Param:            actionParam{},
		ScanConfig:       config,
		VoucherCells:     make(map[string]struct{}),
		CurrentPageCells: make(map[int]scannedCell),
	}
}

// testWarehouseScanConfig returns the production-like warehouse thresholds used by unit tests.
func testWarehouseScanConfig() warehouseScanConfig {
	return warehouseScanConfig{
		Probe:        warehouseSimilarityRule{CellLimit: 9, MinComparable: 4, MaxMismatches: 1, MinMatchRatio: 0.85},
		RepeatPage:   warehouseSimilarityRule{CellLimit: 45, MinComparable: 8, MaxMismatches: 1, MinMatchRatio: 0.85},
		ScanMaxPages: 8,
	}
}
