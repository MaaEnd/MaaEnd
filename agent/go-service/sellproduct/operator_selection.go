package sellproduct

// 本文件只负责“根据已有数据计算应该选择谁”，不负责识别或点击游戏界面。
// Pipeline 决定据点的执行顺序，operator_action.go 负责从界面读取候选，
// 本文件则把账号拥有的干员、启用据点和本轮历史状态合并成最终选择结果。
//
// 选择过程分为两个阶段：
//   - target（售卖）：先取当前据点的最高售卖档，再优先选择同时满足恢复条件的完美候选；
//   - restore（恢复）：把所有启用且尚未恢复的据点放在一起，计算互不重复的全局分配方案。

// 当前账号完整缓存中的拥有干员 ID 集合。
type operatorOwnership struct {
	Operators map[string]struct{}
}

// 根据完整缓存中的真实拥有集合计算本轮唯一候选。
func candidatesForOwnership(p *operatorSelectionParam, ownership operatorOwnership) []operatorCandidate {
	return candidatesForCurrentSelection(p, ownership.Operators)
}

// 返回当前账号可用的所有最高加成档候选。
// 最高加成档优先取同时满足售卖和恢复的完美候选；不存在时回退到最高售卖档。
// 与 candidatesForOwnership 不同，这里会保留所有同档候选，用于判断当前已派驻干员
// 是否已经足够好；只要当前干员在这个列表中，Pipeline 就不需要无意义地换成同档其他人。
func equivalentTargetCandidatesForOwnership(
	p *operatorSelectionParam,
	ownership operatorOwnership,
) []operatorCandidate {
	available := availableOperatorsForTarget(p, ownership.Operators)
	return preferredTargetCandidates(
		p.Candidates,
		available,
		p.Location,
		p.RestoreGroups,
		p.OutpostProsperityMaxLocations,
	)
}

// 根据 usage 生成本轮真正允许选择的候选。
// target 直接按收益优先级过滤已拥有干员；restore 必须先做全据点唯一分配，
// 当前据点只能使用全局方案分给它的那一名干员。
func candidatesForCurrentSelection(p *operatorSelectionParam, owned map[string]struct{}) []operatorCandidate {
	availableOwned := operatorsExcludingConflicts(p, owned)

	if p.Usage == operatorActionUsageTarget {
		// 售卖阶段允许从尚未处理的启用据点临时调人，但不能挪用已经完成恢复的结果。
		removeOtherLockedRestoreOperators(availableOwned, p.LockedRestoreAssignments, p.Location)
		candidates := preferredTargetCandidates(
			p.Candidates,
			availableOwned,
			p.Location,
			p.RestoreGroups,
			p.OutpostProsperityMaxLocations,
		)
		if len(candidates) == 0 {
			return nil
		}
		// 同档有多人时逐个模拟后续恢复方案，最终只返回整体结果最好的一人。
		return []operatorCandidate{selectTargetCandidateForRestorePlan(p, availableOwned, candidates)}
	}

	// usage=all 只用于扫描完整干员列表，不应该进入实际选择逻辑。
	if p.Usage != operatorActionUsageRestore {
		return nil
	}

	// 当前据点若已完成恢复，直接返回锁定结果，无需再进入全局搜索。
	if candidate, ok := p.LockedRestoreAssignments[p.Location]; ok {
		return []operatorCandidate{candidate}
	}

	// 恢复阶段同时规划所有启用且尚未完成恢复的据点。
	available := cloneStringSet(availableOwned)
	for _, candidate := range p.LockedRestoreAssignments {
		delete(available, candidate.Name)
	}
	preferred, reusable := restorePlanPreferences(p, availableOwned)
	plan := buildRestoreAssignmentPlanWithPreferencesAndTargets(
		restoreGroupsForSelection(p),
		available,
		preferred,
		reusable,
	)
	candidate, ok := plan.Assignments[p.Location]
	if !ok {
		return nil
	}
	return []operatorCandidate{candidate}
}

// 复制拥有集合并去掉本轮临时排除的干员。
func operatorsExcludingConflicts(p *operatorSelectionParam, owned map[string]struct{}) map[string]struct{} {
	available := cloneStringSet(owned)
	for excluded := range p.ExcludedOperators {
		delete(available, excluded)
	}
	return available
}

// 返回售卖阶段可用干员：排除冲突干员与其他据点已锁定的恢复干员。
func availableOperatorsForTarget(p *operatorSelectionParam, owned map[string]struct{}) map[string]struct{} {
	available := operatorsExcludingConflicts(p, owned)
	removeOtherLockedRestoreOperators(available, p.LockedRestoreAssignments, p.Location)
	return available
}

// 保护已经完成恢复的据点，避免后续据点再次调走其干员。
// 当前据点自己的锁定结果仍可沿用；尚未完成恢复的启用据点不在锁定集合中，固定顺序售卖仍可临时调人。
func removeOtherLockedRestoreOperators(
	available map[string]struct{},
	locked map[string]operatorCandidate,
	currentLocation string,
) {
	for location, candidate := range locked {
		if location == currentLocation {
			continue
		}
		delete(available, candidate.Name)
	}
}

// 从候选列表中保留当前据点状态下的最高售卖档，并维持原有游戏列表顺序。
func bestBonusTierCandidates(candidates []operatorCandidate, outpostProsperityMax bool) []operatorCandidate {
	if len(candidates) == 0 {
		return nil
	}
	bonusTier := func(candidate operatorCandidate) int {
		if outpostProsperityMax {
			return candidate.OutpostProsperityMaxBonusTier
		}
		return candidate.BonusTier
	}
	bestTier := bonusTier(candidates[0])
	best := make([]operatorCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		tier := bonusTier(candidate)
		if tier < bestTier {
			bestTier = tier
			best = best[:0]
		}
		if tier == bestTier {
			best = append(best, candidate)
		}
	}
	return best
}

// 在同档售卖候选中选择最有利于全局恢复的干员。
// 比较顺序：覆盖数、沿用人数、下次可复用人数、优先级成本；全相等时保留更靠前的候选。
func selectTargetCandidateForRestorePlan(
	p *operatorSelectionParam,
	owned map[string]struct{},
	candidates []operatorCandidate,
) operatorCandidate {
	if len(candidates) == 1 {
		return candidates[0]
	}
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

// 模拟“当前据点使用指定候选售卖”时的全局恢复方案；只构造临时状态，不写回真实会话。
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
		delete(available, lockedCandidate.Name)
	}
	preferred, reusable := restorePlanPreferences(&selection, owned)
	return buildRestoreAssignmentPlanWithPreferencesAndTargets(
		restoreGroupsForSelection(&selection),
		available,
		preferred,
		reusable,
	)
}

// 返回当前据点可用的最高加成档候选。
// 最高加成档指最高售卖档中同时满足该据点恢复条件的完美候选；
// 若不存在完美候选，则回退到最高售卖档。
func preferredTargetCandidates(
	candidates []operatorCandidate,
	owned map[string]struct{},
	location string,
	restoreGroups []operatorCandidateGroup,
	outpostProsperityMaxLocations map[string]struct{},
) []operatorCandidate {
	_, outpostProsperityMax := outpostProsperityMaxLocations[location]
	bestSelling := bestBonusTierCandidates(filterOwnedCandidates(candidates, owned), outpostProsperityMax)
	if len(bestSelling) == 0 {
		return nil
	}

	restoreNames := restoreCandidateNames(restoreGroups, location)
	perfect := make([]operatorCandidate, 0, len(bestSelling))
	for _, candidate := range bestSelling {
		if _, ok := restoreNames[candidate.Name]; ok {
			perfect = append(perfect, candidate)
		}
	}
	if len(perfect) > 0 {
		return perfect
	}
	return bestSelling
}

// 返回指定据点所有恢复候选的内部稳定名称集合。
func restoreCandidateNames(groups []operatorCandidateGroup, location string) map[string]struct{} {
	for _, group := range groups {
		if group.Location != location {
			continue
		}
		names := make(map[string]struct{}, len(group.Candidates))
		for _, candidate := range group.Candidates {
			names[candidate.Name] = struct{}{}
		}
		return names
	}
	return nil
}

// 一次计算恢复规划的次级偏好与下次可复用集合。
// preferred 不是硬约束：若保留某人会降低覆盖率，规划器会优先覆盖。
func restorePlanPreferences(
	p *operatorSelectionParam,
	owned map[string]struct{},
) (map[string]operatorCandidate, map[string]map[string]struct{}) {
	active := p.ActiveLocations
	preferred := make(map[string]operatorCandidate, len(active))
	reusable := make(map[string]map[string]struct{}, len(active))

	for location := range active {
		available := preferredTargetCandidates(
			p.TargetCandidatesByLocation[location],
			owned,
			location,
			p.RestoreGroups,
			p.OutpostProsperityMaxLocations,
		)
		if len(available) == 0 {
			continue
		}
		preferred[location] = available[0]
		names := make(map[string]struct{}, len(available))
		for _, candidate := range available {
			names[candidate.Name] = struct{}{}
		}
		reusable[location] = names
	}

	// 本轮实际售卖结果覆盖默认偏好，更能反映“保持不切换”的成本。
	for location, candidate := range p.TargetAssignments {
		if _, enabled := active[location]; enabled {
			preferred[location] = candidate
		}
	}
	return preferred, reusable
}

// 使用内部稳定名称比较两名干员是否相同。
func sameOperator(a, b operatorCandidate) bool {
	return a.Name == b.Name
}

// 只保留本次任务启用且尚未完成恢复的据点。
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

// 所有据点恢复岗位的全局分配结果。
// 比较顺序：Assigned > KeptTargets > ReusableTargets > 更小的 TotalCost。
type restoreAssignmentPlan struct {
	Assignments     map[string]operatorCandidate
	Assigned        int
	KeptTargets     int
	ReusableTargets int
	TotalCost       int
}

// 在“同一干员不能分配到多个据点”的约束下寻找最优恢复方案。
func buildRestoreAssignmentPlan(groups []operatorCandidateGroup, owned map[string]struct{}) restoreAssignmentPlan {
	return buildRestoreAssignmentPlanWithPreferences(groups, owned, nil)
}

// 在不降低恢复覆盖率的前提下，优先保留各据点的售卖干员。
func buildRestoreAssignmentPlanWithPreferences(
	groups []operatorCandidateGroup,
	owned map[string]struct{},
	preferred map[string]operatorCandidate,
) restoreAssignmentPlan {
	return buildRestoreAssignmentPlanWithPreferencesAndTargets(groups, owned, preferred, nil)
}

// 深度优先穷举全局恢复方案。
// groups 为待规划据点；owned 为允许使用的干员；preferred / reusableTargets 为次级优化目标。
// 同一干员可能适配多个据点，逐个据点贪心选择会错过全局最优，因此需要枚举所有有效组合。
func buildRestoreAssignmentPlanWithPreferencesAndTargets(
	groups []operatorCandidateGroup,
	owned map[string]struct{},
	preferred map[string]operatorCandidate,
	reusableTargets map[string]map[string]struct{},
) restoreAssignmentPlan {
	best := restoreAssignmentPlan{
		Assignments: map[string]operatorCandidate{},
	}
	// current 保存当前递归分支的临时分配，used 防止同一干员被分给多个据点。
	current := map[string]operatorCandidate{}
	used := map[string]struct{}{}

	var walk func(index int, assigned int, keptTargets int, reusableCount int, totalCost int)
	walk = func(index int, assigned int, keptTargets int, reusableCount int, totalCost int) {
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
		// 先探索跳过该据点的分支，保证资源不足时仍能覆盖部分据点。
		walk(index+1, assigned, keptTargets, reusableCount, totalCost)

		for _, candidate := range filterOwnedCandidates(group.Candidates, owned) {
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
				if _, ok := names[candidate.Name]; ok {
					reusable++
				}
			}
			walk(index+1, assigned+1, kept, reusable, totalCost+candidate.Priority)

			// 撤销当前选择，回到上一层状态后继续尝试下一名候选。
			delete(current, group.Location)
			delete(used, candidate.Name)
		}
	}
	walk(0, 0, 0, 0, 0)
	return best
}

// 按“覆盖数、本次保持人数、下次可沿用人数、候选成本”的字典序比较方案。
// 全部相等时返回 false，保留先找到的稳定方案。
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

// 复制分配表，避免回溯改写已保存的最优解。
func cloneRestoreAssignments(src map[string]operatorCandidate) map[string]operatorCandidate {
	dst := make(map[string]operatorCandidate, len(src))
	for location, candidate := range src {
		dst[location] = candidate
	}
	return dst
}
