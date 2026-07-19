package recogtarget

import (
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func TestResolveAndBoxIndex(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		raw           string
		wantBoxIndex  int
		wantIsAndNode bool
		wantErr       bool
	}{
		{
			name: "v2 and uses box index target",
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
			name: "v2 and defaults to first child",
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
			name: "flat and uses top-level box_index",
			raw: `{
				"recognition": "And",
				"all_of": ["A", "B", "C"],
				"box_index": 2
			}`,
			wantBoxIndex:  2,
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotBoxIndex, gotIsAndNode, err := ResolveAndBoxIndex(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotIsAndNode != tc.wantIsAndNode {
				t.Fatalf("isAndNode = %v, want %v", gotIsAndNode, tc.wantIsAndNode)
			}
			if gotBoxIndex != tc.wantBoxIndex {
				t.Fatalf("boxIndex = %d, want %d", gotBoxIndex, tc.wantBoxIndex)
			}
		})
	}
}

func TestSelectedDetail(t *testing.T) {
	t.Parallel()

	detail := &maa.RecognitionDetail{
		CombinedResult: []*maa.RecognitionDetail{
			{Box: maa.Rect{1, 2, 3, 4}},
			{Box: maa.Rect{5, 6, 7, 8}},
		},
	}

	got, err := SelectedDetail(detail, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected detail, got nil")
	}
	if got.Box != (maa.Rect{5, 6, 7, 8}) {
		t.Fatalf("box = %v, want %v", got.Box, maa.Rect{5, 6, 7, 8})
	}
}

func TestSelectDetailFromJSON(t *testing.T) {
	t.Parallel()

	detail := &maa.RecognitionDetail{
		Box: maa.Rect{9, 9, 1, 1},
		CombinedResult: []*maa.RecognitionDetail{
			{Box: maa.Rect{1, 2, 3, 4}},
			{Box: maa.Rect{5, 6, 7, 8}},
		},
	}

	andRaw := `{
		"recognition":"And",
		"all_of":["A","B"],
		"box_index":1
	}`
	got, err := SelectDetailFromJSON([]byte(andRaw), detail)
	if err != nil {
		t.Fatalf("and select: %v", err)
	}
	if got.Box != (maa.Rect{5, 6, 7, 8}) {
		t.Fatalf("and box = %v, want child", got.Box)
	}

	ocrRaw := `{"recognition":"OCR","expected":["x"]}`
	got, err = SelectDetailFromJSON([]byte(ocrRaw), detail)
	if err != nil {
		t.Fatalf("ocr select: %v", err)
	}
	if got.Box != (maa.Rect{9, 9, 1, 1}) {
		t.Fatalf("ocr box = %v, want self", got.Box)
	}
}

func TestEffectiveTypeFromJSON(t *testing.T) {
	t.Parallel()

	got, err := effectiveTypeFromJSON(nil, []byte(`{
		"recognition":"And",
		"all_of":[
			{"recognition":"TemplateMatch"},
			{"recognition":"OCR","expected":["x"]}
		],
		"box_index":1
	}`), map[string]struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "OCR" {
		t.Fatalf("type = %q, want OCR", got)
	}

	got, err = effectiveTypeFromJSON(nil, []byte(`{
		"recognition":"And",
		"all_of":[
			{"recognition":"TemplateMatch"},
			{"recognition":"OCR","expected":["x"]}
		],
		"box_index":0
	}`), map[string]struct{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "TemplateMatch" {
		t.Fatalf("type = %q, want TemplateMatch", got)
	}
}
