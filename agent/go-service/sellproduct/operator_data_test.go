package sellproduct

import "testing"

func TestBuildOperatorSelectionDataUsesGeneratedOrder(t *testing.T) {
	got, err := buildOperatorSelectionData(testSellProductSelectionData())
	if err != nil {
		t.Fatalf("buildOperatorSelectionData: %v", err)
	}
	target := got.TargetCandidates["TestOutpost"]
	if len(target) != 3 {
		t.Fatalf("target candidates = %#v", target)
	}
	if target[0].Name != "Both" || target[0].Priority != 0 || target[0].BonusTier != 0 || target[0].CacheName != "双加成" {
		t.Fatalf("first target candidate = %#v", target[0])
	}
	if target[1].Name != "Money" || target[1].Priority != 1 || target[1].BonusTier != 1 {
		t.Fatalf("second target candidate = %#v", target[1])
	}
	if target[2].Name != "Build" || target[2].Priority != 2 || target[2].BonusTier != 2 {
		t.Fatalf("third target candidate = %#v", target[2])
	}
	if len(got.RestoreGroups) != 1 || got.RestoreGroups[0].Candidates[0].Name != "Restore" {
		t.Fatalf("restore groups = %#v", got.RestoreGroups)
	}
	if got.KnownOperators[0].Name != "Both" || got.KnownOperators[len(got.KnownOperators)-1].Name != "Restore" {
		t.Fatalf("known operators = %#v", got.KnownOperators)
	}
}

func TestBuildOperatorSelectionDataRejectsUnknownOperator(t *testing.T) {
	data := testSellProductSelectionData()
	location := data.Locations["TestOutpost"]
	location.TargetOperators = append(location.TargetOperators, selectionDataTargetOperator{Name: "Missing"})
	data.Locations["TestOutpost"] = location
	if _, err := buildOperatorSelectionData(data); err == nil {
		t.Fatal("unknown operator reference should fail")
	}
}

func TestLoadOperatorSelectionData(t *testing.T) {
	got, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("loadOperatorSelectionData: %v", err)
	}
	locations := []string{
		"RefugeeCamp",
		"InfraStation",
		"ReconstructionHQ",
		"SkyKingFlatsConstructionSite",
		"CardiacRemediationStation",
		"XiranflowCloudseederStation",
	}
	for _, location := range locations {
		if len(got.TargetCandidates[location]) == 0 {
			t.Errorf("%s target candidates should not be empty", location)
		}
	}
	if len(got.RestoreGroups) == 0 {
		t.Fatal("restore groups should not be empty")
	}
	foundDaPan := false
	for _, candidate := range got.KnownOperators {
		if candidate.Name == "DaPan" {
			foundDaPan = true
			break
		}
	}
	if !foundDaPan {
		t.Fatal("全量干员名称中缺少 DaPan")
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
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	operatorSessionRegisterLocation("A")
	old := loadOperatorSelectionDataFunc
	defer func() {
		loadOperatorSelectionDataFunc = old
	}()
	loadOperatorSelectionDataFunc = func() (*operatorSelectionData, error) {
		return &operatorSelectionData{
			KnownOperators: []operatorCandidate{{Name: "Known", CacheName: "已知", Expected: []string{"已知"}}},
			TargetCandidates: map[string][]operatorCandidate{
				"A": {{Name: "Target", CacheName: "目标", Expected: []string{"目标"}}},
			},
			RestoreGroups: []operatorCandidateGroup{
				{Location: "A", Candidates: []operatorCandidate{{Name: "Restore", CacheName: "恢复", Expected: []string{"恢复"}}}},
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

func TestResolveOperatorSelectionParamRejectsUnknownLocation(t *testing.T) {
	old := loadOperatorSelectionDataFunc
	defer func() {
		loadOperatorSelectionDataFunc = old
	}()
	loadOperatorSelectionDataFunc = func() (*operatorSelectionData, error) {
		return &operatorSelectionData{
			TargetCandidates: map[string][]operatorCandidate{
				"Known": {{Name: "Target", Expected: []string{"目标"}}},
			},
		}, nil
	}

	_, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageTarget,
		Location: "Unknown",
	})
	if err == nil {
		t.Fatal("unknown location should return a configuration error")
	}
}

func TestResolveOperatorSelectionParamRejectsMissingKnownOperators(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	operatorSessionRegisterLocation("Known")
	old := loadOperatorSelectionDataFunc
	defer func() {
		loadOperatorSelectionDataFunc = old
	}()
	loadOperatorSelectionDataFunc = func() (*operatorSelectionData, error) {
		return &operatorSelectionData{
			TargetCandidates: map[string][]operatorCandidate{
				"Known": {{Name: "Target", Expected: []string{"目标"}}},
			},
		}, nil
	}

	_, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageTarget,
		Location: "Known",
	})
	if err == nil {
		t.Fatal("missing known operators should return a configuration error")
	}
}

func TestResolveOperatorSelectionParamRejectsInactiveLocation(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	operatorSessionRegisterLocation("Active")
	old := loadOperatorSelectionDataFunc
	defer func() {
		loadOperatorSelectionDataFunc = old
	}()
	loadOperatorSelectionDataFunc = func() (*operatorSelectionData, error) {
		return &operatorSelectionData{
			KnownOperators: []operatorCandidate{{Name: "Known", CacheName: "已知", Expected: []string{"已知"}}},
			TargetCandidates: map[string][]operatorCandidate{
				"Inactive": {{Name: "Target", CacheName: "目标", Expected: []string{"目标"}}},
			},
		}, nil
	}

	_, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageTarget,
		Location: "Inactive",
	})
	if err == nil {
		t.Fatal("inactive location should return a configuration error")
	}
}
