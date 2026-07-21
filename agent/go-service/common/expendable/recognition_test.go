package expendable

import (
	"encoding/json"
	"testing"
)

func TestStripBlacklistPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{`.*#.*`, `.*#.*`},
		{`^(?!(?:foo)$).*#.*`, `.*#.*`},
		{`^(?!(?:a|b)$).{3,}`, `.{3,}`},
	}
	for _, tc := range cases {
		if got := stripBlacklistPrefix(tc.in); got != tc.want {
			t.Fatalf("stripBlacklistPrefix(%q)=%q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWithBlacklist(t *testing.T) {
	t.Parallel()
	got := withBlacklist([]string{`.*[（(].*#.*`, `.*#.*`}, []string{`备注(张三)#1234`})
	want0 := `^(?!(?:备注\(张三\)#1234)$).*[（(].*#.*`
	want1 := `^(?!(?:备注\(张三\)#1234)$).*#.*`
	if len(got) != 2 || got[0] != want0 || got[1] != want1 {
		t.Fatalf("got=%q", got)
	}
	if got := withBlacklist([]string{`.{3,}`}, nil); len(got) != 1 || got[0] != `.{3,}` {
		t.Fatalf("empty visited got=%q", got)
	}
}

func TestParseParams(t *testing.T) {
	t.Parallel()
	p, err := parseParams(`{"candidate":"Cand"}`)
	if err != nil || p.Candidate != "Cand" || p.VisitedNode != "" {
		t.Fatalf("got=%+v err=%v", p, err)
	}
	p2, err := parseParams(`{"candidate":"Cand","visited_node":"Shared"}`)
	if err != nil || p2.Candidate != "Cand" || p2.VisitedNode != "Shared" {
		t.Fatalf("got=%+v err=%v", p2, err)
	}
	if _, err := parseParams(`{}`); err == nil {
		t.Fatal("expected error")
	}
}

func TestNamedChild(t *testing.T) {
	t.Parallel()
	name, err := namedChild([]json.RawMessage{
		[]byte(`"A"`),
		[]byte(`"B"`),
	}, 1)
	if err != nil || name != "B" {
		t.Fatalf("got=%q err=%v", name, err)
	}
	if _, err := namedChild([]json.RawMessage{[]byte(`{"type":"OCR"}`)}, 0); err == nil {
		t.Fatal("inline should fail")
	}
}
