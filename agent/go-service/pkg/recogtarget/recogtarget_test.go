package recogtarget

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

type stubNodeJSON map[string]string

func (s stubNodeJSON) GetNodeJSON(nodeName string) (string, error) {
	raw, ok := s[nodeName]
	if !ok {
		return "", fmt.Errorf("node not found: %s", nodeName)
	}
	return raw, nil
}

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
		{
			name:    "invalid json",
			raw:     `{not-json`,
			wantErr: true,
		},
		{
			name: "missing recognition type",
			raw: `{
				"all_of": ["A"]
			}`,
			wantErr: true,
		},
		{
			name: "and node empty all_of",
			raw: `{
				"recognition": "And",
				"all_of": []
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

func TestParseNodeJSON_ErrorPaths(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name:    "malformed json",
			raw:     `{"recognition":`,
			wantErr: "unmarshal node json",
		},
		{
			name:    "missing recognition",
			raw:     `{"enabled": true}`,
			wantErr: "recognition type is missing",
		},
		{
			name:    "null recognition without type",
			raw:     `{"recognition": null}`,
			wantErr: "recognition type is missing",
		},
		{
			name:    "invalid all_of type",
			raw:     `{"recognition":"And","all_of":"oops"}`,
			wantErr: "unmarshal all_of",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseNodeJSON([]byte(tc.raw))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantErr)
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

func TestSelectDetailFromFields_ErrorPaths(t *testing.T) {
	t.Parallel()

	andFields := Fields{
		Type:     "And",
		AllOf:    []json.RawMessage{[]byte(`"A"`), []byte(`"B"`)},
		BoxIndex: 0,
	}

	t.Run("nil detail", func(t *testing.T) {
		t.Parallel()
		got, err := SelectDetailFromFields(andFields, nil)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if got != nil {
			t.Fatalf("got = %#v, want nil result on error", got)
		}
	})

	t.Run("empty combined result", func(t *testing.T) {
		t.Parallel()
		got, err := SelectDetailFromFields(andFields, &maa.RecognitionDetail{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "combined result is empty") {
			t.Fatalf("error = %q, want combined result is empty", err.Error())
		}
		if got != nil {
			t.Fatalf("got = %#v, want nil result on error", got)
		}
	})

	t.Run("nil combined entry", func(t *testing.T) {
		t.Parallel()
		got, err := SelectDetailFromFields(andFields, &maa.RecognitionDetail{
			CombinedResult: []*maa.RecognitionDetail{nil, {Box: maa.Rect{1, 2, 3, 4}}},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "result is empty") {
			t.Fatalf("error = %q, want result is empty", err.Error())
		}
		if got != nil {
			t.Fatalf("got = %#v, want nil result on error", got)
		}
	})

	t.Run("box index beyond combined result", func(t *testing.T) {
		t.Parallel()
		fields := Fields{
			Type:     "And",
			AllOf:    []json.RawMessage{[]byte(`"A"`), []byte(`"B"`)},
			BoxIndex: 1,
		}
		got, err := SelectDetailFromFields(fields, &maa.RecognitionDetail{
			CombinedResult: []*maa.RecognitionDetail{
				{Box: maa.Rect{1, 2, 3, 4}},
			},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if got != nil {
			t.Fatalf("got = %#v, want nil result on error", got)
		}
	})
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

func TestEffectiveType_NodeNameRefAndCycle(t *testing.T) {
	t.Parallel()

	t.Run("resolves node name reference", func(t *testing.T) {
		t.Parallel()
		nodes := stubNodeJSON{
			"ParentAnd": `{
				"recognition":"And",
				"all_of":["FooNode","OtherNode"],
				"box_index":0
			}`,
			"FooNode": `{
				"recognition":"OCR",
				"expected":["x"]
			}`,
			"OtherNode": `{
				"recognition":"TemplateMatch"
			}`,
		}
		got, err := effectiveType(nodes, "ParentAnd", map[string]struct{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "OCR" {
			t.Fatalf("type = %q, want OCR", got)
		}
	})

	t.Run("follows nested and node name chain", func(t *testing.T) {
		t.Parallel()
		nodes := stubNodeJSON{
			"Outer": `{
				"recognition":"And",
				"all_of":["Inner"],
				"box_index":0
			}`,
			"Inner": `{
				"recognition":"And",
				"all_of":[{"type":"OCR","param":{"expected":["n"]}}],
				"box_index":0
			}`,
		}
		got, err := effectiveType(nodes, "Outer", map[string]struct{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "OCR" {
			t.Fatalf("type = %q, want OCR", got)
		}
	})

	t.Run("detects cyclic all_of references", func(t *testing.T) {
		t.Parallel()
		nodes := stubNodeJSON{
			"A": `{
				"recognition":"And",
				"all_of":["B"],
				"box_index":0
			}`,
			"B": `{
				"recognition":"And",
				"all_of":["A"],
				"box_index":0
			}`,
		}
		got, err := effectiveType(nodes, "A", map[string]struct{}{})
		if err == nil {
			t.Fatal("expected cycle error, got nil")
		}
		if !strings.Contains(err.Error(), "cyclic and references") {
			t.Fatalf("error = %q, want cyclic and references", err.Error())
		}
		if got != "" {
			t.Fatalf("type = %q, want empty on error", got)
		}
	})

	t.Run("missing node name reference", func(t *testing.T) {
		t.Parallel()
		nodes := stubNodeJSON{
			"Parent": `{
				"recognition":"And",
				"all_of":["MissingNode"],
				"box_index":0
			}`,
		}
		_, err := effectiveType(nodes, "Parent", map[string]struct{}{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "MissingNode") {
			t.Fatalf("error = %q, want MissingNode", err.Error())
		}
	})

	t.Run("node name ref requires source", func(t *testing.T) {
		t.Parallel()
		_, err := effectiveTypeFromJSON(nil, []byte(`{
			"recognition":"And",
			"all_of":["FooNode"],
			"box_index":0
		}`), map[string]struct{}{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "context is nil") {
			t.Fatalf("error = %q, want context is nil", err.Error())
		}
	})
}
