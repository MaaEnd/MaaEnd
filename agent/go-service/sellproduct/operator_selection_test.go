package sellproduct

import (
	"slices"
	"testing"
)

// TestCandidatesForCurrentSelectionSkipsTemporarilyExcludedOperator 验证派驻冲突干员会被临时跳过并选择下一候选。
func TestCandidatesForCurrentSelectionSkipsTemporarilyExcludedOperator(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "RefugeeCamp",
		Candidates: []operatorCandidate{
			{Name: "Best", Priority: 0},
			{Name: "Fallback", Priority: 1},
		},
		ExcludedOperators: map[string]struct{}{
			"Best": {},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Best", "Fallback"}))
	if len(candidates) != 1 || candidates[0].Name != "Fallback" {
		t.Fatalf("候选 = %#v，期望仅包含 Fallback", candidates)
	}
}

// TestRestoreSelectionReplansAfterTemporaryExclusion 验证恢复候选冲突后会重新计算全局分配。
func TestRestoreSelectionReplansAfterTemporaryExclusion(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "RefugeeCamp",
		ActiveLocations: map[string]struct{}{
			"RefugeeCamp": {},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "RefugeeCamp",
				Candidates: []operatorCandidate{
					{Name: "Best", Priority: 0},
					{Name: "Fallback", Priority: 1},
				},
			},
		},
		ExcludedOperators: map[string]struct{}{
			"Best": {},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Best", "Fallback"}))
	if len(candidates) != 1 || candidates[0].Name != "Fallback" {
		t.Fatalf("恢复候选 = %#v，期望重新规划为 Fallback", candidates)
	}
}

// TestBuildRestoreAssignmentPlanUniqueOperators 验证同一名干员不会同时分配给两个据点。
func TestBuildRestoreAssignmentPlanUniqueOperators(t *testing.T) {
	groups := []operatorCandidateGroup{
		{
			Location: "A",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
				{Name: "AOnly", Priority: 1},
			},
		},
		{
			Location: "B",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
				{Name: "BOnly", Priority: 1},
			},
		},
	}
	owned := operatorIDSet([]string{"Shared", "AOnly", "BOnly"})

	plan := buildRestoreAssignmentPlan(groups, owned)
	if plan.Assigned != 2 {
		t.Fatalf("已分配据点数 = %d，期望 2", plan.Assigned)
	}
	a := plan.Assignments["A"].Name
	b := plan.Assignments["B"].Name
	if a == "" || b == "" {
		t.Fatalf("存在据点缺少分配结果：%#v", plan.Assignments)
	}
	if a == b {
		t.Fatalf("同一干员被分配到两个据点：A=%s，B=%s", a, b)
	}
	if a != "Shared" && b != "Shared" {
		t.Fatalf("共享的最优干员应分配给其中一个据点，实际 A=%s，B=%s", a, b)
	}
}

// TestBuildRestoreAssignmentPlanMaximizesAssignedLocations 验证全局方案优先最大化可恢复的据点数量。
func TestBuildRestoreAssignmentPlanMaximizesAssignedLocations(t *testing.T) {
	groups := []operatorCandidateGroup{
		{
			Location: "A",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
				{Name: "AOnly", Priority: 9},
			},
		},
		{
			Location: "B",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
			},
		},
	}
	owned := operatorIDSet([]string{"Shared", "AOnly"})

	plan := buildRestoreAssignmentPlan(groups, owned)
	if plan.Assigned != 2 {
		t.Fatalf("已分配据点数 = %d，期望 2", plan.Assigned)
	}
	if got := plan.Assignments["B"].Name; got != "Shared" {
		t.Fatalf("B 应获得唯一可用的 Shared，实际为 %q", got)
	}
	if got := plan.Assignments["A"].Name; got != "AOnly" {
		t.Fatalf("A 应回退选择 AOnly，实际为 %q", got)
	}
}

// TestBuildRestoreAssignmentPlanPrefersKeepingTargetOperator 验证覆盖率相同时优先保留当前售卖干员。
func TestBuildRestoreAssignmentPlanPrefersKeepingTargetOperator(t *testing.T) {
	groups := []operatorCandidateGroup{
		{
			Location: "RefugeeCamp",
			Candidates: []operatorCandidate{
				{Name: "Antal", Priority: 0},
				{Name: "Laevatain", Priority: 1},
			},
		},
	}
	owned := operatorIDSet([]string{"Antal", "Laevatain"})
	preferred := map[string]operatorCandidate{
		"RefugeeCamp": {Name: "Laevatain"},
	}

	plan := buildRestoreAssignmentPlanWithPreferences(groups, owned, preferred)
	if got := plan.Assignments["RefugeeCamp"].Name; got != "Laevatain" {
		t.Fatalf("恢复干员 = %q，期望 Laevatain", got)
	}
	if plan.KeptTargets != 1 {
		t.Fatalf("保留售卖干员数 = %d，期望 1", plan.KeptTargets)
	}
}

// TestBuildRestoreAssignmentPlanDoesNotSacrificeCoverageToKeepTarget 验证保留售卖干员不能牺牲恢复覆盖率。
func TestBuildRestoreAssignmentPlanDoesNotSacrificeCoverageToKeepTarget(t *testing.T) {
	groups := []operatorCandidateGroup{
		{
			Location: "A",
			Candidates: []operatorCandidate{
				{Name: "Shared", Priority: 0},
				{Name: "AOnly", Priority: 1},
			},
		},
		{
			Location:   "B",
			Candidates: []operatorCandidate{{Name: "Shared", Priority: 0}},
		},
	}
	owned := operatorIDSet([]string{"Shared", "AOnly"})
	preferred := map[string]operatorCandidate{
		"A": {Name: "Shared"},
	}

	plan := buildRestoreAssignmentPlanWithPreferences(groups, owned, preferred)
	if plan.Assigned != 2 {
		t.Fatalf("已分配据点数 = %d，期望 2", plan.Assigned)
	}
	if got := plan.Assignments["B"].Name; got != "Shared" {
		t.Fatalf("B 的恢复干员 = %q，期望 Shared", got)
	}
	if got := plan.Assignments["A"].Name; got != "AOnly" {
		t.Fatalf("A 的恢复干员 = %q，期望 AOnly", got)
	}
}

// TestCandidatesForCurrentSelectionUsesGlobalRestorePlan 验证当前据点只使用全局恢复方案分配的干员。
func TestCandidatesForCurrentSelectionUsesGlobalRestorePlan(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "B",
		ActiveLocations: map[string]struct{}{
			"A": {},
			"B": {},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "A",
				Candidates: []operatorCandidate{
					{Name: "Shared", Priority: 0},
					{Name: "AOnly", Priority: 9},
				},
			},
			{
				Location: "B",
				Candidates: []operatorCandidate{
					{Name: "Shared", Priority: 0},
				},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Shared", "AOnly"}))
	if len(candidates) != 1 || candidates[0].Name != "Shared" {
		t.Fatalf("候选 = %#v，期望仅包含 Shared", candidates)
	}
}

// TestCandidatesForCurrentSelectionRejectsIncompleteRestorePlan 验证缺少全局恢复分组时不会回退到局部候选。
func TestCandidatesForCurrentSelectionRejectsIncompleteRestorePlan(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "A",
		Candidates: []operatorCandidate{
			{Name: "LocalOnly", Priority: 0},
		},
	}

	if got := candidatesForCurrentSelection(p, operatorIDSet([]string{"LocalOnly"})); got != nil {
		t.Fatalf("不完整的恢复方案不应回退到局部候选，实际为 %#v", got)
	}
}

// TestCandidatesForCurrentSelectionReturnsOnlyGlobalBestTarget 验证售卖岗位只返回全局最优干员。
func TestCandidatesForCurrentSelectionReturnsOnlyGlobalBestTarget(t *testing.T) {
	p := &operatorSelectionParam{
		Usage: operatorActionUsageTarget,
		Candidates: []operatorCandidate{
			{Name: "Best", Priority: 0, BonusTier: 0},
			{Name: "Fallback", Priority: 1, BonusTier: 1},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Best", "Fallback"}))
	if len(candidates) != 1 || candidates[0].Name != "Best" {
		t.Fatalf("候选 = %#v，期望仅包含 Best", candidates)
	}
}

// TestCandidatesForCurrentSelectionPrioritizesOutpostProsperity 验证据点发展值未满时，
// 单发展值加成优先于单交易收益加成。
func TestCandidatesForCurrentSelectionPrioritizesOutpostProsperity(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "Current",
		Candidates: []operatorCandidate{
			{Name: "Prosperity", Priority: 0, BonusTier: 1},
			{Name: "TradeProfit", Priority: 1, BonusTier: 2},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Prosperity", "TradeProfit"}))
	if len(candidates) != 1 || candidates[0].Name != "Prosperity" {
		t.Fatalf("发展值未满时的售卖候选 = %#v，期望优先 Prosperity", candidates)
	}
}

func TestEquivalentTargetCandidatesIncludeAllBestBonusOperators(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "XiranflowCloudseederStation",
		Candidates: []operatorCandidate{
			{Name: "Lifeng", Priority: 0, BonusTier: 0},
			{Name: "Arcane", Priority: 1, BonusTier: 0},
			{Name: "Ardelia", Priority: 2, BonusTier: 1},
		},
	}

	candidates := equivalentTargetCandidatesForOwnership(p, operatorOwnership{
		Operators: operatorIDSet([]string{"Lifeng", "Arcane", "Ardelia"}),
	})
	if len(candidates) != 2 || candidates[0].Name != "Lifeng" || candidates[1].Name != "Arcane" {
		t.Fatalf("同档候选 = %#v，期望 Lifeng、Arcane", candidates)
	}
}

// TestPreferredTargetCandidatesPrioritizesPerfectOperator 验证最高加成档只保留同时满足售卖与恢复的完美候选。
func TestPreferredTargetCandidatesPrioritizesPerfectOperator(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "Current",
		Candidates: []operatorCandidate{
			{Name: "SellOnly", Priority: 0, BonusTier: 0},
			{Name: "Perfect", Priority: 1, BonusTier: 0},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "Current",
				Candidates: []operatorCandidate{{Name: "Perfect"}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"SellOnly", "Perfect"}))
	if len(candidates) != 1 || candidates[0].Name != "Perfect" {
		t.Fatalf("售卖候选 = %#v，期望选择同时满足售卖与恢复的 Perfect", candidates)
	}
}

// TestPreferredTargetCandidatesFallsBackToBestSellingTier 验证没有最高加成档完美候选时回退到最高售卖档。
func TestPreferredTargetCandidatesFallsBackToBestSellingTier(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "Current",
		Candidates: []operatorCandidate{
			{Name: "SellOnly", Priority: 0, BonusTier: 0},
			{Name: "Perfect", Priority: 1, BonusTier: 0},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "Current",
				Candidates: []operatorCandidate{{Name: "Perfect"}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"SellOnly"}))
	if len(candidates) != 1 || candidates[0].Name != "SellOnly" {
		t.Fatalf("售卖候选 = %#v，期望在未拥有完美候选时回退到 SellOnly", candidates)
	}
}

// TestOutpostProsperityMaxTreatsMoneyAndProductionAsPerfect 验证据点发展值满级后不再要求发展值加成词条。
func TestOutpostProsperityMaxTreatsMoneyAndProductionAsPerfect(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "Current",
		Candidates: []operatorCandidate{
			{
				Name:                          "DevelopmentAndMoney",
				Priority:                      0,
				BonusTier:                     0,
				OutpostProsperityMaxBonusTier: 0,
			},
			{
				Name:                          "MoneyAndProduction",
				Priority:                      1,
				BonusTier:                     1,
				OutpostProsperityMaxBonusTier: 0,
			},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "Current",
				Candidates: []operatorCandidate{
					{Name: "MoneyAndProduction"},
				},
			},
		},
	}
	owned := operatorIDSet([]string{"DevelopmentAndMoney", "MoneyAndProduction"})

	notMax := candidatesForCurrentSelection(p, owned)
	if len(notMax) != 1 || notMax[0].Name != "DevelopmentAndMoney" {
		t.Fatalf("发展值未满时的售卖候选 = %#v，期望优先 DevelopmentAndMoney", notMax)
	}

	p.OutpostProsperityMaxLocations = map[string]struct{}{"Current": {}}
	max := candidatesForCurrentSelection(p, owned)
	if len(max) != 1 || max[0].Name != "MoneyAndProduction" {
		t.Fatalf("发展值满级时的售卖候选 = %#v，期望两词条完美候选 MoneyAndProduction", max)
	}
}

// TestCachedOutpostProsperityChangesGeneratedSellingOperator 验证任务启动时会从统一缓存加载据点发展值状态，
// 并直接影响真实生成数据中的售卖干员选择：满级忽略发展值加成，未满则优先发展值加成。
func TestCachedOutpostProsperityChangesGeneratedSellingOperator(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
	}

	location := "ReconstructionHQ"
	locationOrder := orderedEnabledLocations(t, data.LocationOrder, location)
	uid := currentSellProductCacheUID()
	for _, tt := range []struct {
		name                 string
		outpostProsperityMax bool
		want                 string
	}{
		{name: "满级选择阿列什", outpostProsperityMax: true, want: "Alesh"},
		{name: "未满选择莱万汀", outpostProsperityMax: false, want: "Laevatain"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := writeSellProductCache(
				resolveSellProductCachePathFunc(),
				sellProductCache{
					Accounts: map[string]sellProductCacheAccount{
						uid: {
							Operators: testOperatorSnapshot("Laevatain", "Alesh"),
							Locations: map[string]bool{location: tt.outpostProsperityMax},
						},
					},
				},
			); err != nil {
				t.Fatalf("准备 SellProduct 缓存失败：%v", err)
			}

			operatorSessionReset(operatorCacheModeCache)
			for _, activeLocation := range locationOrder {
				operatorSessionRegisterLocation(activeLocation)
			}
			_, cachedMax := operatorSessionSnapshot().OutpostProsperityMaxLocations[location]
			if cachedMax != tt.outpostProsperityMax {
				t.Fatalf("缓存加载后的据点满级状态 = %v，期望 %v", cachedMax, tt.outpostProsperityMax)
			}

			selection, err := resolveOperatorSelectionParam(&operatorActionParam{
				Usage:    operatorActionUsageTarget,
				Location: location,
			})
			if err != nil {
				t.Fatalf("解析重建指挥部售卖参数失败：%v", err)
			}
			ownership, err := loadOperatorOwnershipForSelection()
			if err != nil {
				t.Fatalf("加载 SellProduct 干员缓存失败：%v", err)
			}
			candidates := candidatesForOwnership(selection, ownership)
			if len(candidates) != 1 || candidates[0].Name != tt.want {
				t.Fatalf("重建指挥部售卖干员 = %#v，期望 %s", candidates, tt.want)
			}
		})
	}
}

// TestCachedProsperityPlanningFollowsGameOrderAndPrioritizesPerfectMatches 验证缓存规划按游戏顺序执行：
// 后续据点有完整替代时优先让莱万汀完美适配难民暂居处；缺少替代时才将她留给重建指挥部。
func TestCachedProsperityPlanningFollowsGameOrderAndPrioritizesPerfectMatches(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
	}

	locationOrder := orderedEnabledLocations(t, data.LocationOrder, "ReconstructionHQ", "RefugeeCamp")
	expectedLocationOrder := []string{"RefugeeCamp", "ReconstructionHQ"}
	if !slices.Equal(locationOrder, expectedLocationOrder) {
		t.Fatalf("据点售卖顺序 = %#v，期望游戏顺序 %#v", locationOrder, expectedLocationOrder)
	}

	uid := currentSellProductCacheUID()
	for _, tt := range []struct {
		name                 string
		operators            []string
		expected             map[string]string
		keepCurrentLaevatain bool
	}{
		{
			name:      "后续据点已有完美替代",
			operators: []string{"Camille", "Laevatain", "Gilberta"},
			expected: map[string]string{
				"RefugeeCamp":      "Laevatain",
				"ReconstructionHQ": "Gilberta",
			},
		},
		{
			name:                 "后续据点缺少等价替代",
			operators:            []string{"Camille", "Laevatain", "Alesh"},
			keepCurrentLaevatain: true,
			expected: map[string]string{
				"RefugeeCamp":      "Camille",
				"ReconstructionHQ": "Laevatain",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := writeSellProductCache(
				resolveSellProductCachePathFunc(),
				sellProductCache{
					Accounts: map[string]sellProductCacheAccount{
						uid: {
							Operators: testOperatorSnapshot(tt.operators...),
							Locations: map[string]bool{
								"RefugeeCamp":      true,
								"ReconstructionHQ": false,
							},
						},
					},
				},
			); err != nil {
				t.Fatalf("准备 SellProduct 缓存失败：%v", err)
			}

			operatorSessionReset(operatorCacheModeCache)
			for _, location := range locationOrder {
				operatorSessionRegisterLocation(location)
			}
			ownership, err := loadOperatorOwnershipForSelection()
			if err != nil {
				t.Fatalf("加载 SellProduct 干员缓存失败：%v", err)
			}

			for _, location := range locationOrder {
				targetSelection, err := resolveOperatorSelectionParam(&operatorActionParam{
					Usage:    operatorActionUsageTarget,
					Location: location,
				})
				if err != nil {
					t.Fatalf("解析 %s 售卖参数失败：%v", location, err)
				}
				target := candidatesForOwnership(targetSelection, ownership)
				if len(target) != 1 || target[0].Name != tt.expected[location] {
					t.Fatalf("%s 售卖干员 = %#v，期望 %s", location, target, tt.expected[location])
				}

				if location == "ReconstructionHQ" && tt.keepCurrentLaevatain {
					equivalent := equivalentTargetCandidatesForOwnership(targetSelection, ownership)
					current, _, ok := findCurrentBestOperator(
						equivalent,
						targetSelection.KnownOperators,
						[]ocrItem{{text: "莱万汀派驻效果"}},
					)
					if !ok || current.Name != "Laevatain" {
						t.Fatalf("重建指挥部当前干员识别 = %#v, %v，期望直接沿用莱万汀", current, ok)
					}
				}

				operatorSessionSetTargetAssignment(location, target[0])
				restoreSelection, err := resolveOperatorSelectionParam(&operatorActionParam{
					Usage:    operatorActionUsageRestore,
					Location: location,
				})
				if err != nil {
					t.Fatalf("解析 %s 售后生产派驻参数失败：%v", location, err)
				}
				restore := candidatesForOwnership(restoreSelection, ownership)
				if len(restore) != 1 || restore[0].Name != tt.expected[location] {
					t.Fatalf("%s 售后生产派驻干员 = %#v，期望沿用 %s", location, restore, tt.expected[location])
				}
				operatorSessionSetPlannedRestore(location, restore[0], true)
				if _, ok := operatorSessionCompleteRestore(location); !ok {
					t.Fatalf("%s 售后生产派驻结果无法锁定", location)
				}
			}
		})
	}
}

// TestObservedOutpostProsperityChangeReplansFutureAssignments 验证任务开始时使用缓存的满级状态，
// 进入据点确认开放新等级后，会为未满据点重新预留发展值干员。
func TestObservedOutpostProsperityChangeReplansFutureAssignments(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
	}
	uid := currentSellProductCacheUID()
	currentLocation := "InfraStation"
	futureLocation := "ReconstructionHQ"
	if err := writeSellProductCache(
		resolveSellProductCachePathFunc(),
		sellProductCache{
			Accounts: map[string]sellProductCacheAccount{
				uid: {Locations: map[string]bool{futureLocation: true}},
			},
		},
	); err != nil {
		t.Fatalf("准备据点发展值缓存失败：%v", err)
	}
	operatorSessionReset(operatorCacheModeCache)
	for _, location := range orderedEnabledLocations(t, data.LocationOrder, currentLocation, futureLocation) {
		operatorSessionRegisterLocation(location)
	}

	selection := func() *operatorSelectionParam {
		session := operatorSessionSnapshot()
		return &operatorSelectionParam{
			Usage:           operatorActionUsageRestore,
			Location:        currentLocation,
			ActiveLocations: session.ActiveLocations,
			TargetCandidatesByLocation: map[string][]operatorCandidate{
				futureLocation: {
					{
						Name:                          "Development",
						Priority:                      1,
						BonusTier:                     1,
						OutpostProsperityMaxBonusTier: 1,
					},
					{
						Name:                          "TradeProfit",
						Priority:                      0,
						BonusTier:                     2,
						OutpostProsperityMaxBonusTier: 0,
					},
				},
			},
			RestoreGroups: []operatorCandidateGroup{
				{
					Location: currentLocation,
					Candidates: []operatorCandidate{
						{Name: "TradeProfit", Priority: 0},
						{Name: "CurrentOnly", Priority: 1},
					},
				},
				{
					Location: futureLocation,
					Candidates: []operatorCandidate{
						{Name: "TradeProfit", Priority: 0},
						{Name: "Development", Priority: 1},
					},
				},
			},
			OutpostProsperityMaxLocations: session.OutpostProsperityMaxLocations,
		}
	}
	owned := operatorIDSet([]string{"Development", "TradeProfit", "CurrentOnly"})

	initial := candidatesForCurrentSelection(selection(), owned)
	if len(initial) != 1 || initial[0].Name != "CurrentOnly" {
		t.Fatalf("缓存满级状态下的当前据点生产分配 = %#v，期望为未来据点保留 TradeProfit", initial)
	}

	operatorSessionSetOutpostProsperityMax(futureLocation, false)
	replanned := candidatesForCurrentSelection(selection(), owned)
	if len(replanned) != 1 || replanned[0].Name != "TradeProfit" {
		t.Fatalf("确认未满后的当前据点生产分配 = %#v，期望为未来据点保留 Development", replanned)
	}
}

func TestTargetSelectionMinimizesGlobalOperatorChangesWithinBestBonusTier(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "XiranflowCloudseederStation",
		ActiveLocations: map[string]struct{}{
			"Other":                       {},
			"XiranflowCloudseederStation": {},
		},
		Candidates: []operatorCandidate{
			{Name: "Lifeng", Priority: 0, BonusTier: 0},
			{Name: "Arcane", Priority: 1, BonusTier: 0},
		},
		TargetAssignments: map[string]operatorCandidate{
			"Other": {Name: "OtherKeeper"},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "Other",
				Candidates: []operatorCandidate{
					{Name: "OtherKeeper", Priority: 0},
				},
			},
			{
				Location: "XiranflowCloudseederStation",
				Candidates: []operatorCandidate{
					{Name: "ChenQianyu", Priority: 0},
					{Name: "Arcane", Priority: 5},
				},
			},
		},
	}
	owned := operatorIDSet([]string{"Lifeng", "Arcane", "ChenQianyu", "OtherKeeper"})

	candidates := candidatesForCurrentSelection(p, owned)
	if len(candidates) != 1 || candidates[0].Name != "Arcane" {
		t.Fatalf("售卖候选 = %#v，期望选择可被全局恢复方案沿用的 Arcane", candidates)
	}

	selection := *p
	selection.Usage = operatorActionUsageRestore
	selection.TargetAssignments = cloneRestoreAssignments(p.TargetAssignments)
	selection.TargetAssignments[p.Location] = candidates[0]
	restore := candidatesForCurrentSelection(&selection, owned)
	if len(restore) != 1 || restore[0].Name != "Arcane" {
		t.Fatalf("恢复候选 = %#v，期望继续沿用 Arcane", restore)
	}
}

func TestGeneratedXiranflowPlanKeepsArcaneForSellingAndRestore(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	location := "XiranflowCloudseederStation"
	operatorSessionRegisterLocation(location)

	targetSelection, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageTarget,
		Location: location,
	})
	if err != nil {
		t.Fatalf("解析盈天台售卖参数失败：%v", err)
	}
	owned := operatorIDSet([]string{"Lifeng", "Arcane", "ChenQianyu"})

	target := candidatesForCurrentSelection(targetSelection, owned)
	if len(target) != 1 || target[0].Name != "Arcane" {
		t.Fatalf("盈天台售卖干员 = %#v，期望 Arcane", target)
	}

	restoreSelection, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageRestore,
		Location: location,
	})
	if err != nil {
		t.Fatalf("解析盈天台恢复参数失败：%v", err)
	}
	restoreSelection.TargetAssignments[location] = target[0]
	restore := candidatesForCurrentSelection(restoreSelection, owned)
	if len(restore) != 1 || restore[0].Name != "Arcane" {
		t.Fatalf("盈天台恢复干员 = %#v，期望继续沿用 Arcane", restore)
	}
}

func orderedEnabledLocations(t *testing.T, gameOrder []string, enabled ...string) []string {
	t.Helper()
	enabledSet := make(map[string]struct{}, len(enabled))
	for _, location := range enabled {
		if _, exists := enabledSet[location]; exists {
			t.Fatalf("测试重复启用了据点 %q", location)
		}
		enabledSet[location] = struct{}{}
	}

	ordered := make([]string, 0, len(enabled))
	for _, location := range gameOrder {
		if _, ok := enabledSet[location]; !ok {
			continue
		}
		ordered = append(ordered, location)
		delete(enabledSet, location)
	}
	if len(enabledSet) != 0 {
		missing := make([]string, 0, len(enabledSet))
		for _, location := range enabled {
			if _, ok := enabledSet[location]; ok {
				missing = append(missing, location)
			}
		}
		t.Fatalf("启用据点不在游戏 location_order 中：%#v", missing)
	}
	return ordered
}

// TestPlanningMatches20260719LogSnapshot 使用 MaaEnd-logs-v0.1.0-20260719-094957
// 中的完整拥有干员快照，验证六据点固定顺序下的售卖与恢复规划。
func TestPlanningMatches20260719LogSnapshot(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
	}

	expectedLocationOrder := []string{
		"RefugeeCamp",
		"InfraStation",
		"ReconstructionHQ",
		"SkyKingFlatsConstructionSite",
		"CardiacRemediationStation",
		"XiranflowCloudseederStation",
	}
	locationOrder := orderedEnabledLocations(t, data.LocationOrder, expectedLocationOrder...)
	if !slices.Equal(locationOrder, expectedLocationOrder) {
		t.Fatalf("据点售卖顺序 = %#v，期望游戏顺序 %#v", locationOrder, expectedLocationOrder)
	}
	for _, location := range locationOrder {
		operatorSessionRegisterLocation(location)
	}

	// 来源：merged/record/SellProductCache.json，更新时间 2026-07-19T01:29:13Z。
	owned := operatorIDSet([]string{
		"Yvonne",
		"Ember",
		"Perlica",
		"LastRite",
		"Catcher",
		"Camille",
		"Estella",
		"DaPan",
		"Antal",
		"ZhuangFangyi",
		"Arclight",
		"MiFu",
		"Snowshine",
		"Tangtang",
		"Gilberta",
		"Rossi",
		"Wulfgard",
		"Akekuri",
		"Ardelia",
		"Avywenna",
		"Laevatain",
		"Fluorite",
		"Arcane",
		"Xaihi",
		"Alesh",
		"ChenQianyu",
		"Pogranichnik",
		"Lifeng",
	})
	expected := map[string]string{
		"RefugeeCamp":                  "Laevatain",
		"InfraStation":                 "Yvonne",
		"ReconstructionHQ":             "Gilberta",
		"SkyKingFlatsConstructionSite": "Tangtang",
		"CardiacRemediationStation":    "ZhuangFangyi",
		"XiranflowCloudseederStation":  "Arcane",
	}

	for _, location := range locationOrder {
		targetSelection, resolveErr := resolveOperatorSelectionParam(&operatorActionParam{
			Usage:    operatorActionUsageTarget,
			Location: location,
		})
		if resolveErr != nil {
			t.Fatalf("解析 %s 售卖参数失败：%v", location, resolveErr)
		}
		target := candidatesForCurrentSelection(targetSelection, owned)
		if len(target) != 1 || target[0].Name != expected[location] {
			t.Fatalf("%s 售卖规划 = %#v，期望 %s", location, target, expected[location])
		}
		operatorSessionSetTargetAssignment(location, target[0])

		restoreSelection, resolveErr := resolveOperatorSelectionParam(&operatorActionParam{
			Usage:    operatorActionUsageRestore,
			Location: location,
		})
		if resolveErr != nil {
			t.Fatalf("解析 %s 恢复参数失败：%v", location, resolveErr)
		}
		restore := candidatesForCurrentSelection(restoreSelection, owned)
		if len(restore) != 1 || restore[0].Name != expected[location] {
			t.Fatalf("%s 恢复规划 = %#v，期望沿用 %s", location, restore, expected[location])
		}
		operatorSessionSetPlannedRestore(location, restore[0], true)
		if _, ok := operatorSessionCompleteRestore(location); !ok {
			t.Fatalf("%s 恢复规划无法锁定", location)
		}
	}

	active := operatorSessionSnapshot().ActiveLocations
	if !operatorConflictSourceManaged("ReconstructionHQ", true, active) {
		t.Fatal("日志中的重建指挥部已启用，莱万汀冲突应允许调至难民暂居处")
	}
}

// TestPlanningMatches20260720ProsperityMaxLogSnapshot 使用 install/debug 最近一次运行的
// SellProductCache.json 与 10:57 运行日志，验证满级据点优先沿用无需切换的完美匹配干员。
func TestPlanningMatches20260720ProsperityMaxLogSnapshot(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
	}
	expectedLocationOrder := []string{
		"SkyKingFlatsConstructionSite",
		"CardiacRemediationStation",
	}
	locationOrder := orderedEnabledLocations(t, data.LocationOrder, expectedLocationOrder...)
	if !slices.Equal(locationOrder, expectedLocationOrder) {
		t.Fatalf("据点售卖顺序 = %#v，期望游戏顺序 %#v", locationOrder, expectedLocationOrder)
	}
	for _, location := range locationOrder {
		operatorSessionRegisterLocation(location)
		operatorSessionSetOutpostProsperityMax(location, true)
	}

	// 来源：install/debug/record/SellProductCache.json，更新时间 2026-07-20T02:56:13Z。
	owned := operatorIDSet([]string{
		"Ember",
		"Perlica",
		"LastRite",
		"Catcher",
		"Camille",
		"Estella",
		"DaPan",
		"Antal",
		"Arclight",
		"Snowshine",
		"Rossi",
		"Wulfgard",
		"Akekuri",
		"Ardelia",
		"Avywenna",
		"Laevatain",
		"Fluorite",
		"Xaihi",
		"Alesh",
		"ChenQianyu",
		"Pogranichnik",
		"Lifeng",
	})
	expected := map[string]string{
		"SkyKingFlatsConstructionSite": "Wulfgard",
		"CardiacRemediationStation":    "Akekuri",
	}
	currentOCR := map[string]string{
		"SkyKingFlatsConstructionSite": "狼卫派驻效果",
		"CardiacRemediationStation":    "秋栗派驻效果",
	}

	for _, location := range locationOrder {
		targetSelection, err := resolveOperatorSelectionParam(&operatorActionParam{
			Usage:    operatorActionUsageTarget,
			Location: location,
		})
		if err != nil {
			t.Fatalf("解析 %s 售卖参数失败：%v", location, err)
		}
		equivalent := equivalentTargetCandidatesForOwnership(targetSelection, operatorOwnership{
			Operators: owned,
		})
		if len(equivalent) != 1 || equivalent[0].Name != expected[location] {
			t.Fatalf("%s 可沿用售卖候选 = %#v，期望仅包含 %s", location, equivalent, expected[location])
		}
		current, _, ok := findCurrentBestOperator(
			equivalent,
			targetSelection.KnownOperators,
			[]ocrItem{{text: currentOCR[location]}},
		)
		if !ok || current.Name != expected[location] {
			t.Fatalf("%s 当前干员识别 = %#v, %v，期望直接沿用 %s", location, current, ok, expected[location])
		}

		target := candidatesForCurrentSelection(targetSelection, owned)
		if len(target) != 1 || target[0].Name != expected[location] {
			t.Fatalf("%s 售卖规划 = %#v，期望沿用 %s", location, target, expected[location])
		}
		operatorSessionSetTargetAssignment(location, target[0])

		restoreSelection, err := resolveOperatorSelectionParam(&operatorActionParam{
			Usage:    operatorActionUsageRestore,
			Location: location,
		})
		if err != nil {
			t.Fatalf("解析 %s 售后生产派驻参数失败：%v", location, err)
		}
		restore := candidatesForCurrentSelection(restoreSelection, owned)
		if len(restore) != 1 || restore[0].Name != expected[location] {
			t.Fatalf("%s 售后生产派驻规划 = %#v，期望沿用 %s", location, restore, expected[location])
		}
		operatorSessionSetPlannedRestore(location, restore[0], true)
		if _, ok := operatorSessionCompleteRestore(location); !ok {
			t.Fatalf("%s 售后生产派驻结果无法锁定", location)
		}
	}
}

func TestGeneratedXiranflowRestorePreparesArcaneForNextRunAfterKeepingLifeng(t *testing.T) {
	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
	}
	location := "XiranflowCloudseederStation"
	targetCandidates := data.TargetCandidates[location]
	var lifeng operatorCandidate
	for _, candidate := range targetCandidates {
		if candidate.Name == "Lifeng" {
			lifeng = candidate
			break
		}
	}
	if lifeng.Name == "" {
		t.Fatal("盈天台售卖候选中缺少 Lifeng")
	}
	p := &operatorSelectionParam{
		Usage:                      operatorActionUsageRestore,
		Location:                   location,
		Candidates:                 targetCandidates,
		TargetCandidatesByLocation: data.TargetCandidates,
		RestoreGroups:              data.RestoreGroups,
		ActiveLocations: map[string]struct{}{
			location: {},
		},
		TargetAssignments: map[string]operatorCandidate{
			location: lifeng,
		},
	}
	ownedNames := []string{"Lifeng", "Arcane", "ChenQianyu"}
	owned := operatorIDSet(ownedNames)

	restore := candidatesForCurrentSelection(p, owned)
	if len(restore) != 1 || restore[0].Name != "Arcane" {
		t.Fatalf("黎风售卖后的盈天台恢复干员 = %#v，期望 Arcane", restore)
	}

	p.Usage = operatorActionUsageTarget
	p.TargetAssignments = nil
	nextRunCandidates := equivalentTargetCandidatesForOwnership(p, operatorOwnership{
		Operators: operatorIDSet(ownedNames),
	})
	stable := false
	for _, candidate := range nextRunCandidates {
		if sameOperator(candidate, restore[0]) {
			stable = true
			break
		}
	}
	if !stable {
		t.Fatalf("恢复干员 %q 不能在下次任务直接用于最高档售卖", restore[0].Name)
	}
}

// TestCandidatesForCurrentSelectionIgnoresInactiveRestoreLocations 验证未启用据点不会占用恢复干员。
func TestCandidatesForCurrentSelectionIgnoresInactiveRestoreLocations(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "Active",
		ActiveLocations: map[string]struct{}{
			"Active": {},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "Active",
				Candidates: []operatorCandidate{{Name: "Shared", Priority: 0}},
			},
			{
				Location:   "Inactive",
				Candidates: []operatorCandidate{{Name: "Shared", Priority: 0}},
			},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Shared"}))
	if len(candidates) != 1 || candidates[0].Name != "Shared" {
		t.Fatalf("候选 = %#v，启用据点应获得 Shared", candidates)
	}
}

// TestCandidatesForCurrentSelectionKeepsLockedRestoreAssignments 验证已完成据点锁定的恢复干员不会被重新分配。
func TestCandidatesForCurrentSelectionKeepsLockedRestoreAssignments(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "Pending",
		ActiveLocations: map[string]struct{}{
			"Done":    {},
			"Pending": {},
		},
		LockedRestoreAssignments: map[string]operatorCandidate{
			"Done": {Name: "Shared", Priority: 0},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "Done",
				Candidates: []operatorCandidate{
					{Name: "Shared", Priority: 0},
				},
			},
			{
				Location: "Pending",
				Candidates: []operatorCandidate{
					{Name: "Shared", Priority: 0},
					{Name: "PendingOnly", Priority: 1},
				},
			},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Shared", "PendingOnly"}))
	if len(candidates) != 1 || candidates[0].Name != "PendingOnly" {
		t.Fatalf("候选 = %#v，期望 PendingOnly", candidates)
	}
}

// TestTargetSelectionProtectsOperatorLockedToCompletedLocation 验证后续据点不会挪用已完成恢复的干员。
func TestTargetSelectionProtectsOperatorLockedToCompletedLocation(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "ReconstructionHQ",
		ActiveLocations: map[string]struct{}{
			"CardiacRemediationStation": {},
			"ReconstructionHQ":          {},
		},
		Candidates: []operatorCandidate{
			{Name: "Pogranichnik", Priority: 0},
			{Name: "Fallback", Priority: 1},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "ReconstructionHQ",
				Candidates: []operatorCandidate{{Name: "Pogranichnik"}},
			},
		},
		LockedRestoreAssignments: map[string]operatorCandidate{
			"CardiacRemediationStation": {Name: "Pogranichnik"},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Pogranichnik", "Fallback"}))
	if len(candidates) != 1 || candidates[0].Name != "Fallback" {
		t.Fatalf("候选 = %#v，期望保护已恢复干员并改选 Fallback", candidates)
	}
}

// TestTargetSelectionMayMoveOperatorFromUnprocessedEnabledLocation 验证未完成恢复的启用据点仍允许按固定顺序临时调人。
func TestTargetSelectionMayMoveOperatorFromUnprocessedEnabledLocation(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "ReconstructionHQ",
		ActiveLocations: map[string]struct{}{
			"CardiacRemediationStation": {},
			"ReconstructionHQ":          {},
		},
		Candidates: []operatorCandidate{
			{Name: "Pogranichnik", Priority: 0},
			{Name: "Fallback", Priority: 1},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "ReconstructionHQ",
				Candidates: []operatorCandidate{{Name: "Pogranichnik"}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Pogranichnik", "Fallback"}))
	if len(candidates) != 1 || candidates[0].Name != "Pogranichnik" {
		t.Fatalf("候选 = %#v，期望调入尚未完成恢复据点的 Pogranichnik", candidates)
	}
}

// TestReconstructionReplanProtectsInfraRestoreFrom20260719NoonLog 复现 12:00 日志：
// 基建前站已经恢复陈千语，重建指挥部排除被未启用据点占用的骏卫后，
// 按发展值优先规则改选莱万汀，同时不能挪用陈千语。
func TestReconstructionReplanProtectsInfraRestoreFrom20260719NoonLog(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
	}
	for _, location := range orderedEnabledLocations(t, data.LocationOrder, "InfraStation", "ReconstructionHQ") {
		operatorSessionRegisterLocation(location)
	}
	candidateByName := func(name string) operatorCandidate {
		for _, candidate := range data.KnownOperators {
			if candidate.Name == name {
				return candidate
			}
		}
		t.Fatalf("干员数据中缺少 %s", name)
		return operatorCandidate{}
	}

	chenQianyu := candidateByName("ChenQianyu")
	operatorSessionSetPlannedRestore("InfraStation", chenQianyu, true)
	if restored, ok := operatorSessionCompleteRestore("InfraStation"); !ok || restored.Name != "ChenQianyu" {
		t.Fatalf("基建前站恢复结果 = %#v, %v，期望陈千语", restored, ok)
	}

	owned := operatorIDSet([]string{
		"Ember", "Perlica", "LastRite", "Catcher", "Camille", "Estella", "DaPan", "Antal", "Arclight", "Snowshine",
		"Rossi", "Wulfgard", "Akekuri", "Ardelia", "Avywenna", "Laevatain", "Fluorite", "Xaihi", "Alesh", "ChenQianyu",
		"Pogranichnik", "Lifeng",
	})
	selection, err := resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageTarget,
		Location: "ReconstructionHQ",
	})
	if err != nil {
		t.Fatalf("解析重建指挥部售卖参数失败：%v", err)
	}
	initial := candidatesForCurrentSelection(selection, owned)
	if len(initial) != 1 || initial[0].Name != "Pogranichnik" {
		t.Fatalf("重建指挥部初始售卖候选 = %#v，期望骏卫", initial)
	}

	operatorSessionSetTargetAssignment("ReconstructionHQ", initial[0])
	if excluded, ok := operatorSessionExcludeSelected(operatorActionUsageTarget, "ReconstructionHQ"); !ok || excluded.Name != "Pogranichnik" {
		t.Fatalf("重建指挥部排除结果 = %#v, %v，期望骏卫", excluded, ok)
	}

	selection, err = resolveOperatorSelectionParam(&operatorActionParam{
		Usage:    operatorActionUsageTarget,
		Location: "ReconstructionHQ",
	})
	if err != nil {
		t.Fatalf("重新解析重建指挥部售卖参数失败：%v", err)
	}
	replanned := candidatesForCurrentSelection(selection, owned)
	if len(replanned) != 1 || replanned[0].Name != "Laevatain" {
		t.Fatalf("重建指挥部重新规划 = %#v，期望避开陈千语并按发展值优先改选莱万汀", replanned)
	}
	if sameOperator(replanned[0], chenQianyu) {
		t.Fatal("重建指挥部不应挪用基建前站已经恢复的陈千语")
	}
}

// TestTargetSelectionMayKeepOperatorLockedToCurrentLocation 验证当前据点可以继续使用自己锁定的恢复干员。
func TestTargetSelectionMayKeepOperatorLockedToCurrentLocation(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "CardiacRemediationStation",
		Candidates: []operatorCandidate{
			{Name: "Pogranichnik", Priority: 0},
		},
		LockedRestoreAssignments: map[string]operatorCandidate{
			"CardiacRemediationStation": {Name: "Pogranichnik"},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Pogranichnik"}))
	if len(candidates) != 1 || candidates[0].Name != "Pogranichnik" {
		t.Fatalf("候选 = %#v，期望当前据点已锁定的干员", candidates)
	}
}

// TestOperatorSessionTargetAssignmentClearedAfterRestore 验证恢复完成后清除售卖分配并锁定恢复结果。
func TestOperatorSessionTargetAssignmentClearedAfterRestore(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	target := operatorCandidate{Name: "Laevatain"}
	operatorSessionSetTargetAssignment("RefugeeCamp", target)
	operatorSessionSetPlannedRestore("RefugeeCamp", target, true)

	before := operatorSessionSnapshot()
	if got := before.TargetAssignments["RefugeeCamp"].Name; got != "Laevatain" {
		t.Fatalf("售卖分配 = %q，期望 Laevatain", got)
	}
	if _, ok := operatorSessionCompleteRestore("RefugeeCamp"); !ok {
		t.Fatal("恢复完成操作应成功")
	}
	after := operatorSessionSnapshot()
	if _, exists := after.TargetAssignments["RefugeeCamp"]; exists {
		t.Fatal("恢复完成后应清除售卖分配")
	}
	if got := after.LockedRestoreAssignments["RefugeeCamp"].Name; got != "Laevatain" {
		t.Fatalf("锁定的恢复分配 = %q，期望 Laevatain", got)
	}
}

// TestCandidatesForCurrentSelectionIgnoresSkippedRestoreLocations 验证已跳过的恢复据点不会继续占用干员。
func TestCandidatesForCurrentSelectionIgnoresSkippedRestoreLocations(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageRestore,
		Location: "Pending",
		ActiveLocations: map[string]struct{}{
			"Skipped": {},
			"Pending": {},
		},
		CompletedRestoreLocations: map[string]struct{}{
			"Skipped": {},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "Skipped",
				Candidates: []operatorCandidate{{Name: "Shared", Priority: 0}},
			},
			{
				Location:   "Pending",
				Candidates: []operatorCandidate{{Name: "Shared", Priority: 0}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorIDSet([]string{"Shared"}))
	if len(candidates) != 1 || candidates[0].Name != "Shared" {
		t.Fatalf("候选 = %#v，待处理据点应获得 Shared", candidates)
	}
}
