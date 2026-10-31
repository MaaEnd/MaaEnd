package arenaofgames

import (
	"strings"
	"testing"
)

func TestRegexFullMatch(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		input   string
		matched bool
	}{
		{"exact", `1m`, "1m", true},
		{"prefix is not enough", `1m`, "1m2s", false},
		{"suffix is not enough", `1m`, "2s1m", false},
		{"short alternative first", `1m|1m2s`, "1m2s", true},
		{"empty alternative first", `|1m`, "1m", true},
		{"empty alternative does not accept a longer hand", `|1m`, "1m2s", false},
		{"lazy repetition still chooses the full match", `(?:1m)*?`, "1m1m", true},
		{"inline case flag", `(?i)1M`, "1m", true},
		{"inline multiline flag", `(?m)^1m$`, "1m", true},
		{"multiline match does not accept trailing text", `(?m)^1m$`, "1m\n2s", false},
		{"unclosed quote consumes the original pattern only", `\Q1m`, "1m", true},
		{"explicit quote", `\Q1m\E`, "1m", true},
		{"text anchors", `\A1m\z`, "1m", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cache regexMatcherCache
			matcher, err := cache.get(tc.pattern, false)
			if err != nil {
				t.Fatalf("get(%q): %v", tc.pattern, err)
			}
			if got := matcher.fullMatch([]byte(tc.input)); got != tc.matched {
				t.Fatalf("fullMatch(%q) = %v; want %v", tc.input, got, tc.matched)
			}
		})
	}
}

func TestRegexOrderedEvaluation(t *testing.T) {
	var cache regexMatcherCache
	matcher, err := cache.get(`1m2s3p`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cards  string
		status string
	}{
		{"", "continuing"},
		{"1m", "continuing"},
		{"1m2s", "continuing"},
		{"1m2s3p", "complete"},
		{"2s1m", "rejected"},
		{"1m3p", "rejected"},
		{"3p2s1m", "rejected"},
		{"1m2s3p4m", "rejected"},
		{"1m2s3p4m5s", "rejected"},
	} {
		t.Run(tc.cards, func(t *testing.T) {
			got, err := matcher.evaluate(regexTestHand(t, tc.cards))
			if err != nil || got != tc.status {
				t.Fatalf("evaluate(%q) = %q, %v; want %q", tc.cards, got, err, tc.status)
			}
		})
	}
}

func TestRegexUnorderedEvaluation(t *testing.T) {
	var cache regexMatcherCache
	matcher, err := cache.get(`1m1m2s3p`, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		cards  string
		status string
	}{
		{"", "continuing"},
		{"1m", "continuing"},
		{"2s1m", "continuing"},
		{"3p1m2s", "continuing"},
		{"1m1m2s", "continuing"},
		{"1m1m2s3p", "complete"},
		{"3p1m2s1m", "complete"},
		{"1m1m1m", "rejected"},
		{"1m2s3p4m", "rejected"},
		{"3p1m2s1m5s", "rejected"},
	} {
		t.Run(tc.cards, func(t *testing.T) {
			got, err := matcher.evaluate(regexTestHand(t, tc.cards))
			if err != nil || got != tc.status {
				t.Fatalf("evaluate(%q) = %q, %v; want %q", tc.cards, got, err, tc.status)
			}
		})
	}
	// 换序的相同手牌应直接复用多重集状态，不能因为重牌生成额外缓存项。
	before := len(matcher.unorderedStates)
	if status, err := matcher.evaluate(regexTestHand(t, "1m3p1m2s")); err != nil || status != "complete" {
		t.Fatalf("reordered duplicate hand = %q, %v", status, err)
	}
	if len(matcher.unorderedStates) != before {
		t.Fatal("reordering the same multiset added cached states")
	}
}

func TestRegexEmptyHandAndEarlyStop(t *testing.T) {
	for _, unordered := range []bool{false, true} {
		var cache regexMatcherCache
		matcher, err := cache.get(`|1m|1m2s`, unordered)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			cards  string
			status string
		}{
			{"", "continuing"},
			{"1m", "complete"},
			{"1m2s", "complete"},
		} {
			status, err := matcher.evaluate(regexTestHand(t, tc.cards))
			if err != nil || status != tc.status {
				t.Fatalf("unordered=%v cards=%q: %q, %v; want %q", unordered, tc.cards, status, err, tc.status)
			}
		}
	}
}

func TestRegexFiveCardLimit(t *testing.T) {
	for _, unordered := range []bool{false, true} {
		var cache regexMatcherCache
		matcher, err := cache.get(`1m2s3p4m5s`, unordered)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			cards  string
			status string
		}{
			{"1m2s3p4m", "continuing"},
			{"1m2s3p4m5s", "complete"},
			{"1m2s3p4m5p", "rejected"},
		} {
			status, err := matcher.evaluate(regexTestHand(t, tc.cards))
			if err != nil || status != tc.status {
				t.Fatalf("unordered=%v cards=%q: %q, %v; want %q", unordered, tc.cards, status, err, tc.status)
			}
		}
	}
}

func TestRegexInvalidConfigurations(t *testing.T) {
	for _, pattern := range []string{
		"", " \n\t", `[`, `)|(?:`, `(?=1m)1m`, `(1m)\1`,
	} {
		var cache regexMatcherCache
		if matcher, err := cache.get(pattern, false); err == nil || matcher != nil {
			t.Fatalf("get(%q) = %v, %v; want configuration error", pattern, matcher, err)
		}
	}
}

func TestRegexGloballyImpossibleConfigurations(t *testing.T) {
	for _, unordered := range []bool{false, true} {
		for _, pattern := range []string{`6m`, `\A\z`, `(?:)`, `(?:[1-5][msp]){6}`, `1m `} {
			var cache regexMatcherCache
			matcher, err := cache.get(pattern, unordered)
			if err == nil || matcher != nil || !strings.Contains(err.Error(), "no legal hand") {
				t.Fatalf("unordered=%v get(%q) = %v, %v; want no legal hand", unordered, pattern, matcher, err)
			}
			// 全局搜索失败也要缓存，避免每个回调再次遍历所有可能手牌。
			if again, nextErr := cache.get(pattern, unordered); again != nil || nextErr != err {
				t.Fatalf("impossible rule error was not cached: %v, %v", again, nextErr)
			}
		}
	}
}

func TestRegexConfigurationCache(t *testing.T) {
	var cache regexMatcherCache
	first, err := cache.get(`1m`, false)
	if err != nil {
		t.Fatal(err)
	}
	if status, err := first.evaluate(regexTestHand(t, "")); err != nil || status != "continuing" {
		t.Fatalf("empty hand = %q, %v", status, err)
	}
	if again, err := cache.get(`1m`, false); err != nil || again != first {
		t.Fatal("same configuration did not survive an empty-hand evaluation")
	}
	unordered, err := cache.get(`1m`, true)
	if err != nil || unordered == first || !unordered.unordered {
		t.Fatalf("changing the ordering mode did not replace the matcher: %v", err)
	}
	changed, err := cache.get(`2m`, true)
	if err != nil || changed == unordered {
		t.Fatalf("changing the pattern did not replace the matcher: %v", err)
	}
	if status, err := changed.evaluate(regexTestHand(t, "1m")); err != nil || status != "rejected" {
		t.Fatalf("new pattern reused old results: %q, %v", status, err)
	}
	if status, err := first.evaluate(regexTestHand(t, "1m")); err != nil || status != "complete" {
		t.Fatalf("replacing the cache mutated a previous matcher: %q, %v", status, err)
	}
	if restored, err := cache.get(`1m`, true); err != nil || restored == unordered {
		t.Fatal("the cache retained more than the latest configuration")
	}
	_, invalidErr := cache.get(`[`, false)
	if _, err := cache.get(`[`, false); invalidErr == nil || err != invalidErr {
		t.Fatal("syntax errors were not cached")
	}
	if recovered, err := cache.get(`1m`, false); err != nil || recovered == first {
		t.Fatal("a valid replacement did not recover from a cached syntax error")
	}
}

func TestRegexRejectsInvalidHandDetails(t *testing.T) {
	var cache regexMatcherCache
	matcher, err := cache.get(`1m`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, detail := range []handDetail{
		{Count: -1},
		{Count: 6},
		{Count: 1},
		{Count: 1, Slots: [5]handSlot{{Type: "unknown", Point: 1}}},
		{Count: 1, Slots: [5]handSlot{{Type: "Character", Point: 6}}},
		{Count: 0, Slots: [5]handSlot{{Type: "Character", Point: 1}}},
	} {
		if status, err := matcher.evaluate(detail); err == nil || status != "" {
			t.Fatalf("invalid detail %+v produced %q, %v", detail, status, err)
		}
	}
	if status, err := (*regexMatcher)(nil).evaluate(handDetail{}); err == nil || status != "" {
		t.Fatalf("uninitialized matcher produced %q, %v", status, err)
	}
}

func regexTestHand(tb testing.TB, cards string) handDetail {
	tb.Helper()
	if len(cards)%2 != 0 || len(cards) > 10 {
		tb.Fatalf("invalid test hand %q", cards)
	}
	detail := handDetail{Count: len(cards) / 2}
	for slot := range detail.Slots {
		detail.Slots[slot] = handSlot{Type: "back"}
	}
	for slot := range detail.Count {
		var suit string
		switch cards[2*slot+1] {
		case 'm':
			suit = "Character"
		case 's':
			suit = "Bamboo"
		case 'p':
			suit = "Dot"
		default:
			tb.Fatalf("invalid test suit in %q", cards)
		}
		detail.Slots[slot] = handSlot{Type: suit, Point: int(cards[2*slot] - '0')}
	}
	return detail
}

func BenchmarkRegexMatcherCold(b *testing.B) {
	for _, tc := range []struct {
		name      string
		pattern   string
		unordered bool
		valid     bool
	}{
		{"ordered_last_solution", `5p5p5p5p5p`, false, true},
		{"unordered_last_solution", `5p5p5p5p5p`, true, true},
		{"ordered_no_solution", `6m`, false, false},
		{"unordered_no_solution", `6m`, true, false},
		{"ordered_empty_matches_only", `(?:)`, false, false},
		{"unordered_empty_matches_only", `(?:)`, true, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var cache regexMatcherCache
				_, err := cache.get(tc.pattern, tc.unordered)
				if (err == nil) != tc.valid {
					b.Fatalf("get: %v", err)
				}
			}
		})
	}
}

func BenchmarkRegexEvaluateCached(b *testing.B) {
	for _, tc := range []struct {
		name      string
		unordered bool
	}{
		{"ordered", false},
		{"unordered", true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var cache regexMatcherCache
			matcher, err := cache.get(`5p5p5p5p5p`, tc.unordered)
			if err != nil {
				b.Fatal(err)
			}
			detail := regexTestHand(b, "5p5p")
			if status, err := matcher.evaluate(detail); err != nil || status != "continuing" {
				b.Fatalf("warmup: %q, %v", status, err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if status, err := matcher.evaluate(detail); err != nil || status != "continuing" {
					b.Fatalf("evaluate: %q, %v", status, err)
				}
			}
		})
	}
}
