package sellproduct

// operatorOwnership 描述当前账号的干员缓存及其完整性。
type operatorOwnership struct {
	Operators map[string]struct{}
	Complete  bool
}

// candidatesForOwnership 在完整缓存上计算精确方案；缓存不完整时假设所有候选都可能拥有，
// 从理论最优开始搜索。一次完整的列表到底会把该假设替换为真实拥有集合并重新规划。
func candidatesForOwnership(p *operatorSelectionParam, ownership operatorOwnership) []operatorCandidate {
	owned := ownership.Operators
	if !ownership.Complete {
		owned = operatorCandidateCacheNameSet(collectScanCandidates(p))
	}
	return candidatesForCurrentSelection(p, owned)
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
		candidates := availableTargetCandidates(p.Candidates, availableOwned, p.Location, p.LockedRestoreAssignments)
		if len(candidates) == 0 {
			return nil
		}
		// 缓存已经给出全局优先级，只搜索第一名，禁止在当前页降级选择次优干员。
		return candidates[:1]
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
	plan := buildRestoreAssignmentPlanWithPreferences(groups, available, preferred)
	for location, candidate := range p.LockedRestoreAssignments {
		plan.Assignments[location] = candidate
	}
	candidate, ok := plan.Assignments[p.Location]
	if !ok {
		return nil
	}
	return []operatorCandidate{candidate}
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
	if len(active) == 0 {
		active = map[string]struct{}{p.Location: {}}
	}
	for location := range active {
		candidates := p.TargetCandidatesByLocation[location]
		if len(candidates) == 0 && location == p.Location {
			candidates = p.Candidates
		}
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

// sameOperator 使用内部名称比较干员，缺少内部名称时回退到缓存键。
func sameOperator(a, b operatorCandidate) bool {
	if a.Name != "" && b.Name != "" {
		return a.Name == b.Name
	}
	return operatorCandidateCacheName(a) == operatorCandidateCacheName(b)
}

// restoreGroupsForSelection 只保留本次任务启用且尚未完成恢复的据点。
// 旧调用方没有注册作用域时，安全回退为只规划当前据点，绝不预留给未知据点。
func restoreGroupsForSelection(p *operatorSelectionParam) []operatorCandidateGroup {
	active := p.ActiveLocations
	if len(active) == 0 {
		active = map[string]struct{}{p.Location: {}}
	}
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
// Assignments 以据点为键；Assigned 和 KeptTargets 越大越好，TotalCost 越小越好。
type restoreAssignmentPlan struct {
	Assignments map[string]operatorCandidate
	Assigned    int
	KeptTargets int
	TotalCost   int
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
	best := restoreAssignmentPlan{
		Assignments: map[string]operatorCandidate{},
	}
	current := map[string]operatorCandidate{}
	used := map[string]struct{}{}

	var walk func(index int, assigned int, keptTargets int, totalCost int)
	walk = func(index int, assigned int, keptTargets int, totalCost int) {
		// 所有据点都已做出“跳过或分配”决策，检查当前叶子方案是否更优。
		if index >= len(groups) {
			if isBetterRestorePlan(assigned, keptTargets, totalCost, best) {
				best.Assigned = assigned
				best.KeptTargets = keptTargets
				best.TotalCost = totalCost
				best.Assignments = cloneRestoreAssignments(current)
			}
			return
		}

		group := groups[index]
		// 先探索不为当前据点分配干员的分支，保证资源不足时也能得到部分解。
		walk(index+1, assigned, keptTargets, totalCost)

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
			walk(index+1, assigned+1, kept, totalCost+candidate.Priority)
			delete(current, group.Location)
			delete(used, candidate.Name)
		}
	}
	walk(0, 0, 0, 0)
	return best
}

// isBetterRestorePlan 按“覆盖数、保持人数、候选成本”的字典序比较两个方案。
func isBetterRestorePlan(assigned, keptTargets, totalCost int, best restoreAssignmentPlan) bool {
	if assigned != best.Assigned {
		return assigned > best.Assigned
	}
	if keptTargets != best.KeptTargets {
		return keptTargets > best.KeptTargets
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
