package expendable

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestStripBlacklistPrefix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, prefix, suffix, want string
	}{
		{`.*#.*`, ``, ``, `.*#.*`},
		{`^(?!(?:foo)$).*#.*`, ``, ``, `.*#.*`},
		{`^(?!(?:a|b)$).{3,}`, ``, ``, `.{3,}`},
		{`^(?!(?:Alice\#1001)(?:\D.*)?$).*#.*`, ``, `(?:\D.*)?`, `.*#.*`},
		{`^(?!.*(?:Alice\#1001)(?:\D.*)?$).*#.*`, `.*`, `(?:\D.*)?`, `.*#.*`},
	}
	for _, tc := range cases {
		if got := stripBlacklistPrefix(tc.in, tc.prefix, tc.suffix); got != tc.want {
			t.Fatalf("stripBlacklistPrefix(%q, %q, %q)=%q, want %q", tc.in, tc.prefix, tc.suffix, got, tc.want)
		}
	}
}

func TestWithBlacklist(t *testing.T) {
	t.Parallel()
	got := withBlacklist([]string{`.*[（(].*#.*`, `.*#.*`}, []string{`备注(张三)#1234`}, "", "")
	want0 := `^(?!(?:备注\(张三\)#1234)$).*[（(].*#.*`
	want1 := `^(?!(?:备注\(张三\)#1234)$).*#.*`
	if len(got) != 2 || got[0] != want0 || got[1] != want1 {
		t.Fatalf("got=%q", got)
	}
	if got := withBlacklist([]string{`.{3,}`}, nil, "", ""); len(got) != 1 || got[0] != `.{3,}` {
		t.Fatalf("empty visited got=%q", got)
	}

	suffix := `(?:\D.*)?`
	got2 := withBlacklist([]string{`.*#.*`}, []string{`Alice#1001`}, "", suffix)
	want2 := `^(?!(?:Alice\#1001)(?:\D.*)?$).*#.*`
	if len(got2) != 1 || got2[0] != want2 {
		t.Fatalf("suffix got=%q want=%q", got2, want2)
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

	// 备注名包裹：key 在中间，需要 blacklist_prefix=.*
	got3 := withBlacklist([]string{`.*#.*`}, []string{`Alice#1001`}, `.*`, suffix)
	want3 := `^(?!.*(?:Alice\#1001)(?:\D.*)?$).*#.*`
	if len(got3) != 1 || got3[0] != want3 {
		t.Fatalf("prefix got=%q want=%q", got3, want3)
	}
	re3, err := regexp.Compile(got3[0])
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, s := range []string{`Alice#1001`, `小明(Alice#1001)`, `小明(Alice#1001小)`, `小明(Alice#1001)尾`} {
		if re3.MatchString(s) {
			t.Fatalf("should blacklist %q", s)
		}
	}
	if !re3.MatchString(`小明(Alice#10010)`) {
		t.Fatal("should still allow longer UID Alice#10010")
	}
	if !re3.MatchString(`Bob#2002`) {
		t.Fatal("should allow other friends")
	}
}

func TestApplyKeyRegex(t *testing.T) {
	t.Parallel()
	// 拜访好友：备注 小明(Alice#1001) / 普通 Alice#1001 → 统一抽 Alice#1001
	friendKey := `\(?([^()]*\#\d+)\)?`
	cases := []struct {
		text, keyRegex, want string
	}{
		{`Alice#1001`, friendKey, `Alice#1001`},
		{`Alice#1001小`, friendKey, `Alice#1001`},
		{`小明(Alice#1001)`, friendKey, `Alice#1001`},
		{`小明(Alice#1001小)`, friendKey, `Alice#1001`},
		{`plain`, `.*?\#\d+`, `plain`},
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
	if err != nil || p.Candidate != "Cand" || p.VisitedNode != "" || p.KeyRegex != "" || p.BlacklistPrefix != "" || p.BlacklistSuffix != "" {
		t.Fatalf("got=%+v err=%v", p, err)
	}
	p2, err := parseParams(`{"candidate":"Cand","visited_node":"Shared","key_regex":"\\(?([^()]*\\#\\d+)\\)?","blacklist_prefix":".*","blacklist_suffix":"(?:\\D.*)?"}`)
	if err != nil || p2.Candidate != "Cand" || p2.VisitedNode != "Shared" {
		t.Fatalf("got=%+v err=%v", p2, err)
	}
	if p2.KeyRegex != `\(?([^()]*\#\d+)\)?` || p2.BlacklistPrefix != `.*` || p2.BlacklistSuffix != `(?:\D.*)?` {
		t.Fatalf("optional fields: key=%q prefix=%q suffix=%q", p2.KeyRegex, p2.BlacklistPrefix, p2.BlacklistSuffix)
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
