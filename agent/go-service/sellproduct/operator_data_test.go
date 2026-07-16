package sellproduct

import "testing"

func TestBuildOperatorSelectionData(t *testing.T) {
	data := settlementTradeFile{
		Settlements: map[string]settlementTradeSettlement{
			"stm_tundra_1": {
				DomainID: "domain_1",
				SettlementName: map[string]string{
					"EN": "Refugee Camp",
				},
				SettlementFeatures: []settlementTradeFeature{
					{
						Bonuses: []settlementTradeBonus{{Type: "expProfit"}},
						MatchingOperators: []settlementTradeOperator{
							{Name: map[string]string{"EN": "Build", "CN": "建设"}},
							{Name: map[string]string{"EN": "Both", "CN": "双加成"}},
						},
					},
					{
						Bonuses: []settlementTradeBonus{{Type: "moneyProfit"}},
						MatchingOperators: []settlementTradeOperator{
							{Name: map[string]string{"EN": "Money", "CN": "收益"}},
							{Name: map[string]string{"EN": "Both", "CN": "双加成"}},
						},
					},
					{
						Bonuses: []settlementTradeBonus{{Type: "moneyProduceSpeed"}},
						MatchingOperators: []settlementTradeOperator{
							{Name: map[string]string{"EN": "Restore", "CN": "恢复"}},
						},
					},
				},
			},
		},
	}

	got := buildOperatorSelectionData(data, map[string]int{
		"Both":    0,
		"Money":   1,
		"Build":   2,
		"Restore": 3,
	})

	target := got.TargetCandidates["RefugeeCamp"]
	if len(target) != 3 {
		t.Fatalf("target candidates = %#v", target)
	}
	if target[0].Name != "Both" || target[0].Priority != 0 {
		t.Fatalf("first target candidate = %#v, want Both priority 0", target[0])
	}
	if target[0].CacheName != "双加成" {
		t.Fatalf("first target cache name = %q, want 双加成", target[0].CacheName)
	}
	if target[1].Name != "Money" || target[1].Priority != 1 {
		t.Fatalf("second target candidate = %#v, want Money priority 1", target[1])
	}
	if target[2].Name != "Build" || target[2].Priority != 2 {
		t.Fatalf("third target candidate = %#v, want Build priority 2", target[2])
	}

	if len(got.RestoreGroups) != 1 {
		t.Fatalf("restore groups = %#v", got.RestoreGroups)
	}
	restore := got.RestoreGroups[0].Candidates
	if len(restore) != 1 || restore[0].Name != "Restore" {
		t.Fatalf("restore candidates = %#v, want Restore", restore)
	}
	if restore[0].CacheName != "恢复" {
		t.Fatalf("restore cache name = %q, want 恢复", restore[0].CacheName)
	}
}

func TestLoadOperatorSelectionDataReadsSettlementTradeFile(t *testing.T) {
	got, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("loadOperatorSelectionData: %v", err)
	}
	if len(got.TargetCandidates["RefugeeCamp"]) == 0 {
		t.Fatal("RefugeeCamp target candidates should not be empty")
	}
	if len(got.RestoreGroups) == 0 {
		t.Fatal("restore groups should not be empty")
	}
}

func TestLoadOperatorSelectionDataCachedReusesDerivedData(t *testing.T) {
	first, err := loadOperatorSelectionDataCached()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	second, err := loadOperatorSelectionDataCached()
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if first != second {
		t.Fatal("cached loader should reuse the derived operator data")
	}
}

func TestResolveOperatorSelectionParamUsesDataFileCandidates(t *testing.T) {
	old := loadOperatorSelectionDataFunc
	defer func() {
		loadOperatorSelectionDataFunc = old
	}()
	loadOperatorSelectionDataFunc = func() (*operatorSelectionData, error) {
		return &operatorSelectionData{
			TargetCandidates: map[string][]operatorCandidate{
				"A": {{Name: "Target", Expected: []string{"目标"}}},
			},
			RestoreGroups: []operatorCandidateGroup{
				{Location: "A", Candidates: []operatorCandidate{{Name: "Restore", Expected: []string{"恢复"}}}},
			},
		}, nil
	}

	target, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageTarget,
		Location: "A",
	})
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	if len(target.Candidates) != 1 || target.Candidates[0].Name != "Target" {
		t.Fatalf("target candidates = %#v", target.Candidates)
	}

	restore, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageRestore,
		Location: "A",
	})
	if err != nil {
		t.Fatalf("resolve restore: %v", err)
	}
	if len(restore.RestoreGroups) != 1 || restore.RestoreGroups[0].Candidates[0].Name != "Restore" {
		t.Fatalf("restore groups = %#v", restore.RestoreGroups)
	}
}
