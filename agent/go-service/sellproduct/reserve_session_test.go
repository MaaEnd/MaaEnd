package sellproduct

import (
	"reflect"
	"testing"
)

func TestReserveSessionRulesAreIndependentFromPriority(t *testing.T) {
	resetReserveSession()
	if replaced := registerReserveRule("item_useful", 120); replaced {
		t.Fatal("first registration must not replace a rule")
	}
	setSelectedReserveItem("item_other")
	if _, _, configured := selectedReserveRule(); configured {
		t.Fatal("unconfigured selected item must use default selling")
	}
	setSelectedReserveItem("item_useful")
	itemID, quantity, configured := selectedReserveRule()
	if itemID != "item_useful" || quantity != 120 || !configured {
		t.Fatalf("unexpected selected rule: item=%q quantity=%d configured=%v", itemID, quantity, configured)
	}
}

func TestReserveSessionLaterRuleOverridesEarlierRule(t *testing.T) {
	resetReserveSession()
	registerReserveRule("item_useful", 100)
	if replaced := registerReserveRule("item_useful", 250); !replaced {
		t.Fatal("duplicate registration should replace the previous slot")
	}
	setSelectedReserveItem("item_useful")
	_, quantity, configured := selectedReserveRule()
	if quantity != 250 || !configured {
		t.Fatalf("later rule not applied: quantity=%d configured=%v", quantity, configured)
	}
}

func TestReserveSessionZeroMeansSellAll(t *testing.T) {
	resetReserveSession()
	registerReserveRule("item_useful", 0)
	setSelectedReserveItem("item_useful")
	_, quantity, configured := selectedReserveRule()
	if quantity != 0 || configured {
		t.Fatalf("zero reserve should use default selling: quantity=%d configured=%v", quantity, configured)
	}
}

func TestBuildReserveSlidingOverride(t *testing.T) {
	withReserve := buildReserveSlidingOverride("Sliding", 88, true)
	wantWithReserve := map[string]any{
		"Sliding": map[string]any{
			"next": []string{"SellProductSkipToNextSellLoop", "SellProductSellThenLoop"},
			"attach": map[string]any{
				"Target":        88,
				"TargetReverse": true,
			},
		},
	}
	if !reflect.DeepEqual(withReserve, wantWithReserve) {
		t.Fatalf("unexpected reserve override: %#v", withReserve)
	}

	withoutReserve := buildReserveSlidingOverride("Sliding", 0, false)
	wantWithoutReserve := map[string]any{
		"Sliding": map[string]any{
			"next": []string{"SellProductSell"},
			"attach": map[string]any{
				"Target":        999999,
				"TargetReverse": false,
			},
		},
	}
	if !reflect.DeepEqual(withoutReserve, wantWithoutReserve) {
		t.Fatalf("unexpected default override: %#v", withoutReserve)
	}
}

func TestParseReserveSessionActionParam(t *testing.T) {
	param, err := parseReserveSessionActionParam(`{"operation":"register","item_id":"item_a","quantity":12}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if param.ItemID != "item_a" || param.Quantity != 12 {
		t.Fatalf("unexpected params: %+v", param)
	}
	param, err = parseReserveSessionActionParam(`{"operation":"register","quantity":19000}`)
	if err != nil {
		t.Fatalf("register params may load item id from attach: %v", err)
	}
	if param.ItemID != "" || param.Quantity != 19000 {
		t.Fatalf("unexpected attach-backed params: %+v", param)
	}
	if _, err := parseReserveSessionActionParam(`{"operation":"apply"}`); err == nil {
		t.Fatal("apply without sliding_node should fail")
	}
	if _, err := parseReserveSessionActionParam(`{"operation":"select","item_id":"item_a"}`); err != nil {
		t.Fatalf("valid select params should pass: %v", err)
	}
	if _, err := parseReserveSessionActionParam(`{"operation":"register","item_id":"item_a","quantity":-1}`); err == nil {
		t.Fatal("negative quantity should fail")
	}
	if _, err := parseReserveSessionActionParam(`{"operation":"register","quantity":"19000"}`); err == nil {
		t.Fatal("string quantity should fail")
	}
}

func TestParseReserveItemIDAttach(t *testing.T) {
	itemID, err := parseReserveItemIDAttach(`{"attach":{"item_id":" item_a "}}`, "RegisterRule")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if itemID != "item_a" {
		t.Fatalf("unexpected item id: %q", itemID)
	}
	itemID, err = parseReserveItemIDAttach(`{"attach":{}}`, "RegisterRule")
	if err != nil || itemID != "" {
		t.Fatalf("empty attach should represent an unconfigured slot: item=%q err=%v", itemID, err)
	}
}
