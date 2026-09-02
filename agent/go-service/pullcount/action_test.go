package pullcount

import (
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/ims"
)

func TestParseActionParam(t *testing.T) {
	if _, err := parseActionParam(""); err == nil {
		t.Fatal("expected error for empty param")
	}
	if _, err := parseActionParam(`{"items":{"item_diamond":"item_diamond_NUMBER"}}`); err == nil {
		t.Fatal("expected error for missing stage")
	}
	if _, err := parseActionParam(`{"stage":"record"}`); err == nil {
		t.Fatal("expected error for record without items")
	}
	if _, err := parseActionParam(`{"stage":"record","items":{"":"node"}}`); err == nil {
		t.Fatal("expected error for empty item id")
	}

	params, err := parseActionParam(`{
		"stage": "record",
		"items": {
			"  item_diamond  ": "  item_diamond_NUMBER  ",
			"item_originium_recharge": "ORIGEOMETRY_NUMBER"
		},
		"optional": true
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if params.Stage != stageRecord || !params.Optional {
		t.Fatalf("got %+v", params)
	}
	if params.Items["item_diamond"] != "item_diamond_NUMBER" {
		t.Fatalf("items=%v", params.Items)
	}

	finish, err := parseActionParam(`{"stage":"finish"}`)
	if err != nil {
		t.Fatal(err)
	}
	if finish.Stage != stageFinish || len(finish.Items) != 0 {
		t.Fatalf("finish=%+v", finish)
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

func TestSessionQuantityFallsBackToIMS(t *testing.T) {
	ims.ClearCache()
	t.Cleanup(ims.ClearCache)

	session := newRunSession()
	session.Items[itemDiamond] = 40
	if got := session.quantity(itemDiamond); got != 40 {
		t.Fatalf("session diamond=%d, want 40", got)
	}
	if got := session.quantity(itemOriginium); got != 0 {
		t.Fatalf("missing originium=%d, want 0", got)
	}

	ims.MarkSynced(time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC), map[string]int{
		itemOriginium: 39,
		itemSpecial:   2,
	})
	if got := session.quantity(itemOriginium); got != 39 {
		t.Fatalf("ims originium=%d, want 39", got)
	}
	if got := session.quantity(itemDiamond); got != 40 {
		t.Fatalf("recorded diamond should win over ims, got=%d", got)
	}
	if got := sumVoucherPulls(session.quantity); got != 2 {
		t.Fatalf("voucher pulls=%d, want 2", got)
	}
}
