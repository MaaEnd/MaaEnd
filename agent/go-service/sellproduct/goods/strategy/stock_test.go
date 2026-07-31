package strategy

import "testing"

func TestStockUsesStockThenPriceThenRarityThenStableOrder(t *testing.T) {
	selector := Stock{MinimumUnitPrice: 3}
	candidates := []Candidate{
		{ItemID: "cheap", Stock: 99999, StockKnown: true, UnitPrice: 2},
		{ItemID: "first", Stock: 1000, StockKnown: true, Rarity: 2, UnitPrice: 10},
		{ItemID: "expensive", Stock: 1000, StockKnown: true, Rarity: 2, UnitPrice: 70},
		{ItemID: "largest", Stock: 2000, StockKnown: true, Rarity: 3, UnitPrice: 30},
	}

	candidate, ok := selector.Select(candidates)
	if !ok || candidate.ItemID != "largest" {
		t.Fatalf("stock candidate = %+v, %v", candidate, ok)
	}
	candidates[3].Stock = 1000
	candidate, ok = selector.Select(candidates)
	if !ok || candidate.ItemID != "expensive" {
		t.Fatalf("equal-stock candidate = %+v, %v", candidate, ok)
	}
	candidates[2].UnitPrice = 10
	candidates[3].UnitPrice = 10
	candidate, ok = selector.Select(candidates)
	if !ok || candidate.ItemID != "largest" {
		t.Fatalf("equal-stock-price candidate = %+v, %v", candidate, ok)
	}
	candidates[3].Rarity = 2
	candidate, ok = selector.Select(candidates)
	if !ok || candidate.ItemID != "first" {
		t.Fatalf("fully tied candidate = %+v, %v", candidate, ok)
	}
}

func TestStockRejectsCandidatesBelowMinimumPrice(t *testing.T) {
	if _, ok := (Stock{MinimumUnitPrice: 3}).Select([]Candidate{{ItemID: "cheap", Stock: 99999, StockKnown: true, UnitPrice: 2}}); ok {
		t.Fatal("candidate below minimum unit price must not be selected")
	}
}

func TestStockRejectsCandidateWithUnknownStock(t *testing.T) {
	if _, ok := (Stock{}).Select([]Candidate{{ItemID: "unknown", UnitPrice: 70}}); ok {
		t.Fatal("库存未知的候选不应进入库存优先选择")
	}
}
