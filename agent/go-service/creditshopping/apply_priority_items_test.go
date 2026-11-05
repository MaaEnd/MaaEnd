package creditshopping

import "testing"

func TestBuildPriorityLevelOverrideIconRecognition(t *testing.T) {
	patch, err := buildPriorityLevelOverride(2, []string{"Protohedron", "ArmsINSPKit", "Protohedron"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := patch["CreditShoppingPriority2ItemArmsINSPKit"]; ok {
		t.Fatal("per-item nodes should not be patched")
	}

	node, ok := patch["CreditShoppingPriority2Item"].(map[string]any)
	if !ok {
		t.Fatal("missing CreditShoppingPriority2Item")
	}
	if node["recognition"] != "Custom" {
		t.Fatalf("recognition %v", node["recognition"])
	}
	if node["custom_recognition"] != "IconRecognition" {
		t.Fatalf("custom_recognition %v", node["custom_recognition"])
	}
	param, ok := node["custom_recognition_param"].(map[string]any)
	if !ok {
		t.Fatal("missing custom_recognition_param")
	}
	assertStringList(t, param["item_ids"], []string{
		"item_weapon_expcard_mid",
		"item_char_skill_level_7_12",
	})
	if param["grid_type"] != "credit_trade" || param["order_by"] != "natural" {
		t.Fatalf("param %#v", param)
	}
}

func TestBuildPriorityLevelOverrideEmpty(t *testing.T) {
	patch, err := buildPriorityLevelOverride(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch) != 0 {
		t.Fatalf("empty selection should not patch, got %#v", patch)
	}
}

func TestBuildPriorityLevelOverrideUnknownID(t *testing.T) {
	_, err := buildPriorityLevelOverride(1, []string{"NotAnItem"})
	if err == nil {
		t.Fatal("expected error for unknown item id")
	}
}

func TestParseAttachPriorityKey(t *testing.T) {
	level, id, ok := parseAttachPriorityKey("priority2__Protohedron")
	if !ok || level != 2 || id != "Protohedron" {
		t.Fatalf("got level=%d id=%q ok=%v", level, id, ok)
	}
}

func assertStringList(t *testing.T, got any, want []string) {
	t.Helper()
	list, ok := got.([]string)
	if !ok {
		t.Fatalf("got type %T (%#v), want %v", got, got, want)
	}
	if len(list) != len(want) {
		t.Fatalf("len %d want %d (%v)", len(list), len(want), list)
	}
	for i := range want {
		if list[i] != want[i] {
			t.Fatalf("[%d]=%q want %q", i, list[i], want[i])
		}
	}
}
