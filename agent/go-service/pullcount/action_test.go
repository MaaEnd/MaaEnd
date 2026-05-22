package pullcount

import "testing"

// TestCalculatePullCount verifies the resource formula and fixed next-pool pulls.
func TestCalculatePullCount(t *testing.T) {
	tests := []struct {
		name string
		vals resourceValues
		sum  voucherSummary
		want calculationResult
	}{
		{
			name: "issue resource example",
			vals: resourceValues{ConvertedOriginiumOroberyl: 2925, Oroberyl: 20770},
			sum:  voucherSummary{CarryToNextPulls: 3},
			want: calculationResult{
				ReservedOriginiumOroberyl: 2175,
				UsableOriginiumOroberyl:   750,
				OroberylPulls:             41,
				UsableOriginiumPulls:      1,
				ResourcePulls:             43,
				CurrentPoolTotal:          46,
				NextPoolTotal:             56,
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
		got := calculatePullCount(tt.vals, tt.sum)
		if got.ReservedOriginiumOroberyl != tt.want.ReservedOriginiumOroberyl ||
			got.UsableOriginiumOroberyl != tt.want.UsableOriginiumOroberyl ||
			got.OroberylPulls != tt.want.OroberylPulls ||
			got.UsableOriginiumPulls != tt.want.UsableOriginiumPulls ||
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

	if quantity, added, err := addVoucher(session, "p1:carry", "carry_to_next", 1); err != nil || !added || quantity != 1 {
		t.Fatalf("addVoucher carry = quantity %d added %v err %v, want 1 true nil", quantity, added, err)
	}
	if _, added, err := addVoucher(session, "p1:carry", "carry_to_next", 1); err != nil || added {
		t.Fatalf("addVoucher duplicate = added %v err %v, want false nil", added, err)
	}
	if quantity, added, err := addVoucher(session, "p1:carry2", "carry_to_next", 1); err != nil || !added || quantity != 1 {
		t.Fatalf("addVoucher default quantity = quantity %d added %v err %v, want 1 true nil", quantity, added, err)
	}
	if session.Vouchers.CarryToNextPulls != 2 {
		t.Fatalf("voucher summary = %+v, want carry 2", session.Vouchers)
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
		{"carry_to_next", 2},
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

// newTestSession builds the minimal state needed by page-decision unit tests.
func newTestSession() *runSession {
	return newRunSession()
}
