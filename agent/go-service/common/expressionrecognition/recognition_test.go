package expressionrecognition

import (
	"testing"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/boolexpr"
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
			name: "traditional chinese ten thousand suffix",
			text: "1.38萬",
			want: 13800,
		},
		{
			name: "korean ten thousand suffix",
			text: "1.38만",
			want: 13800,
		},
		{
			name: "traditional chinese hundred million suffix",
			text: "1.2億",
			want: 120000000,
		},
		{
			name: "korean hundred million suffix",
			text: "1.2억",
			want: 120000000,
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
	params, err := parseParams(`{
		"expression":"{NodeA}<{Minimum}",
		"box_node":"  NodeA  ",
		"constants":{"  Minimum  ":" 11.9万 "}
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params.BoxNode != "NodeA" {
		t.Fatalf("parseParams() boxNode = %q, want %q", params.BoxNode, "NodeA")
	}
	if params.Constants["Minimum"] != "11.9万" {
		t.Fatalf("parseParams() constants = %#v, want trimmed Minimum value", params.Constants)
	}
}

func TestResolveExpressionPlaceholdersUsesConstantsBeforeNodes(t *testing.T) {
	params, err := parseParams(`{
		"expression":"{SelectedBid}>={MinimumTransferQuote}",
		"constants":{"MinimumTransferQuote":"11.9万"}
	}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	resolved, values, err := resolveExpressionPlaceholders(params, func(name string) (int, error) {
		if name == "MinimumTransferQuote" {
			t.Fatalf("constant %q must not execute a recognition node", name)
		}
		return 211000, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "211000>=119000" {
		t.Fatalf("resolved expression = %q, want %q", resolved, "211000>=119000")
	}
	if values["SelectedBid"] != 211000 || values["MinimumTransferQuote"] != 119000 {
		t.Fatalf("resolved values = %#v, want selected bid and constant", values)
	}
}

func TestParseParamsRejectsInvalidConstant(t *testing.T) {
	_, err := parseParams(`{
		"expression":"{NodeA}>={Minimum}",
		"constants":{"Minimum":"not-a-number"}
	}`)
	if err == nil {
		t.Fatal("expected invalid constant error, got nil")
	}
}

func TestParseOCRNumericValueClampsOverflow(t *testing.T) {
	got, err := parseOCRNumericValue("99999999999999999999999999999")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != boolexpr.IntMax {
		t.Fatalf("parseOCRNumericValue() = %d, want %d", got, boolexpr.IntMax)
	}
}
