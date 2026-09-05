package autoessence

import (
	"regexp"
	"strings"
	"testing"
)

func TestCombinedBaseExpectedPatterns(t *testing.T) {
	entries := [3]skillEntry{
		{CN: "力量", TC: "力量", EN: "Strength", JP: "筋力", KR: "힘"},
		{CN: "意志", TC: "意志", EN: "Will", JP: "意志", KR: "의지"},
		{CN: "敏捷", TC: "敏捷", EN: "Agility", JP: "敏捷", KR: "민첩"},
	}

	got := combinedBaseExpectedPatterns(entries)
	if len(got) != 6 {
		t.Fatalf("expected 6 regex patterns, got %d: %#v", len(got), got)
	}

	for _, text := range []string{
		"意志/力量/敏捷",
		"Strength/Will/Agility",
		"筋力/意志/민첩",
	} {
		if !matchesAnyPattern(t, got, text) {
			t.Fatalf("cross-locale regex should match %q, patterns=%#v", text, got)
		}
	}

	for _, pattern := range got {
		if !strings.Contains(pattern, "(?:") {
			t.Fatalf("expected regex-only patterns, got literal-like %q", pattern)
		}
	}
}

func TestSkillExpectedTextsCoversAllLocales(t *testing.T) {
	entry := skillEntry{CN: "攻击", TC: "攻擊", EN: "ATK", JP: "攻撃力", KR: "공격력"}
	got := skillExpectedTexts(entry)
	want := []string{"攻击", "攻擊", "ATK", "攻撃力", "공격력"}

	if len(got) != len(want) {
		t.Fatalf("expected %d locale texts, got %#v", len(want), got)
	}
	for _, expected := range want {
		if !containsString(got, expected) {
			t.Fatalf("missing locale text %q in %#v", expected, got)
		}
	}
}

func TestBuildCrossLocaleCombinedBaseRegexesMatchesAnyOrder(t *testing.T) {
	entries := [3]skillEntry{
		{CN: "力量", EN: "Strength"},
		{CN: "意志", EN: "Will"},
		{CN: "敏捷", EN: "Agility"},
	}

	for _, text := range []string{
		"力量/意志/敏捷",
		"意志/力量/敏捷",
		"Strength/Will/Agility",
		"Will/Strength/Agility",
	} {
		if !matchesAnyPattern(t, buildCrossLocaleCombinedBaseRegexes(entries), text) {
			t.Fatalf("no cross-locale regex matched %q", text)
		}
	}
}

func matchesAnyPattern(t *testing.T, patterns []string, text string) bool {
	t.Helper()

	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("compile pattern %q: %v", pattern, err)
		}
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

func TestBuildLocationEngraveOverride(t *testing.T) {
	selection := baseEngraveSelection{
		base1: skillEntry{CN: "力量", TC: "力量", EN: "Strength", JP: "筋力", KR: "힘"},
		base2: skillEntry{CN: "意志", TC: "意志", EN: "Will", JP: "意志", KR: "의지"},
		base3: skillEntry{CN: "敏捷", TC: "敏捷", EN: "Agility", JP: "敏捷", KR: "민첩"},
	}

	override := buildLocationEngraveOverride(selection)
	if len(override) != 1 {
		t.Fatalf("expected 1 node override, got %d", len(override))
	}

	cond1 := expectedFromOverride(t, override, nodeEngraveCondition1OCR)
	if !matchesAnyPattern(t, cond1, "Strength/Will/Agility") {
		t.Fatalf("condition1 cross-locale regex missing in %#v", cond1)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func expectedFromOverride(t *testing.T, override map[string]any, nodeName string) []string {
	t.Helper()

	nodeOverride, ok := override[nodeName].(map[string]any)
	if !ok {
		t.Fatalf("node override %q is missing", nodeName)
	}
	recognition, ok := nodeOverride["recognition"].(map[string]any)
	if !ok {
		t.Fatalf("node override %q recognition is missing", nodeName)
	}
	param, ok := recognition["param"].(map[string]any)
	if !ok {
		t.Fatalf("node override %q recognition.param is missing", nodeName)
	}
	expected, ok := param["expected"].([]string)
	if !ok {
		t.Fatalf("node override %q expected is missing", nodeName)
	}
	return expected
}
