package strategy

import "testing"

func TestNewBuildsConfiguredSelectors(t *testing.T) {
	rarity, ok := New(KindRarity, Config{})
	if !ok {
		t.Fatal("rarity selector should be created")
	}
	selected, ok := rarity.Select([]Candidate{
		{ItemID: "low", Rarity: 2, UnitPrice: 100},
		{ItemID: "high", Rarity: 3, UnitPrice: 10},
	})
	if !ok || selected.ItemID != "high" {
		t.Fatalf("rarity selector result = %+v, %v", selected, ok)
	}

	price, ok := New(KindPrice, Config{})
	if !ok {
		t.Fatal("price selector should be created")
	}
	selected, ok = price.Select([]Candidate{
		{ItemID: "rare", Rarity: 4, UnitPrice: 20},
		{ItemID: "expensive", Rarity: 2, UnitPrice: 100},
	})
	if !ok || selected.ItemID != "expensive" {
		t.Fatalf("price selector result = %+v, %v", selected, ok)
	}

	stock, ok := New(KindStock, Config{MinimumUnitPrice: 10})
	if !ok {
		t.Fatal("stock selector should be created")
	}
	selected, ok = stock.Select([]Candidate{
		{ItemID: "cheap", Stock: 1000, StockKnown: true, UnitPrice: 2},
		{ItemID: "eligible", Stock: 100, StockKnown: true, UnitPrice: 10},
	})
	if !ok || selected.ItemID != "eligible" {
		t.Fatalf("stock selector result = %+v, %v", selected, ok)
	}
}

func TestNewRejectsUnknownOrInvalidStrategy(t *testing.T) {
	if _, ok := New(Kind("unknown"), Config{}); ok {
		t.Fatal("unknown strategy should be rejected")
	}
	if _, ok := New(KindStock, Config{MinimumUnitPrice: -1}); ok {
		t.Fatal("negative minimum unit price should be rejected")
	}
}
