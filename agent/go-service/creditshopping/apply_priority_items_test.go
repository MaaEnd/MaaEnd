package creditshopping

import "testing"

func TestPriorityItemNode(t *testing.T) {
	node := priorityItemNode([]string{"item_gold"})
	if node["recognition"] != "Custom" || node["custom_recognition"] != "IconRecognition" {
		t.Fatalf("node %#v", node)
	}
	if roi, ok := node["roi"]; ok {
		t.Fatalf("override must not pin a controller-specific roi: %v", roi)
	}
	param, ok := node["custom_recognition_param"].(map[string]any)
	if !ok {
		t.Fatal("missing custom_recognition_param")
	}
	assertStringList(t, param["item_ids"], []string{"item_gold"})
	if param["grid_type"] != "credit_trade" || param["order_by"] != "natural" || param["deduplicate"] != true {
		t.Fatalf("param %#v", param)
	}
}

func TestRecognitionItemIDs(t *testing.T) {
	got, err := recognitionItemIDs([]string{"Protohedron", "ArmsINSPKit", "Protohedron"})
	if err != nil {
		t.Fatal(err)
	}
	assertStringList(t, got, []string{"item_char_skill_level_7_12", "item_weapon_expcard_mid"})
}

func TestRecognitionItemIDsUnknown(t *testing.T) {
	if _, err := recognitionItemIDs([]string{"NotAnItem"}); err == nil {
		t.Fatal("expected error for unknown item")
	}
}

func TestParseAttachKey(t *testing.T) {
	level, caseID, ok := parseAttachKey("priority2__Protohedron")
	if !ok || level != 2 || caseID != "Protohedron" {
		t.Fatalf("level=%d case=%q ok=%v", level, caseID, ok)
	}
	if _, _, ok := parseAttachKey("other__Protohedron"); ok {
		t.Fatal("non-priority key must not match")
	}
	if _, _, ok := parseAttachKey("priority1__"); ok {
		t.Fatal("empty case id must not match")
	}
}

func TestAttachEnabled(t *testing.T) {
	for raw, want := range map[string]bool{
		`"TCreds"`: true,
		`true`:     true,
		`""`:       false,
		`"false"`:  false,
		`null`:     false,
	} {
		if got := attachEnabled([]byte(raw)); got != want {
			t.Fatalf("attachEnabled(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestPriorityItemNodeName(t *testing.T) {
	if name := priorityItemNodeName(2); name != "CreditShoppingPriority2Item" {
		t.Fatalf("name %q", name)
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
