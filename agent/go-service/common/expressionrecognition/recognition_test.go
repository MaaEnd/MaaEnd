package expressionrecognition

import (
	"testing"
)

func TestParseOCRNumericValue(t *testing.T) {
	testCases := []struct {
		name    string
		text    string
		want    int
		wantErr bool
	}{
		{
			name: "plain integer",
			text: "138",
			want: 138,
		},
		{
			name: "chinese ten thousand suffix",
			text: "1.38万",
			want: 13800,
		},
		{
			name: "western thousand suffix",
			text: "13.8K",
			want: 13800,
		},
		{
			name: "western million suffix",
			text: "22.01M",
			want: 22010000,
		},
		{
			name: "decimal comma suffix",
			text: "13,8K",
			want: 13800,
		},
		{
			name:    "unsupported w suffix",
			text:    "1.2W",
			wantErr: true,
		},
		{
			name: "embedded numeric token",
			text: "约 1.38万",
			want: 13800,
		},
		{
			name:    "invalid text",
			text:    "abc",
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOCRNumericValue(tc.text)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseOCRNumericValue(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}

func TestParseParamsTrimsBoxNode(t *testing.T) {
	params, err := parseParams(`{"expression":"{NodeA}<{NodeB}","box_node":"  NodeA  "}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.BoxNode != "NodeA" {
		t.Fatalf("parseParams() boxNode = %q, want %q", params.BoxNode, "NodeA")
	}
}

func TestParseExpressionIntLiteral(t *testing.T) {
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
			want: expressionIntMax,
		},
		{
			name: "negative overflow clamps to min int",
			raw:  "-99999999999999999999999999999",
			want: expressionIntMin,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseExpressionIntLiteral(tc.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseExpressionIntLiteral(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

func TestEvaluateExpressionOverflowLiteral(t *testing.T) {
	result, err := evaluateExpression("99999999999999999999999999999 < 7920000")
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

func TestParseOCRNumericValueClampsOverflow(t *testing.T) {
	got, err := parseOCRNumericValue("99999999999999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expressionIntMax {
		t.Fatalf("parseOCRNumericValue() = %d, want %d", got, expressionIntMax)
	}
}
