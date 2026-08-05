package ims

import (
	"testing"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
)

func TestParseEvaluateItemQuantityParam(t *testing.T) {
	if _, err := parseEvaluateItemQuantityParam(""); err == nil {
		t.Fatal("expected error for empty param")
	}
	if _, err := parseEvaluateItemQuantityParam(`{}`); err == nil {
		t.Fatal("expected error when expression missing")
	}

	params, err := parseEvaluateItemQuantityParam(`{"expression":" {CHARTERED_HH_PERMIT}+((max({ORIGEOMETRY}-29,0)*75)/500) "}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.Expression != "{CHARTERED_HH_PERMIT}+((max({ORIGEOMETRY}-29,0)*75)/500)" {
		t.Fatalf("expression = %q", params.Expression)
	}
}

func TestEvaluateItemQuantityFormula(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	MarkSynced(time.Now(), map[string]int{
		"CHARTERED_HH_PERMIT": 3,
		"ORIGEOMETRY":         20,
	})

	expr := "{CHARTERED_HH_PERMIT}+((max({ORIGEOMETRY}-29,0)*75)/500)"
	resolved, values, err := boolexpr.ResolvePlaceholders(expr, func(itemID string) (int, error) {
		return globalCache.quantity(itemID), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if values["CHARTERED_HH_PERMIT"] != 3 || values["ORIGEOMETRY"] != 20 {
		t.Fatalf("values=%v", values)
	}

	result, err := boolexpr.Evaluate(resolved)
	if err != nil {
		t.Fatal(err)
	}
	qty, ok := result.(int)
	if !ok || qty != 3 {
		// 3 + ((max(20-29,0)*75)/500) = 3 + 0 = 3
		t.Fatalf("result=%#v want 3", result)
	}

	MarkSynced(time.Now(), map[string]int{
		"CHARTERED_HH_PERMIT": 2,
		"ORIGEOMETRY":         129,
	})
	resolved, _, err = boolexpr.ResolvePlaceholders(expr, func(itemID string) (int, error) {
		return globalCache.quantity(itemID), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err = boolexpr.Evaluate(resolved)
	if err != nil {
		t.Fatal(err)
	}
	qty, ok = result.(int)
	if !ok || qty != 17 {
		// 2 + ((max(129-29,0)*75)/500) = 2 + (100*75)/500 = 2 + 15 = 17
		t.Fatalf("result=%#v want 17", result)
	}
}
