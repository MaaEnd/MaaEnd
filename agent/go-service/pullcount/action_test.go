package pullcount

import (
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
)

func TestParseActionParam(t *testing.T) {
	empty, err := parseActionParam("")
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("empty param should be ok, got %+v err=%v", empty, err)
	}
	if _, err := parseActionParam(`{"items":{"":"node"}}`); err == nil {
		t.Fatal("expected error for empty item id")
	}

	params, err := parseActionParam(`{
		"items": {
			"  item_ticketgacha_special_single  ": "  VoucherNode  "
		},
		"optional": false
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if resolveOptional(params.Optional) {
		t.Fatal("expected optional false")
	}
	if params.Items["item_ticketgacha_special_single"] != "VoucherNode" {
		t.Fatalf("items=%v", params.Items)
	}
	if !resolveOptional(nil) {
		t.Fatal("omitted optional should default true")
	}
}

func TestCalculatePullCount(t *testing.T) {
	tests := []struct {
		name string
		vals resourceValues
		sum  voucherSummary
		want calculationResult
	}{
		{
			name: "issue resource example",
			vals: resourceValues{Originium: 39, Oroberyl: 20770},
			sum:  voucherSummary{CarryToNextPulls: 3},
			want: calculationResult{
				ReservedOriginiumOroberyl:  2175,
				ConvertedOriginiumOroberyl: 2925,
				UsableOriginiumOroberyl:    750,
				OroberylPulls:              41,
				UsableOriginiumPulls:       1,
				ResourcePulls:              43,
				CurrentPoolTotal:           46,
				NextPoolTotal:              56,
			},
		},
		{
			name: "reserved originium clamps to zero",
			vals: resourceValues{Originium: 26, Oroberyl: 499},
			want: calculationResult{
				ReservedOriginiumOroberyl:  2175,
				ConvertedOriginiumOroberyl: 1950,
				UsableOriginiumOroberyl:    0,
				ResourcePulls:              0,
				NextPoolTotal:              10,
			},
		},
	}

	for _, tt := range tests {
		got := calculatePullCount(tt.vals, tt.sum)
		if got.ReservedOriginiumOroberyl != tt.want.ReservedOriginiumOroberyl ||
			got.ConvertedOriginiumOroberyl != tt.want.ConvertedOriginiumOroberyl ||
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

func TestSumVoucherPulls(t *testing.T) {
	qty := map[string]int{
		itemSpecial:    3,
		itemSpecialLT:  1,
		itemSpecialTen: 2,
	}
	got := sumVoucherPulls(func(id string) int { return qty[id] })
	if got != 24 {
		t.Fatalf("sumVoucherPulls=%d, want 24", got)
	}
}

func TestQuantityOfPrefersRecognizedVouchersAndIMSCurrencies(t *testing.T) {
	ims.ClearCache()
	t.Cleanup(ims.ClearCache)

	recognized := map[string]int{
		itemDiamond: 40,
		itemSpecial: 3,
	}
	if got := quantityOf(recognized, itemDiamond); got != 0 {
		t.Fatalf("diamond without ims=%d, want 0", got)
	}
	if got := quantityOf(recognized, itemSpecial); got != 3 {
		t.Fatalf("voucher recognized=%d, want 3", got)
	}

	ims.MarkSynced(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), map[string]int{
		itemDiamond:   20770,
		itemOriginium: 39,
		itemSpecial:   2,
	})
	if got := quantityOf(recognized, itemDiamond); got != 20770 {
		t.Fatalf("diamond ims=%d, want 20770", got)
	}
	if got := quantityOf(recognized, itemOriginium); got != 39 {
		t.Fatalf("originium ims=%d, want 39", got)
	}
	if got := quantityOf(recognized, itemSpecial); got != 3 {
		t.Fatalf("recognized voucher should win over ims, got=%d", got)
	}
}
