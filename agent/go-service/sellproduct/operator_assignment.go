package sellproduct

// restoreAssignmentPlan 是所有据点恢复岗位的全局分配结果。
// Assignments 以据点为键；Assigned 越大越好，TotalCost 为各候选 Priority 之和，越小越好。
type restoreAssignmentPlan struct {
	Assignments map[string]operatorCandidate
	Assigned    int
	TotalCost   int
}

// buildRestoreAssignmentPlan 在“同一干员不能分配到多个据点”的约束下寻找最优恢复方案。
//
// 这里使用深度优先穷举而不是逐据点贪心：某个高优先级干员可能同时适配多个据点，
// 若局部先选会导致后续据点无人可用。比较方案时先最大化成功分配的据点数，再最小化
// Priority 总和，因此在无法覆盖全部据点时也会返回覆盖面最大的可执行方案。
func buildRestoreAssignmentPlan(groups []operatorCandidateGroup, owned map[string]struct{}) restoreAssignmentPlan {
	best := restoreAssignmentPlan{
		Assignments: map[string]operatorCandidate{},
	}
	current := map[string]operatorCandidate{}
	used := map[string]struct{}{}

	var walk func(index int, assigned int, totalCost int)
	walk = func(index int, assigned int, totalCost int) {
		// 所有据点都已做出“跳过或分配”决策，检查当前叶子方案是否更优。
		if index >= len(groups) {
			if isBetterRestorePlan(assigned, totalCost, best.Assigned, best.TotalCost) {
				best.Assigned = assigned
				best.TotalCost = totalCost
				best.Assignments = cloneRestoreAssignments(current)
			}
			return
		}

		group := groups[index]
		// 先探索不为当前据点分配干员的分支，保证资源不足时也能得到部分解。
		walk(index+1, assigned, totalCost)

		for _, candidate := range filterOwnedCandidates(group.Candidates, owned) {
			// Name 是本轮分配的唯一身份；已占用的干员不能同时恢复到另一个据点。
			if _, ok := used[candidate.Name]; ok {
				continue
			}
			used[candidate.Name] = struct{}{}
			current[group.Location] = candidate
			walk(index+1, assigned+1, totalCost+candidate.Priority)
			delete(current, group.Location)
			delete(used, candidate.Name)
		}
	}
	walk(0, 0, 0)
	return best
}

// isBetterRestorePlan 按“覆盖据点数优先、总成本次优”的字典序比较两个方案。
func isBetterRestorePlan(assigned, totalCost, bestAssigned, bestTotalCost int) bool {
	if assigned != bestAssigned {
		return assigned > bestAssigned
	}
	return totalCost < bestTotalCost
}

// cloneRestoreAssignments 复制当前回溯状态，防止后续撤销选择时改写已经保存的最优解。
func cloneRestoreAssignments(src map[string]operatorCandidate) map[string]operatorCandidate {
	dst := make(map[string]operatorCandidate, len(src))
	for location, candidate := range src {
		dst[location] = candidate
	}
	return dst
}
