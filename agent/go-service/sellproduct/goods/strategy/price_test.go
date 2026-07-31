package strategy

import "testing"

func TestPriceUsesPriceThenRarity(t *testing.T) {
	candidate, ok := (Price{}).Select([]Candidate{
		{ItemID: "rare_cheap", Rarity: 4, UnitPrice: 20},
		{ItemID: "common_expensive", Rarity: 2, UnitPrice: 100},
		{ItemID: "rare_expensive", Rarity: 3, UnitPrice: 100},
	})
	if !ok || candidate.ItemID != "rare_expensive" {
		t.Fatalf("price candidate = %+v, %v", candidate, ok)
	}
}

func TestPriceKeepsInputOrderForEqualValue(t *testing.T) {
	candidate, ok := (Price{}).Select([]Candidate{
		{ItemID: "first", Rarity: 3, UnitPrice: 100},
		{ItemID: "second", Rarity: 3, UnitPrice: 100},
	})
	if !ok || candidate.ItemID != "first" {
		t.Fatalf("price candidate = %+v, %v", candidate, ok)
	}
}

func TestPriceSortsCopyWithoutModifyingInput(t *testing.T) {
	candidates := []Candidate{
		{ItemID: "cheap", Rarity: 4, UnitPrice: 20},
		{ItemID: "expensive", Rarity: 2, UnitPrice: 100},
	}
	ordered := (Price{}).Sort(candidates)
	if ordered[0].ItemID != "expensive" || candidates[0].ItemID != "cheap" {
		t.Fatalf("ordered = %+v, input = %+v", ordered, candidates)
	}
}

func TestPriceRejectsEmptyCandidates(t *testing.T) {
	if _, ok := (Price{}).Select(nil); ok {
		t.Fatal("empty price candidates must not select an item")
	}
}
