package expendable

import (
	"testing"
)

type stubNodeJSON map[string]string

func (s stubNodeJSON) GetNodeJSON(nodeName string) (string, error) {
	raw, ok := s[nodeName]
	if !ok {
		return "", &nodeMissingError{name: nodeName}
	}
	return raw, nil
}

type nodeMissingError struct {
	name string
}

func (e *nodeMissingError) Error() string {
	return "node not found: " + e.name
}

func TestStripVisitedExclusionPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{`.*#.*`, `.*#.*`},
		{`^(?!(?:foo)$).*#.*`, `.*#.*`},
		{`^(?!(?:a|b)$).{3,}`, `.{3,}`},
		{`^(?!(?:备注\(张三\))$).*[（(].*#.*`, `.*[（(].*#.*`},
	}
	for _, tc := range cases {
		got := stripVisitedExclusionPrefix(tc.in)
		if got != tc.want {
			t.Fatalf("stripVisitedExclusionPrefix(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestApplyVisitedExclusion(t *testing.T) {
	t.Parallel()

	got := applyVisitedExclusion([]string{`.*[（(].*#.*`, `.*#.*`}, []string{`备注(张三)#1234`})
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	want0 := `^(?!(?:备注\(张三\)#1234)$).*[（(].*#.*`
	want1 := `^(?!(?:备注\(张三\)#1234)$).*#.*`
	if got[0] != want0 || got[1] != want1 {
		t.Fatalf("got=%q", got)
	}

	gotEmpty := applyVisitedExclusion([]string{`.{3,}`}, nil)
	if len(gotEmpty) != 1 || gotEmpty[0] != `.{3,}` {
		t.Fatalf("empty visited got=%q", gotEmpty)
	}
}

func TestParseParams(t *testing.T) {
	t.Parallel()

	p, err := parseParams(`{"candidate":"Cand"}`)
	if err != nil {
		t.Fatal(err)
	}
	if p.Candidate != "Cand" {
		t.Fatalf("unexpected params: %+v", p)
	}

	if _, err := parseParams(`{}`); err == nil {
		t.Fatal("expected error for empty params")
	}
}

func TestDiscoverOCRNodes(t *testing.T) {
	t.Parallel()

	src := stubNodeJSON{
		"Cand": `{
			"recognition":"Or",
			"any_of":["ByRed","ByNew"]
		}`,
		"ByRed": `{
			"recognition":"And",
			"all_of":["RedDot","TextRed"]
		}`,
		"ByNew": `{
			"recognition":"And",
			"all_of":["NewTag","TextNew"]
		}`,
		"RedDot":  `{"recognition":"ColorMatch"}`,
		"NewTag":  `{"recognition":"OCR","expected":["NEW"]}`,
		"TextRed": `{"recognition":{"type":"OCR","param":{"expected":[".{3,}"]}}}`,
		"TextNew": `{"recognition":"OCR","expected":[".{3,}"]}`,
		"Friend": `{
			"recognition":"And",
			"all_of":["Btn","Clue","Name"],
			"box_index":2
		}`,
		"Btn":  `{"recognition":"TemplateMatch"}`,
		"Clue": `{"recognition":"ColorMatch"}`,
		"Name": `{"recognition":"OCR","expected":[".*#.*"]}`,
	}

	got, err := discoverOCRNodesRec(src, "Cand", map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0] != "TextRed" || got[1] != "NewTag" || got[2] != "TextNew" {
		t.Fatalf("daily-like discover = %v", got)
	}

	gotFriend, err := discoverOCRNodesRec(src, "Friend", map[string]struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	if len(gotFriend) != 1 || gotFriend[0] != "Name" {
		t.Fatalf("friend-like discover = %v", gotFriend)
	}
}
