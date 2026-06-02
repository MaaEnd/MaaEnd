package achievement

var defaultRules = []rule{
	{
		ID:     firstOpenMXUAchievementID,
		Title:  "小试牛刀",
		Event:  eventOpenMXU,
		Target: 1,
	},
}

func normalizeRule(r rule) rule {
	if r.Target <= 0 {
		r.Target = 1
	}
	return r
}

// rulesByEvent returns all built-in rules matching the given event.
func rulesByEvent(event string) []rule {
	matched := make([]rule, 0, 1)
	for _, r := range defaultRules {
		r = normalizeRule(r)
		if r.Event == event {
			matched = append(matched, r)
		}
	}
	return matched
}

// getRules returns a copy of the built-in achievement rules.
func getRules() []rule {
	result := make([]rule, 0, len(defaultRules))
	for _, r := range defaultRules {
		result = append(result, normalizeRule(r))
	}
	return result
}
