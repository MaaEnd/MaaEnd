package boolexpr

import (
	"fmt"
	"testing"
)

func TestParseIntLiteral(t *testing.T) {
	testCases := []struct {
		name string
		raw  string
		want int
	}{
		{
			name: "plain integer",
			raw:  "300000000",
			want: 300000000,
		},
		{
			name: "positive overflow clamps to max int",
			raw:  "99999999999999999999999999999",
			want: IntMax,
		},
		{
			name: "negative overflow clamps to min int",
			raw:  "-99999999999999999999999999999",
			want: IntMin,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseIntLiteral(tc.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("ParseIntLiteral(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestEvaluateOverflowLiteral(t *testing.T) {
	result, err := Evaluate("99999999999999999999999999999 < 7920000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	matched, ok := result.(bool)
	if !ok {
		t.Fatalf("expected bool result, got %T", result)
	}
	if matched {
		t.Fatalf("expected overflow literal comparison to be false, got true")
	}
}

func TestResolvePlaceholders(t *testing.T) {
	resolved, values, err := ResolvePlaceholders(
		"({A}+{B})>=10 && {C}==1",
		func(name string) (int, error) {
			switch name {
			case "A":
				return 3, nil
			case "B":
				return 7, nil
			case "C":
				return 1, nil
			default:
				return 0, fmt.Errorf("unknown %s", name)
			}
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "(3+7)>=10 && 1==1" {
		t.Fatalf("resolved = %q", resolved)
	}
	if values["A"] != 3 || values["B"] != 7 || values["C"] != 1 {
		t.Fatalf("values = %#v", values)
	}

	result, err := Evaluate(resolved)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	matched, ok := result.(bool)
	if !ok || !matched {
		t.Fatalf("expected true, got %#v", result)
	}
}

func TestResolvePlaceholdersEmptyName(t *testing.T) {
	if _, _, err := ResolvePlaceholders("{ }", func(string) (int, error) {
		return 0, nil
	}); err == nil {
		t.Fatal("expected error for empty placeholder")
	}
}
