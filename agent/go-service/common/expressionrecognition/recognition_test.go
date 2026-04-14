package expressionrecognition

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
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

func TestResolveAndNodeBoxIndex(t *testing.T) {
	testCases := []struct {
		name          string
		raw           string
		wantBoxIndex  int
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
			wantBoxIndex:  1,
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
			wantBoxIndex:  0,
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
			gotBoxIndex, gotIsAndNode, err := resolveAndNodeBoxIndex(tc.raw)
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
				t.Fatalf("resolveAndNodeBoxIndex() isAndNode = %v, want %v", gotIsAndNode, tc.wantIsAndNode)
			}
			if gotBoxIndex != tc.wantBoxIndex {
				t.Fatalf("resolveAndNodeBoxIndex() boxIndex = %d, want %d", gotBoxIndex, tc.wantBoxIndex)
			}
		})
	}
}

func TestExtractAndSelectedDetail(t *testing.T) {
	detail := &maa.RecognitionDetail{
		CombinedResult: []*maa.RecognitionDetail{
			{Box: maa.Rect{1, 2, 3, 4}},
			{Box: maa.Rect{5, 6, 7, 8}},
		},
	}

	got, err := extractAndSelectedDetail(detail, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("expected detail, got nil")
	}
	if got.Box != (maa.Rect{5, 6, 7, 8}) {
		t.Fatalf("extractAndSelectedDetail() box = %v, want %v", got.Box, maa.Rect{5, 6, 7, 8})
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
