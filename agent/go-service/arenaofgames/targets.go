package arenaofgames

// targetGoal 将模式选择与三态求值集中在一处；正则目标独立于内置牌型。
type targetGoal struct {
	attach targetAttach
	regex  *regexMatcher
}

// resolveGoal 在任何截图、OCR 或点击之前校验目标配置。
// 缺省模式保留旧配置的内置牌型行为，只有正则模式才编译和校验表达式。
func resolveGoal(src interface{ GetNodeJSON(string) (string, error) }, cache *regexMatcherCache) (targetGoal, error) {
	attach, err := readTargetAttach(src)
	if err != nil {
		return targetGoal{}, err
	}
	goal := targetGoal{attach: attach}
	if attach.Mode == "regex" {
		goal.regex, err = cache.get(attach.Regex, attach.Unordered)
		if err != nil {
			return targetGoal{}, err
		}
	}
	return goal, nil
}

func (g targetGoal) evaluate(detail handDetail, presets map[category]bool) (string, error) {
	if g.regex != nil {
		return g.regex.evaluate(detail)
	}
	return evaluateHand(handCounts(detail), detail.Count, presets), nil
}
