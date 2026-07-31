package strategy

import "testing"

func TestRarityUsesRarityThenPrice(t *testing.T) {
	candidate, ok := (Rarity{}).Select([]Candidate{
		{ItemID: "low", Stock: 99999, Rarity: 2, UnitPrice: 100},
		{ItemID: "high_cheap", Stock: 10, Rarity: 3, UnitPrice: 80},
		{ItemID: "high_expensive", Stock: 1, Rarity: 3, UnitPrice: 120},
	})
	if !ok || candidate.ItemID != "high_expensive" {
		t.Fatalf("rarity candidate = %+v, %v", candidate, ok)
	}
}

func TestRarityKeepsInputOrderForEqualValue(t *testing.T) {
	candidate, ok := (Rarity{}).Select([]Candidate{
		{ItemID: "first", Rarity: 3, UnitPrice: 100},
		{ItemID: "second", Rarity: 3, UnitPrice: 100},
	})
	if !ok || candidate.ItemID != "first" {
		t.Fatalf("rarity candidate = %+v, %v", candidate, ok)
	}
}

func TestRaritySortsCopyWithoutModifyingInput(t *testing.T) {
	candidates := []Candidate{
		{ItemID: "low", Rarity: 2, UnitPrice: 100},
		{ItemID: "high", Rarity: 3, UnitPrice: 10},
	}
	ordered := (Rarity{}).Sort(candidates)
	if ordered[0].ItemID != "high" || candidates[0].ItemID != "low" {
		t.Fatalf("ordered = %+v, input = %+v", ordered, candidates)
	}
}

func TestRarityRejectsEmptyCandidates(t *testing.T) {
	if _, ok := (Rarity{}).Select(nil); ok {
		t.Fatal("empty rarity candidates must not select an item")
	}
}
