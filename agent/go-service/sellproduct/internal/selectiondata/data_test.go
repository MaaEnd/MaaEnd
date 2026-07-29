package selectiondata

import "testing"

func TestExpectedNamesUsesLocaleOrderAndDeduplicates(t *testing.T) {
	got := ExpectedNames(map[string]string{
		"zh_cn": "息壤",
		"zh_tw": "息壤",
		"en_us": "Xiranite",
		"ja_jp": "息壌",
		"ko_kr": "식양",
	})
	want := []string{"息壤", "Xiranite", "息壌", "식양"}
	if len(got) != len(want) {
		t.Fatalf("expected names = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected names = %v, want %v", got, want)
		}
	}
}

func TestLoad(t *testing.T) {
	data, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(data.Items) == 0 || len(data.Operators) == 0 || len(data.Locations) == 0 {
		t.Fatalf("selection data is incomplete: %+v", data)
	}
}

func TestLocationNameFallsBackToStableID(t *testing.T) {
	const location = "UnknownLocation"
	if got := LocationName(location); got != location {
		t.Fatalf("location name = %q, want %q", got, location)
	}
}

func TestItemNameFallsBackToStableID(t *testing.T) {
	const itemID = "UnknownItem"
	if got := ItemName(itemID); got != itemID {
		t.Fatalf("item name = %q, want %q", got, itemID)
	}
}

func TestOperatorNameFallsBackToStableID(t *testing.T) {
	const operator = "UnknownOperator"
	if got := OperatorName(operator); got != operator {
		t.Fatalf("operator name = %q, want %q", got, operator)
	}
}

func TestDomainValidationIsIndependent(t *testing.T) {
	base := File{
		LocationOrder: []string{"Outpost"},
		Locations:     map[string]Location{"Outpost": {}},
	}
	goodsData := base
	goodsData.Items = map[string]Item{"item": {}}
	if err := ValidateGoods(&goodsData); err != nil {
		t.Fatalf("goods validation should not require operators: %v", err)
	}
	operatorData := base
	operatorData.Operators = map[string]Operator{"Operator": {}}
	if err := ValidateOperators(&operatorData); err != nil {
		t.Fatalf("operator validation should not require items: %v", err)
	}
}

func TestLocationItemsContainValueAttributes(t *testing.T) {
	data, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for locationName, location := range data.Locations {
		for _, item := range location.Items {
			if item.Rarity <= 0 || item.UnitPrice <= 0 {
				t.Fatalf("location %q item %+v must have positive value attributes", locationName, item)
			}
		}
	}
}
