package sellproduct

import "testing"

func testSellProductSelectionData() *sellProductSelectionDataFile {
	return &sellProductSelectionDataFile{
		Items: map[string]selectionDataItem{
			"item_a": {Names: map[string]string{"zh_cn": "物品甲", "en_us": "Item A"}},
			"item_b": {Names: map[string]string{"zh_cn": "物品乙", "en_us": "Item B"}},
		},
		Operators: map[string]selectionDataOperator{
			"Both":          {Names: map[string]string{"zh_cn": "双加成", "en_us": "Both"}},
			"Money":         {Names: map[string]string{"zh_cn": "收益", "en_us": "Money"}},
			"Build":         {Names: map[string]string{"zh_cn": "建设", "en_us": "Build"}},
			"Restore":       {Names: map[string]string{"zh_cn": "恢复", "en_us": "Restore"}},
			"OtherOperator": {Names: map[string]string{"zh_cn": "其他干员", "en_us": "Other Operator"}},
		},
		LocationOrder: []string{"TestOutpost"},
		Locations: map[string]selectionDataLocation{
			"TestOutpost": {
				ItemOrder: []string{
					"item_b",
					"item_a",
				},
				TargetOperators: []selectionDataTargetOperator{
					{Name: "Both", BonusTier: 0},
					{Name: "Money", BonusTier: 1},
					{Name: "Build", BonusTier: 2},
				},
				RestoreOperators: []string{"Restore"},
			},
		},
	}
}

func TestSelectionExpectedNamesUsesLocaleOrderAndDeduplicates(t *testing.T) {
	got := selectionExpectedNames(map[string]string{
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

func TestLoadSellProductSelectionData(t *testing.T) {
	data, err := loadSellProductSelectionData()
	if err != nil {
		t.Fatalf("loadSellProductSelectionData: %v", err)
	}
	if len(data.Items) == 0 || len(data.Operators) == 0 || len(data.Locations) == 0 {
		t.Fatalf("selection data is incomplete: %+v", data)
	}
}
