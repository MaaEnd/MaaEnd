package achievement

import "testing"

func TestGetRulesIncludesStarterMXUAchievement(t *testing.T) {
	rules := getRules()
	if len(rules) == 0 {
		t.Fatal("getRules returned no rules")
	}

	for _, r := range rules {
		if r.ID == firstOpenMXUAchievementID {
			if r.Target != 1 {
				t.Fatalf("starter MXU achievement target = %d, want 1", r.Target)
			}
			return
		}
	}

	t.Fatalf("starter MXU achievement with ID %q not found in rules", firstOpenMXUAchievementID)
}

func TestGetRulesReturnsDefensiveCopy(t *testing.T) {
	rules := getRules()
	if len(rules) == 0 {
		t.Fatal("getRules returned no rules")
	}

	rules[0].ID = "mutated"

	fresh := getRules()
	if fresh[0].ID == "mutated" {
		t.Fatal("getRules returned mutable shared rules")
	}
}

func TestRulesByEventNormalizesTargets(t *testing.T) {
	oldRules := defaultRules
	defaultRules = []rule{
		{
			ID:     "zero-target",
			Title:  "Zero Target",
			Event:  eventOpenMXU,
			Target: 0,
		},
		{
			ID:     "negative-target",
			Title:  "Negative Target",
			Event:  eventOpenMXU,
			Target: -5,
		},
	}
	t.Cleanup(func() {
		defaultRules = oldRules
	})

	rules := rulesByEvent(eventOpenMXU)
	if len(rules) != 2 {
		t.Fatalf("rulesByEvent returned %d rules, want 2", len(rules))
	}
	for _, r := range rules {
		if r.Target != 1 {
			t.Fatalf("rule %q target = %d, want normalized target 1", r.ID, r.Target)
		}
	}
}
