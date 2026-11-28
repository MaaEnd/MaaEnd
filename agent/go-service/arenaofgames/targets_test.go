package arenaofgames

import (
	"encoding/json"
	"testing"
)

func TestResolveGoalKeepsPresetDefaultsAndIgnoresStaleRegex(t *testing.T) {
	var cache regexMatcherCache
	for _, mode := range []string{"", "preset"} {
		store := goalStore(t, targetAttach{Mode: mode, Regex: "[", Unordered: true, Flush: true})
		goal, err := resolveGoal(store, &cache)
		if err != nil || goal.regex != nil || goal.attach.Mode != "preset" {
			t.Fatalf("mode=%q: goal=%+v error=%v", mode, goal, err)
		}
		targets, err := resolvePresetTargets(store, nil, goal.attach)
		if err != nil {
			t.Fatal(err)
		}
		detail := goalHand(t,
			handSlot{Type: "Bamboo", Point: 1}, handSlot{Type: "Bamboo", Point: 2},
			handSlot{Type: "Bamboo", Point: 3}, handSlot{Type: "Bamboo", Point: 5})
		if status, err := goal.evaluate(detail, targets); err != nil || status != "complete" {
			t.Fatalf("preset goal = %q, %v; want complete", status, err)
		}
	}
}

func TestRegexGoalIsIndependentOfPresetAndDailyTargets(t *testing.T) {
	// 来源仅有目标配置，没有今日好运 OCR 节点；正则模式应可正常启动。
	store := goalStore(t, targetAttach{
		Mode: "regex", Regex: "3m", HandOfToday: true,
		Flush: true, FullHouse: true, FourOfAKind: true, StraightFlush: true,
	})
	var cache regexMatcherCache
	goal, err := resolveGoal(store, &cache)
	if err != nil || goal.regex == nil {
		t.Fatalf("resolve regex goal: %+v, %v", goal, err)
	}
	detail := goalHand(t,
		handSlot{Type: "Bamboo", Point: 1}, handSlot{Type: "Bamboo", Point: 2},
		handSlot{Type: "Bamboo", Point: 3}, handSlot{Type: "Bamboo", Point: 5})
	if status, err := goal.evaluate(detail, map[category]bool{catFlush: true}); err != nil || status != "rejected" {
		t.Fatalf("preset flush must not satisfy regex: %q, %v", status, err)
	}
}

func TestRegexGoalRoutesEachDrawState(t *testing.T) {
	var cache regexMatcherCache
	goal, err := resolveGoal(goalStore(t, targetAttach{Mode: "regex", Regex: "3s2m5p"}), &cache)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		cards []handSlot
		next  string
	}{
		{"empty", nil, nodeDrawCardPrefix + "1"},
		{"prefix", []handSlot{{Type: "Bamboo", Point: 3}, {Type: "Character", Point: 2}}, nodeDrawCardPrefix + "3"},
		{"matched", []handSlot{{Type: "Bamboo", Point: 3}, {Type: "Character", Point: 2}, {Type: "Dot", Point: 5}}, nodeStop},
		{"wrong suit", []handSlot{{Type: "Dot", Point: 3}}, nodeAbandon},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := goalHand(t, tc.cards...)
			status, err := goal.evaluate(detail, nil)
			if err != nil {
				t.Fatal(err)
			}
			next, err := decideNext(status, detail.Count)
			if err != nil || next != tc.next {
				t.Fatalf("status=%q next=%q error=%v; want %q", status, next, err, tc.next)
			}
		})
	}
	// 回到空手只表示重新抽牌，不替换已编译的规则和求值缓存。
	restarted, err := resolveGoal(goalStore(t, goal.attach), &cache)
	if err != nil || restarted.regex != goal.regex {
		t.Fatalf("restart replaced regex cache: %v", err)
	}
}

func TestResolveGoalRejectsConfigurationBeforeDecision(t *testing.T) {
	for _, attach := range []targetAttach{
		{Mode: "unknown"},
		{Mode: "regex", Regex: ""},
		{Mode: "regex", Regex: "["},
		{Mode: "regex", Regex: "[6-9][msp]"},
		{Mode: "regex", Regex: "^$"},
	} {
		var cache regexMatcherCache
		if _, err := resolveGoal(goalStore(t, attach), &cache); err == nil {
			t.Fatalf("accepted invalid target %+v", attach)
		}
	}
	var cache regexMatcherCache
	for _, store := range []nodeStore{{}, {targetNode: "{"}} {
		if _, err := resolveGoal(store, &cache); err == nil {
			t.Fatalf("accepted unreadable target configuration %v", store)
		}
	}
}

func goalStore(t *testing.T, attach targetAttach) nodeStore {
	t.Helper()
	raw, err := json.Marshal(struct {
		Attach targetAttach `json:"attach"`
	}{Attach: attach})
	if err != nil {
		t.Fatal(err)
	}
	return nodeStore{targetNode: string(raw)}
}

func goalHand(t *testing.T, cards ...handSlot) handDetail {
	t.Helper()
	detail, err := classifyHand(handCandidates(cards...))
	if err != nil {
		t.Fatal(err)
	}
	return detail
}
