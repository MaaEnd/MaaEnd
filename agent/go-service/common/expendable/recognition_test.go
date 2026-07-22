package expendable

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestStripBlacklistPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in                 string
		allowTrailingNoise bool
		want               string
	}{
		{`.*#.*`, false, `.*#.*`},
		{`^(?!(?:foo)$).*#.*`, false, `.*#.*`},
		{`^(?!(?:a|b)$).{3,}`, false, `.{3,}`},
		{`^(?!(?:小明\(Alice\#1001\))(?:\D.*)?$).*[（(].*#.*`, true, `.*[（(].*#.*`},
		{`^(?!(?:Alice\#1001)(?:\D.*)?$).*#.*`, true, `.*#.*`},
	}
	for _, tc := range cases {
		if got := stripBlacklistPrefix(tc.in, tc.allowTrailingNoise); got != tc.want {
			t.Fatalf("stripBlacklistPrefix(%q, %v)=%q, want %q", tc.in, tc.allowTrailingNoise, got, tc.want)
		}
	}
}

func TestWithBlacklist(t *testing.T) {
	t.Parallel()
	got := withBlacklist([]string{`.*[（(].*#.*`, `.*#.*`}, []string{`备注(张三)#1234`}, false)
	want0 := `^(?!(?:备注\(张三\)#1234)$).*[（(].*#.*`
	want1 := `^(?!(?:备注\(张三\)#1234)$).*#.*`
	if len(got) != 2 || got[0] != want0 || got[1] != want1 {
		t.Fatalf("got=%q", got)
	}
	if got := withBlacklist([]string{`.{3,}`}, nil, false); len(got) != 1 || got[0] != `.{3,}` {
		t.Fatalf("empty visited got=%q", got)
	}

	// 普通 ID：截到 #UID 后，容忍尾噪，且不误伤更长 UID。
	got2 := withBlacklist([]string{`.*#.*`}, []string{`Alice#1001`}, true)
	want2 := `^(?!(?:Alice\#1001)(?:\D.*)?$).*#.*`
	if len(got2) != 1 || got2[0] != want2 {
		t.Fatalf("normal got=%q want=%q", got2, want2)
	}
	re, err := regexp.Compile(got2[0])
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, s := range []string{`Alice#1001`, `Alice#1001小`, `Alice#1001》`, `Alice#1001は`} {
		if re.MatchString(s) {
			t.Fatalf("should blacklist %q", s)
		}
	}
	if !re.MatchString(`Alice#10010`) {
		t.Fatal("should still allow Alice#10010")
	}
	if !re.MatchString(`Bob#2002`) {
		t.Fatal("should allow other friends")
	}

	// 备注：截到第一个 ) 后，容忍 ) 后尾噪。
	got3 := withBlacklist([]string{`.*[（(].*#.*`}, []string{`小明(Alice#1001)`}, true)
	want3 := `^(?!(?:小明\(Alice\#1001\))(?:\D.*)?$).*[（(].*#.*`
	if len(got3) != 1 || got3[0] != want3 {
		t.Fatalf("remark got=%q want=%q", got3, want3)
	}
	re3, err := regexp.Compile(got3[0])
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, s := range []string{`小明(Alice#1001)`, `小明(Alice#1001)尾`, `小明(Alice#1001)小`} {
		if re3.MatchString(s) {
			t.Fatalf("should blacklist %q", s)
		}
	}
	if !re3.MatchString(`小红(Bob#2002)`) {
		t.Fatal("should allow other remark friends")
	}
}

func TestApplyKeyRegex(t *testing.T) {
	t.Parallel()
	remarkKey := `.*?[)）]`
	normalKey := `.*?\#\d+`
	cases := []struct {
		text, keyRegex, want string
	}{
		{`小明(Alice#1001)`, remarkKey, `小明(Alice#1001)`},
		{`小明(Alice#1001)尾`, remarkKey, `小明(Alice#1001)`},
		{`小明(Alice#1001）多余`, remarkKey, `小明(Alice#1001）`},
		{`Alice#1001`, normalKey, `Alice#1001`},
		{`Alice#1001小`, normalKey, `Alice#1001`},
		{`Alice#1001は`, normalKey, `Alice#1001`},
		{`无括号`, remarkKey, `无括号`},
		{`Alice#1001`, ``, `Alice#1001`},
	}
	for _, tc := range cases {
		got, err := applyKeyRegex(tc.text, tc.keyRegex)
		if err != nil || got != tc.want {
			t.Fatalf("applyKeyRegex(%q, %q)=%q err=%v, want %q", tc.text, tc.keyRegex, got, err, tc.want)
		}
	}
}

func TestParseParams(t *testing.T) {
	t.Parallel()
	p, err := parseParams(`{"candidate":"Cand"}`)
	if err != nil || p.Candidate != "Cand" || p.VisitedNode != "" || p.KeyRegex != "" {
		t.Fatalf("got=%+v err=%v", p, err)
	}
	p2, err := parseParams(`{"candidate":"Cand","visited_node":"Shared","key_regex":".*?[)）]"}`)
	if err != nil || p2.Candidate != "Cand" || p2.VisitedNode != "Shared" || p2.KeyRegex != `.*?[)）]` {
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
	if _, err := parseParams(`{"candidate":"Cand","key_regex":"("}`); err == nil {
		t.Fatal("invalid key_regex: expected error")
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
