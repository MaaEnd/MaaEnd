package creditshopping

import (
	"testing"
)

func TestBuildPriorityLevelOverrideOrderAndDisable(t *testing.T) {
	patch, err := buildPriorityLevelOverride(2, []string{"Protohedron", "ArmsINSPKit", "Protohedron"})
	if err != nil {
		t.Fatal(err)
	}

	orNode, ok := patch["CreditShoppingPriority2Item"].(map[string]any)
	if !ok {
		t.Fatal("missing Or parent node")
	}
	anyOf, ok := orNode["any_of"].([]string)
	if !ok {
		t.Fatalf("any_of type %T", orNode["any_of"])
	}
	wantAnyOf := []string{
		"CreditShoppingPriority2ItemProtohedron",
		"CreditShoppingPriority2ItemArmsINSPKit",
	}
	if len(anyOf) != len(wantAnyOf) {
		t.Fatalf("any_of len %d want %d", len(anyOf), len(wantAnyOf))
	}
	for i := range wantAnyOf {
		if anyOf[i] != wantAnyOf[i] {
			t.Fatalf("any_of[%d]=%q want %q", i, anyOf[i], wantAnyOf[i])
		}
	}

	enabled, ok := patch["CreditShoppingPriority2ItemProtohedron"].(map[string]any)
	if !ok || enabled["recognition"] != "TemplateMatch" {
		t.Fatal("Protohedron should be TemplateMatch enabled")
	}
	disabled, ok := patch["CreditShoppingPriority2ItemArmsInspector"].(map[string]any)
	if !ok || disabled["recognition"] != "ColorMatch" {
		t.Fatal("ArmsInspector should be disabled ColorMatch stub")
	}
}

func TestBuildPriorityLevelOverrideUnknownID(t *testing.T) {
	_, err := buildPriorityLevelOverride(1, []string{"NotAnItem"})
	if err == nil {
		t.Fatal("expected error for unknown item id")
	}
}
