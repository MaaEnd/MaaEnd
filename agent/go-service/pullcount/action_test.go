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
			vals: resourceValues{ConvertedOriginiumOroberyl: 2925, Oroberyl: 20770, BondQuota: 80},
			sum:  voucherSummary{CarryToNextPulls: 3, DossierPulls: 10},
			want: calculationResult{
				ReservedOriginiumOroberyl: 2175,
				UsableOriginiumOroberyl:   750,
				OroberylPulls:             41,
				UsableOriginiumPulls:      1,
				BondQuotaPulls:            3,
				ResourcePulls:             46,
				CurrentPoolTotal:          49,
				NextPoolTotal:             69,
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
			got.BondQuotaPulls != tt.want.BondQuotaPulls ||
			got.ResourcePulls != tt.want.ResourcePulls ||
			got.CurrentPoolTotal != tt.want.CurrentPoolTotal ||
			got.NextPoolTotal != tt.want.NextPoolTotal {
			t.Fatalf("%s: calculatePullCount() = %+v, want key fields %+v", tt.name, got, tt.want)
		}
	}
}

// TestAddVoucher verifies voucher quantity accumulation and duplicate suppression.
func TestAddVoucher(t *testing.T) {
	session := newTestSession()

	if added := recordCarryToNextVoucher(session, "p1", voucherKindPermit, 16); !added {
		t.Fatal("recordCarryToNextVoucher first hit = false, want true")
	}
	if added := recordCarryToNextVoucher(session, "p1", voucherKindPermit, 16); added {
		t.Fatal("recordCarryToNextVoucher duplicate = true, want false")
	}
	if added := recordCarryToNextVoucher(session, "p2", voucherKindDossier, 1); !added {
		t.Fatal("recordCarryToNextVoucher second hit = false, want true")
	}
	if added := recordCarryToNextVoucher(session, "p3", voucherKindPermit, 0); added {
		t.Fatal("recordCarryToNextVoucher zero quantity = true, want false")
	}
	if session.Vouchers.CarryToNextPulls != 16 || session.Vouchers.DossierPulls != 10 {
		t.Fatalf("voucher summary = %+v, want carry 16 and dossier 10", session.Vouchers)
	}
}

// TestVoucherPulls verifies item quantity to pull-count conversion.
func TestVoucherPulls(t *testing.T) {
	if got := voucherPulls(voucherKindPermit, 16); got != 16 {
		t.Fatalf("voucherPulls(permit, 16) = %d, want 16", got)
	}
	if got := voucherPulls(voucherKindDossier, 1); got != 10 {
		t.Fatalf("voucherPulls(dossier, 1) = %d, want 10", got)
	}
	if got := voucherPulls(voucherKindDossier, 0); got != 0 {
		t.Fatalf("voucherPulls(dossier, 0) = %d, want 0", got)
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
