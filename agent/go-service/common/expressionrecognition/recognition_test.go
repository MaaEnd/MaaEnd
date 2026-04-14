package expressionrecognition

import "testing"

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

func TestResolveAndNodeBoxTarget(t *testing.T) {
	testCases := []struct {
		name          string
		raw           string
		wantNode      string
		wantIsAndNode bool
		wantErr       bool
	}{
		{
			name: "and node uses box index target",
			raw: `{
				"recognition": {
					"type": "And",
					"param": {
						"all_of": ["ColorNode", "TextNode"],
						"box_index": 1
					}
				}
			}`,
			wantNode:      "TextNode",
			wantIsAndNode: true,
		},
		{
			name: "and node defaults to first child",
			raw: `{
				"recognition": {
					"type": "And",
					"param": {
						"all_of": ["FirstNode", "SecondNode"]
					}
				}
			}`,
			wantNode:      "FirstNode",
			wantIsAndNode: true,
		},
		{
			name: "non and node ignored",
			raw: `{
				"recognition": {
					"type": "OCR",
					"param": {
						"expected": ["\\d+"]
					}
				}
			}`,
			wantIsAndNode: false,
		},
		{
			name: "and node rejects inline child",
			raw: `{
				"recognition": {
					"type": "And",
					"param": {
						"all_of": [
							{"type": "OCR", "param": {"expected": ["\\d+"]}}
						]
					}
				}
			}`,
			wantErr: true,
		},
		{
			name: "and node rejects out of range index",
			raw: `{
				"recognition": {
					"type": "And",
					"param": {
						"all_of": ["OnlyNode"],
						"box_index": 1
					}
				}
			}`,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotNode, gotIsAndNode, err := resolveAndNodeBoxTarget(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotIsAndNode != tc.wantIsAndNode {
				t.Fatalf("resolveAndNodeBoxTarget() isAndNode = %v, want %v", gotIsAndNode, tc.wantIsAndNode)
			}
			if gotNode != tc.wantNode {
				t.Fatalf("resolveAndNodeBoxTarget() node = %q, want %q", gotNode, tc.wantNode)
			}
		})
	}
}
