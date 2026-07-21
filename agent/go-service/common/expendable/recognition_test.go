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

	if _, err := parseParams(""); err == nil || err.Error() != "custom_recognition_param is empty" {
		t.Fatalf("empty raw: err=%v", err)
	}
	if _, err := parseParams("   \n\t  "); err == nil || err.Error() != "custom_recognition_param is empty" {
		t.Fatalf("whitespace-only raw: err=%v", err)
	}
	if _, err := parseParams(`{`); err == nil {
		t.Fatal("invalid JSON: expected unmarshal error")
	}
	if _, err := parseParams(`{"candidate":"   "}`); err == nil || err.Error() != "candidate is required" {
		t.Fatalf("blank candidate: err=%v", err)
	}
}


func TestNamedChild(t *testing.T) {
	t.Parallel()
	children := []json.RawMessage{
		[]byte(`"A"`),
		[]byte(`"B"`),
	}
	name, err := namedChild(children, 1)
	if err != nil || name != "B" {
		t.Fatalf("got=%q err=%v", name, err)
	}
	if _, err := namedChild([]json.RawMessage{[]byte(`{"type":"OCR"}`)}, 0); err == nil {
		t.Fatal("inline should fail")
	}

	if _, err := namedChild(children, -1); err == nil || err.Error() != "box_index -1 out of range" {
		t.Fatalf("negative index: err=%v", err)
	}
	if _, err := namedChild(children, 2); err == nil || err.Error() != "box_index 2 out of range" {
		t.Fatalf("index past end: err=%v", err)
	}
	if _, err := namedChild(nil, 0); err == nil || err.Error() != "box_index 0 out of range" {
		t.Fatalf("empty slice: err=%v", err)
	}
	if _, err := namedChild([]json.RawMessage{[]byte(`"  "`)}, 0); err == nil || err.Error() != "box_index target name is empty" {
		t.Fatalf("blank name: err=%v", err)
	}
}
