package jsonclean

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustParse asserts the cleaned bytes parse under strict JSON and returns the
// decoded value.
func mustParse(t *testing.T, in string) any {
	t.Helper()
	cleaned := Clean([]byte(in))
	var v any
	if err := json.Unmarshal(cleaned, &v); err != nil {
		t.Fatalf("json.Unmarshal(%q) failed: %v; cleaned=%q", in, err, string(cleaned))
	}
	return v
}

func TestCleanLineComment(t *testing.T) {
	in := "{\n  // a comment\n  \"a\": 1,\n}"
	v := mustParse(t, in)
	if got := v.(map[string]any)["a"].(float64); got != 1 {
		t.Fatalf("a=%v want 1", got)
	}
}

func TestCleanBlockComment(t *testing.T) {
	in := "/* header */\n{\n  /* multi\n     line */\n  \"a\": 1\n}"
	v := mustParse(t, in)
	if got := v.(map[string]any)["a"].(float64); got != 1 {
		t.Fatalf("a=%v want 1", got)
	}
}

func TestCleanCommentInsideString(t *testing.T) {
	// // and /* inside a string literal must survive.
	in := `{"a": "http://x/y", "b": "/* not a comment */", "c": "//"}`
	v := mustParse(t, in)
	m := v.(map[string]any)
	if m["a"] != "http://x/y" {
		t.Fatalf("a=%v", m["a"])
	}
	if m["b"] != "/* not a comment */" {
		t.Fatalf("b=%v", m["b"])
	}
	if m["c"] != "//" {
		t.Fatalf("c=%v", m["c"])
	}
}

func TestCleanTrailingCommas(t *testing.T) {
	in := `{
  "a": 1,
  "b": [1, 2,],
}`
	v := mustParse(t, in)
	m := v.(map[string]any)
	if m["a"].(float64) != 1 {
		t.Fatal("a != 1")
	}
	arr := m["b"].([]any)
	if len(arr) != 2 || arr[1].(float64) != 2 {
		t.Fatalf("b=%v", arr)
	}
}

func TestCleanBOM(t *testing.T) {
	in := "\xEF\xBB\xBF{\"a\": 1}"
	v := mustParse(t, in)
	if got := v.(map[string]any)["a"].(float64); got != 1 {
		t.Fatalf("a=%v want 1", got)
	}
}

func TestCleanNewlinePreserved(t *testing.T) {
	in := "{\n  // c\n  \"a\": 1\n}"
	cleaned := string(Clean([]byte(in)))
	// The comment line's leading whitespace stays; the newline that terminated
	// the comment must be preserved so line numbers do not shift.
	if cleaned != "{\n  \n  \"a\": 1\n}" {
		t.Fatalf("cleaned=%q", cleaned)
	}
}

func TestCleanEmptyAndPureComment(t *testing.T) {
	if got := string(Clean([]byte(""))); got != "" {
		t.Fatalf("empty=%q", got)
	}
	if got := string(Clean([]byte("// just a comment\n"))); got != "\n" {
		t.Fatalf("pure=%q", got)
	}
}

func TestCleanSlashNotComment(t *testing.T) {
	// A lone '/' outside a string must not be treated as a comment start
	// (Clean only strips // and /*, a bare slash passes through unchanged).
	in := `{"/": 1}`
	cleaned := string(Clean([]byte(in)))
	if cleaned != in {
		t.Fatalf("cleaned=%q want %q", cleaned, in)
	}
}
func TestCleanUnterminatedBlockCommentNotSwallowed(t *testing.T) {
	in := `{"a": 1} /* unterminated`
	cleaned := string(Clean([]byte(in)))
	// 未闭合的 /* 不应被静默吞掉；要么原样保留触发后续解析失败，要么显式报错。
	if !strings.Contains(cleaned, "/*") {
		t.Fatalf("unterminated block comment was swallowed: cleaned=%q", cleaned)
	}
	// 且清理结果不应能通过严格 JSON 解析
	var v any
	if err := json.Unmarshal([]byte(cleaned), &v); err == nil {
		t.Fatalf("unterminated block comment accepted as valid JSON: cleaned=%q", cleaned)
	}
}

func TestCleanClosedBlockCommentStripped(t *testing.T) {
	in := `{"a": 1} /* closed */`
	cleaned := string(Clean([]byte(in)))
	if strings.Contains(cleaned, "/*") || strings.Contains(cleaned, "closed") {
		t.Fatalf("closed block comment not stripped: cleaned=%q", cleaned)
	}
	var v any
	if err := json.Unmarshal([]byte(cleaned), &v); err != nil {
		t.Fatalf("closed block comment broke parsing: %v; cleaned=%q", err, cleaned)
	}
}
