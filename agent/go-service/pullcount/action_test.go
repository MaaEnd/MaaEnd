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
	session.PendingQuantity["current_only"] = 2

	if quantity, added, err := addVoucher(session, "p1:current", "current_only", 1); err != nil || !added || quantity != 2 {
		t.Fatalf("addVoucher current = quantity %d added %v err %v, want 2 true nil", quantity, added, err)
	}
	session.PendingQuantity["current_only"] = 2
	if _, added, err := addVoucher(session, "p1:current", "current_only", 1); err != nil || added {
		t.Fatalf("addVoucher duplicate = added %v err %v, want false nil", added, err)
	}
	session.PendingQuantity["carry_to_next"] = 3
	if _, _, err := addVoucher(session, "p1:carry", "carry_to_next", 1); err != nil {
		t.Fatalf("addVoucher carry error = %v", err)
	}
	if quantity, added, err := addVoucher(session, "p1:next", "next_only", 10); err != nil || !added || quantity != 1 {
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
		scope     string
		pullValue int
	}{
		{"bad_scope", 1},
		{"current_only", 2},
	} {
		if _, _, err := addVoucher(session, "key", tt.scope, tt.pullValue); err == nil {
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

// TestPageShouldFinish verifies the Pipeline finish branch is still driven by Go state.
func TestPageShouldFinish(t *testing.T) {
	session := newTestSession()
	session.StopAfterPageDone = true
	session.PageStopReason = "max pages"
	if _, ok := pageShouldFinishResult(&maa.CustomRecognitionArg{}, session); !ok {
		t.Fatal("pageShouldFinishResult() ok = false, want true")
	}

	session.StopAfterPageDone = false
	if _, ok := pageShouldFinishResult(&maa.CustomRecognitionArg{}, session); ok {
		t.Fatal("pageShouldFinishResult() ok = true, want false")
	}
}

// TestParseActionParam verifies calculation params reject invalid scan limits.
func TestParseActionParam(t *testing.T) {
	param, err := parseActionParam(`{"scan_max_pages":3}`)
	if err != nil {
		t.Fatalf("parseActionParam() error = %v", err)
	}
	if param.ScanMaxPages != 3 {
		t.Fatalf("ScanMaxPages = %d, want 3", param.ScanMaxPages)
	}

	if _, err := parseActionParam(`{"scan_max_pages":0}`); err == nil {
		t.Fatal("parseActionParam() error = nil, want invalid scan_max_pages error")
	}
}

// newTestSession builds the minimal state needed by page-decision unit tests.
func newTestSession() *runSession {
	return &runSession{
		Param:           defaultActionParam,
		VoucherHits:     make(map[string]struct{}),
		PendingQuantity: make(map[string]int),
	}
}
