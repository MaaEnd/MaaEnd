package sellproduct

// operatorOwnership 描述当前账号完整缓存中的拥有干员集合。
type operatorOwnership struct {
	Operators map[string]struct{}
}

// candidatesForOwnership 根据完整缓存中的真实拥有集合计算精确方案。
func candidatesForOwnership(p *operatorSelectionParam, ownership operatorOwnership) []operatorCandidate {
	return candidatesForCurrentSelection(p, ownership.Operators)
}

// equivalentTargetCandidatesForOwnership 返回当前账号可用的最高售卖加成档候选。
// 当前派驻识别使用完整同档集合，避免把稳定顺序误当成收益差异而产生无意义更换。
func equivalentTargetCandidatesForOwnership(
	p *operatorSelectionParam,
	ownership operatorOwnership,
) []operatorCandidate {
	available := cloneStringSet(ownership.Operators)
	for excluded := range p.ExcludedOperators {
		delete(available, excluded)
	}
	return bestBonusTierCandidates(availableTargetCandidates(
		p.Candidates,
		available,
		p.Location,
		p.LockedRestoreAssignments,
	))
}

// candidatesForCurrentSelection 根据 usage 生成本轮真正允许选择的候选。
// target 直接按收益优先级过滤已拥有干员；restore 必须先做全据点唯一分配，
// 当前据点只能使用全局方案分给它的那一名干员。
func candidatesForCurrentSelection(p *operatorSelectionParam, owned map[string]struct{}) []operatorCandidate {
	availableOwned := cloneStringSet(owned)
	for excluded := range p.ExcludedOperators {
		delete(availableOwned, excluded)
	}
	if p.Usage == operatorActionUsageTarget {
		candidates := bestBonusTierCandidates(availableTargetCandidates(
			p.Candidates,
			availableOwned,
			p.Location,
			p.LockedRestoreAssignments,
		))
		if len(candidates) == 0 {
			return nil
		}
		return []operatorCandidate{selectTargetCandidateForRestorePlan(p, availableOwned, candidates)}
	}
	if p.Usage != operatorActionUsageRestore {
		return nil
	}
	groups := restoreGroupsForSelection(p)
	available := cloneStringSet(availableOwned)
	for _, candidate := range p.LockedRestoreAssignments {
		delete(available, operatorCandidateCacheName(candidate))
	}
	preferred := preferredRestoreAssignments(p, availableOwned)
	plan := buildRestoreAssignmentPlanWithPreferencesAndTargets(
		groups,
		available,
		preferred,
		reusableTargetCandidatesByLocation(p, availableOwned),
	)
	for location, candidate := range p.LockedRestoreAssignments {
		plan.Assignments[location] = candidate
	}
	candidate, ok := plan.Assignments[p.Location]
	if !ok {
		return nil
	}
	return []operatorCandidate{candidate}
}

func bestBonusTierCandidates(candidates []operatorCandidate) []operatorCandidate {
	if len(candidates) == 0 {
		return nil
	}
	bestTier := candidates[0].BonusTier
	count := 1
	for count < len(candidates) && candidates[count].BonusTier == bestTier {
		count++
	}
	return candidates[:count]
}

// selectTargetCandidateForRestorePlan 在同档售卖候选中选择最有利于全局恢复的干员。
// 比较顺序复用恢复规划的“覆盖数、沿用人数、稳定成本”，最后按售卖候选顺序决胜。
func selectTargetCandidateForRestorePlan(
	p *operatorSelectionParam,
	owned map[string]struct{},
	candidates []operatorCandidate,
) operatorCandidate {
	bestCandidate := candidates[0]
	bestPlan := restorePlanForTargetCandidate(p, owned, bestCandidate)
	for _, candidate := range candidates[1:] {
		plan := restorePlanForTargetCandidate(p, owned, candidate)
		if isBetterRestorePlan(
			plan.Assigned,
			plan.KeptTargets,
			plan.ReusableTargets,
			plan.TotalCost,
			bestPlan,
		) {
			bestCandidate = candidate
			bestPlan = plan
		}
	}
	return bestCandidate
}

func restorePlanForTargetCandidate(
	p *operatorSelectionParam,
	owned map[string]struct{},
	candidate operatorCandidate,
) restoreAssignmentPlan {
	selection := *p
	selection.TargetAssignments = cloneRestoreAssignments(p.TargetAssignments)
	selection.TargetAssignments[p.Location] = candidate

	available := cloneStringSet(owned)
	for _, lockedCandidate := range p.LockedRestoreAssignments {
		delete(available, operatorCandidateCacheName(lockedCandidate))
	}
	return buildRestoreAssignmentPlanWithPreferencesAndTargets(
		restoreGroupsForSelection(&selection),
		available,
		preferredRestoreAssignments(&selection, owned),
		reusableTargetCandidatesByLocation(&selection, owned),
	)
}

// availableTargetCandidates 筛出已拥有且未被其他据点恢复岗位锁定的候选。
func availableTargetCandidates(
	candidates []operatorCandidate,
	owned map[string]struct{},
	location string,
	locked map[string]operatorCandidate,
) []operatorCandidate {
	filtered := filterOwnedCandidates(candidates, owned)
	result := make([]operatorCandidate, 0, len(filtered))
	for _, candidate := range filtered {
		reservedElsewhere := false
		for lockedLocation, lockedCandidate := range locked {
			if lockedLocation != location && sameOperator(candidate, lockedCandidate) {
				reservedElsewhere = true
				break
			}
		}
		if !reservedElsewhere {
			result = append(result, candidate)
		}
	}
	return result
}

// preferredRestoreAssignments 返回各据点应尽量保留的当前售卖干员。
func preferredRestoreAssignments(p *operatorSelectionParam, owned map[string]struct{}) map[string]operatorCandidate {
	preferred := make(map[string]operatorCandidate)
	active := p.ActiveLocations
	for location := range active {
		candidates := p.TargetCandidatesByLocation[location]
		available := availableTargetCandidates(candidates, owned, location, p.LockedRestoreAssignments)
		if len(available) > 0 {
			preferred[location] = available[0]
		}
	}
	for location, candidate := range p.TargetAssignments {
		if _, enabled := active[location]; enabled {
			preferred[location] = candidate
		}
	}
	return preferred
}

// reusableTargetCandidatesByLocation 返回各据点下次运行时可直接沿用的最高售卖加成档干员。
func reusableTargetCandidatesByLocation(
	p *operatorSelectionParam,
	owned map[string]struct{},
) map[string]map[string]struct{} {
	active := p.ActiveLocations
	reusable := make(map[string]map[string]struct{}, len(active))
	for location := range active {
		candidates := p.TargetCandidatesByLocation[location]
		available := bestBonusTierCandidates(availableTargetCandidates(
			candidates,
			owned,
			location,
			p.LockedRestoreAssignments,
		))
		if len(available) == 0 {
			continue
		}
		names := make(map[string]struct{}, len(available))
		for _, candidate := range available {
			names[operatorCandidateCacheName(candidate)] = struct{}{}
		}
		reusable[location] = names
	}
	return reusable
}

// sameOperator 使用内部稳定名称比较干员。
func sameOperator(a, b operatorCandidate) bool {
	return a.Name == b.Name
}

// restoreGroupsForSelection 只保留本次任务启用且尚未完成恢复的据点。
func restoreGroupsForSelection(p *operatorSelectionParam) []operatorCandidateGroup {
	active := p.ActiveLocations
	groups := make([]operatorCandidateGroup, 0, len(active))
	for _, group := range p.RestoreGroups {
		if _, ok := active[group.Location]; !ok {
			continue
		}
		if _, completed := p.CompletedRestoreLocations[group.Location]; completed {
			continue
		}
		groups = append(groups, group)
	}
	return groups
}

// restoreAssignmentPlan 是所有据点恢复岗位的全局分配结果。
// Assignments 以据点为键；Assigned、KeptTargets、ReusableTargets 越大越好，TotalCost 越小越好。
type restoreAssignmentPlan struct {
	Assignments     map[string]operatorCandidate
	Assigned        int
	KeptTargets     int
	ReusableTargets int
	TotalCost       int
}

// buildRestoreAssignmentPlan 在“同一干员不能分配到多个据点”的约束下寻找最优恢复方案。
//
// 这里使用深度优先穷举而不是逐据点贪心：某个高优先级干员可能同时适配多个据点，
// 若局部先选会导致后续据点无人可用。比较方案时先最大化成功分配的据点数，再尽量
// 保持各据点的售卖干员，最后最小化 Priority 总和。
func buildRestoreAssignmentPlan(groups []operatorCandidateGroup, owned map[string]struct{}) restoreAssignmentPlan {
	return buildRestoreAssignmentPlanWithPreferences(groups, owned, nil)
}

// buildRestoreAssignmentPlanWithPreferences 在不降低恢复覆盖率的前提下，优先保留各据点的售卖干员。
func buildRestoreAssignmentPlanWithPreferences(
	groups []operatorCandidateGroup,
	owned map[string]struct{},
	preferred map[string]operatorCandidate,
) restoreAssignmentPlan {
	return buildRestoreAssignmentPlanWithPreferencesAndTargets(groups, owned, preferred, nil)
}

// buildRestoreAssignmentPlanWithPreferencesAndTargets 在当前运行更换次数相同时，
// 优先让最终派驻可以直接用于下次最高档售卖，避免任务间反复切换。
func buildRestoreAssignmentPlanWithPreferencesAndTargets(
	groups []operatorCandidateGroup,
	owned map[string]struct{},
	preferred map[string]operatorCandidate,
	reusableTargets map[string]map[string]struct{},
) restoreAssignmentPlan {
	best := restoreAssignmentPlan{
		Assignments: map[string]operatorCandidate{},
	}
	current := map[string]operatorCandidate{}
	used := map[string]struct{}{}

	var walk func(index int, assigned int, keptTargets int, reusableCount int, totalCost int)
	walk = func(index int, assigned int, keptTargets int, reusableCount int, totalCost int) {
		// 所有据点都已做出“跳过或分配”决策，检查当前叶子方案是否更优。
		if index >= len(groups) {
			if isBetterRestorePlan(assigned, keptTargets, reusableCount, totalCost, best) {
				best.Assigned = assigned
				best.KeptTargets = keptTargets
				best.ReusableTargets = reusableCount
				best.TotalCost = totalCost
				best.Assignments = cloneRestoreAssignments(current)
			}
			return
		}

		group := groups[index]
		// 先探索不为当前据点分配干员的分支，保证资源不足时也能得到部分解。
		walk(index+1, assigned, keptTargets, reusableCount, totalCost)

		for _, candidate := range filterOwnedCandidates(group.Candidates, owned) {
			// Name 是本轮分配的唯一身份；已占用的干员不能同时恢复到另一个据点。
			if _, ok := used[candidate.Name]; ok {
				continue
			}
			used[candidate.Name] = struct{}{}
			current[group.Location] = candidate
			kept := keptTargets
			if preferredCandidate, ok := preferred[group.Location]; ok && sameOperator(candidate, preferredCandidate) {
				kept++
			}
			reusable := reusableCount
			if names := reusableTargets[group.Location]; names != nil {
				if _, ok := names[operatorCandidateCacheName(candidate)]; ok {
					reusable++
				}
			}
			walk(index+1, assigned+1, kept, reusable, totalCost+candidate.Priority)
			delete(current, group.Location)
			delete(used, candidate.Name)
		}
	}
	walk(0, 0, 0, 0, 0)
	return best
}

// isBetterRestorePlan 按“覆盖数、本次保持人数、下次可沿用人数、候选成本”的字典序比较方案。
func isBetterRestorePlan(
	assigned int,
	keptTargets int,
	reusableTargets int,
	totalCost int,
	best restoreAssignmentPlan,
) bool {
	if assigned != best.Assigned {
		return assigned > best.Assigned
	}
	if keptTargets != best.KeptTargets {
		return keptTargets > best.KeptTargets
	}
	if reusableTargets != best.ReusableTargets {
		return reusableTargets > best.ReusableTargets
	}
	return totalCost < best.TotalCost
}

// cloneRestoreAssignments 复制当前回溯状态，防止后续撤销选择时改写已经保存的最优解。
func cloneRestoreAssignments(src map[string]operatorCandidate) map[string]operatorCandidate {
	dst := make(map[string]operatorCandidate, len(src))
	for location, candidate := range src {
		dst[location] = candidate
	}
	return dst
}
