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
			{Name: "Best", CacheName: "最优", Priority: 0},
			{Name: "Fallback", CacheName: "备选", Priority: 1},
		},
		ExcludedOperators: map[string]struct{}{
			"最优": {},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"最优", "备选"}))
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
					{Name: "Best", CacheName: "最优", Priority: 0},
					{Name: "Fallback", CacheName: "备选", Priority: 1},
				},
			},
		},
		ExcludedOperators: map[string]struct{}{
			"最优": {},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"最优", "备选"}))
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
				{Name: "Shared", CacheName: "Shared", Priority: 0},
				{Name: "AOnly", CacheName: "AOnly", Priority: 1},
			},
		},
		{
			Location: "B",
			Candidates: []operatorCandidate{
				{Name: "Shared", CacheName: "Shared", Priority: 0},
				{Name: "BOnly", CacheName: "BOnly", Priority: 1},
			},
		},
	}
	owned := operatorNameSet([]string{"Shared", "AOnly", "BOnly"})

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
				{Name: "Shared", CacheName: "Shared", Priority: 0},
				{Name: "AOnly", CacheName: "AOnly", Priority: 9},
			},
		},
		{
			Location: "B",
			Candidates: []operatorCandidate{
				{Name: "Shared", CacheName: "Shared", Priority: 0},
			},
		},
	}
	owned := operatorNameSet([]string{"Shared", "AOnly"})

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
				{Name: "Antal", CacheName: "安塔尔", Priority: 0},
				{Name: "Laevatain", CacheName: "莱万汀", Priority: 1},
			},
		},
	}
	owned := operatorNameSet([]string{"安塔尔", "莱万汀"})
	preferred := map[string]operatorCandidate{
		"RefugeeCamp": {Name: "Laevatain", CacheName: "莱万汀"},
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
				{Name: "Shared", CacheName: "共享", Priority: 0},
				{Name: "AOnly", CacheName: "甲专属", Priority: 1},
			},
		},
		{
			Location:   "B",
			Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
		},
	}
	owned := operatorNameSet([]string{"共享", "甲专属"})
	preferred := map[string]operatorCandidate{
		"A": {Name: "Shared", CacheName: "共享"},
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
					{Name: "Shared", CacheName: "Shared", Priority: 0},
					{Name: "AOnly", CacheName: "AOnly", Priority: 9},
				},
			},
			{
				Location: "B",
				Candidates: []operatorCandidate{
					{Name: "Shared", CacheName: "Shared", Priority: 0},
				},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"Shared", "AOnly"}))
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

	if got := candidatesForCurrentSelection(p, operatorNameSet([]string{"LocalOnly"})); got != nil {
		t.Fatalf("不完整的恢复方案不应回退到局部候选，实际为 %#v", got)
	}
}

// TestCandidatesForCurrentSelectionReturnsOnlyGlobalBestTarget 验证售卖岗位只返回全局最优干员。
func TestCandidatesForCurrentSelectionReturnsOnlyGlobalBestTarget(t *testing.T) {
	p := &operatorSelectionParam{
		Usage: operatorActionUsageTarget,
		Candidates: []operatorCandidate{
			{Name: "Best", CacheName: "最优", Priority: 0, BonusTier: 0},
			{Name: "Fallback", CacheName: "备选", Priority: 1, BonusTier: 1},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"最优", "备选"}))
	if len(candidates) != 1 || candidates[0].Name != "Best" {
		t.Fatalf("候选 = %#v，期望仅包含 Best", candidates)
	}
}

func TestEquivalentTargetCandidatesIncludeAllBestBonusOperators(t *testing.T) {
	p := &operatorSelectionParam{
		Usage:    operatorActionUsageTarget,
		Location: "XiranflowCloudseederStation",
		Candidates: []operatorCandidate{
			{Name: "Lifeng", CacheName: "黎风", Priority: 0, BonusTier: 0},
			{Name: "Arcane", CacheName: "诀", Priority: 1, BonusTier: 0},
			{Name: "Ardelia", CacheName: "艾尔黛拉", Priority: 2, BonusTier: 1},
		},
	}

	candidates := equivalentTargetCandidatesForOwnership(p, operatorOwnership{
		Operators: operatorNameSet([]string{"黎风", "诀", "艾尔黛拉"}),
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
			{Name: "SellOnly", CacheName: "仅售卖", Priority: 0, BonusTier: 0},
			{Name: "Perfect", CacheName: "完美", Priority: 1, BonusTier: 0},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "Current",
				Candidates: []operatorCandidate{{Name: "Perfect", CacheName: "完美"}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"仅售卖", "完美"}))
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
			{Name: "SellOnly", CacheName: "仅售卖", Priority: 0, BonusTier: 0},
			{Name: "Perfect", CacheName: "完美", Priority: 1, BonusTier: 0},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "Current",
				Candidates: []operatorCandidate{{Name: "Perfect", CacheName: "完美"}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"仅售卖"}))
	if len(candidates) != 1 || candidates[0].Name != "SellOnly" {
		t.Fatalf("售卖候选 = %#v，期望在未拥有完美候选时回退到 SellOnly", candidates)
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
			{Name: "Lifeng", CacheName: "黎风", Priority: 0, BonusTier: 0},
			{Name: "Arcane", CacheName: "诀", Priority: 1, BonusTier: 0},
		},
		TargetAssignments: map[string]operatorCandidate{
			"Other": {Name: "OtherKeeper", CacheName: "其他驻员"},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "Other",
				Candidates: []operatorCandidate{
					{Name: "OtherKeeper", CacheName: "其他驻员", Priority: 0},
				},
			},
			{
				Location: "XiranflowCloudseederStation",
				Candidates: []operatorCandidate{
					{Name: "ChenQianyu", CacheName: "陈千语", Priority: 0},
					{Name: "Arcane", CacheName: "诀", Priority: 5},
				},
			},
		},
	}
	owned := operatorNameSet([]string{"黎风", "诀", "陈千语", "其他驻员"})

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
	owned := operatorNameSet([]string{"黎风", "诀", "陈千语"})

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

// TestPlanningMatches20260719LogSnapshot 使用 MaaEnd-logs-v0.1.0-20260719-094957
// 中的完整拥有干员快照，验证六据点固定顺序下的售卖与恢复规划。
func TestPlanningMatches20260719LogSnapshot(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
	}

	locationOrder := []string{
		"RefugeeCamp",
		"InfraStation",
		"ReconstructionHQ",
		"SkyKingFlatsConstructionSite",
		"CardiacRemediationStation",
		"XiranflowCloudseederStation",
	}
	if !slices.Equal(data.LocationOrder, locationOrder) {
		t.Fatalf("据点顺序 = %#v，期望 %#v", data.LocationOrder, locationOrder)
	}
	for _, location := range locationOrder {
		operatorSessionRegisterLocation(location)
	}

	// 来源：merged/record/SellProductOwnedOperators.json，更新时间 2026-07-19T01:29:13Z。
	owned := operatorNameSet([]string{
		"伊冯",
		"余烬",
		"佩丽卡",
		"别礼",
		"卡契尔",
		"卡缪",
		"埃特拉",
		"大潘",
		"安塔尔",
		"庄方宜",
		"弧光",
		"弭弗",
		"昼雪",
		"汤汤",
		"洁尔佩塔",
		"洛茜",
		"狼卫",
		"秋栗",
		"艾尔黛拉",
		"艾维文娜",
		"莱万汀",
		"萤石",
		"诀",
		"赛希",
		"阿列什",
		"陈千语",
		"骏卫",
		"黎风",
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
	ownedNames := []string{"黎风", "诀", "陈千语"}
	owned := operatorNameSet(ownedNames)

	restore := candidatesForCurrentSelection(p, owned)
	if len(restore) != 1 || restore[0].Name != "Arcane" {
		t.Fatalf("黎风售卖后的盈天台恢复干员 = %#v，期望 Arcane", restore)
	}

	p.Usage = operatorActionUsageTarget
	p.TargetAssignments = nil
	nextRunCandidates := equivalentTargetCandidatesForOwnership(p, operatorOwnership{
		Operators: operatorNameSet(ownedNames),
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
				Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
			},
			{
				Location:   "Inactive",
				Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
			},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"共享"}))
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
			"Done": {Name: "Shared", CacheName: "共享", Priority: 0},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location: "Done",
				Candidates: []operatorCandidate{
					{Name: "Shared", CacheName: "共享", Priority: 0},
				},
			},
			{
				Location: "Pending",
				Candidates: []operatorCandidate{
					{Name: "Shared", CacheName: "共享", Priority: 0},
					{Name: "PendingOnly", CacheName: "待处理专属", Priority: 1},
				},
			},
		},
	}
	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"共享", "待处理专属"}))
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
			{Name: "Pogranichnik", CacheName: "骏卫", Priority: 0},
			{Name: "Fallback", CacheName: "备选", Priority: 1},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "ReconstructionHQ",
				Candidates: []operatorCandidate{{Name: "Pogranichnik", CacheName: "骏卫"}},
			},
		},
		LockedRestoreAssignments: map[string]operatorCandidate{
			"CardiacRemediationStation": {Name: "Pogranichnik", CacheName: "骏卫"},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"骏卫", "备选"}))
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
			{Name: "Pogranichnik", CacheName: "骏卫", Priority: 0},
			{Name: "Fallback", CacheName: "备选", Priority: 1},
		},
		RestoreGroups: []operatorCandidateGroup{
			{
				Location:   "ReconstructionHQ",
				Candidates: []operatorCandidate{{Name: "Pogranichnik", CacheName: "骏卫"}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"骏卫", "备选"}))
	if len(candidates) != 1 || candidates[0].Name != "Pogranichnik" {
		t.Fatalf("候选 = %#v，期望调入尚未完成恢复据点的 Pogranichnik", candidates)
	}
}

// TestReconstructionReplanProtectsInfraRestoreFrom20260719NoonLog 复现 12:00 日志：
// 基建前站已经恢复陈千语，重建指挥部排除被未启用据点占用的骏卫后必须改选阿列什。
func TestReconstructionReplanProtectsInfraRestoreFrom20260719NoonLog(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	for _, location := range []string{"InfraStation", "ReconstructionHQ"} {
		operatorSessionRegisterLocation(location)
	}

	data, err := loadOperatorSelectionData()
	if err != nil {
		t.Fatalf("加载 SellProduct 干员数据失败：%v", err)
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

	owned := operatorNameSet([]string{
		"余烬", "佩丽卡", "别礼", "卡契尔", "卡缪", "埃特拉", "大潘", "安塔尔", "弧光", "昼雪",
		"洛茜", "狼卫", "秋栗", "艾尔黛拉", "艾维文娜", "莱万汀", "萤石", "赛希", "阿列什", "陈千语",
		"骏卫", "黎风",
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
	if len(replanned) != 1 || replanned[0].Name != "Alesh" {
		t.Fatalf("重建指挥部重新规划 = %#v，期望避开陈千语并改选阿列什", replanned)
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
			{Name: "Pogranichnik", CacheName: "骏卫", Priority: 0},
		},
		LockedRestoreAssignments: map[string]operatorCandidate{
			"CardiacRemediationStation": {Name: "Pogranichnik", CacheName: "骏卫"},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"骏卫"}))
	if len(candidates) != 1 || candidates[0].Name != "Pogranichnik" {
		t.Fatalf("候选 = %#v，期望当前据点已锁定的干员", candidates)
	}
}

// TestOperatorSessionTargetAssignmentClearedAfterRestore 验证恢复完成后清除售卖分配并锁定恢复结果。
func TestOperatorSessionTargetAssignmentClearedAfterRestore(t *testing.T) {
	resetOperatorSessionForTest(t, operatorCacheModeCache)
	target := operatorCandidate{Name: "Laevatain", CacheName: "莱万汀"}
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
				Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
			},
			{
				Location:   "Pending",
				Candidates: []operatorCandidate{{Name: "Shared", CacheName: "共享", Priority: 0}},
			},
		},
	}

	candidates := candidatesForCurrentSelection(p, operatorNameSet([]string{"共享"}))
	if len(candidates) != 1 || candidates[0].Name != "Shared" {
		t.Fatalf("候选 = %#v，待处理据点应获得 Shared", candidates)
	}
}
