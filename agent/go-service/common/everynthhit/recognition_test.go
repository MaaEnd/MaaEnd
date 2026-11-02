package everynthhit

import (
	"encoding/json"
	"testing"
)

// TestParseEvery 覆盖 every 的两种合法写法（JSON 数字、数字字符串）与各类非法输入。
// 非法输入一律返回 false，由 Run 记 Warn 后按未命中处理，不能 panic。
// 非正数（0、负数）由 Run 的 every <= 0 判断拦下，因此不算解析失败。
func TestParseEvery(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   json.RawMessage
		want  int
		valid bool
	}{
		{name: "json integer", raw: json.RawMessage(`3`), want: 3, valid: true},
		{name: "json number with zero fraction", raw: json.RawMessage(`3.0`), want: 3, valid: true},
		{name: "numeric string", raw: json.RawMessage(`"3"`), want: 3, valid: true},
		{name: "numeric string with spaces", raw: json.RawMessage(`" 3 "`), want: 3, valid: true},
		{name: "fraction is not an integer", raw: json.RawMessage(`3.5`), want: 0, valid: false},
		{name: "non numeric string", raw: json.RawMessage(`"three"`), want: 0, valid: false},
		{name: "empty string", raw: json.RawMessage(`""`), want: 0, valid: false},
		{name: "boolean", raw: json.RawMessage(`true`), want: 0, valid: false},
		{name: "empty raw", raw: json.RawMessage(""), want: 0, valid: false},
		{name: "missing every", raw: nil, want: 0, valid: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseEvery(tc.raw)
			if got != tc.want || ok != tc.valid {
				t.Fatalf("parseEvery(%q) = %d, %v; want %d, %v", string(tc.raw), got, ok, tc.want, tc.valid)
			}
		})
	}
}

// TestRecognitionParamKeepsRawEvery 校验 every 的原始 JSON 类型被保留，
// 数字与数字字符串因此能走到同一条解析路径。
func TestRecognitionParamKeepsRawEvery(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "number", raw: `{"key":"route","every":6}`},
		{name: "numeric string", raw: `{"key":"route","every":"6"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var param recognitionParam
			if err := json.Unmarshal([]byte(tc.raw), &param); err != nil {
				t.Fatalf("unmarshal %s: %v", tc.raw, err)
			}
			if param.Key != "route" {
				t.Fatalf("key = %q; want route", param.Key)
			}
			every, ok := parseEvery(param.Every)
			if !ok || every != 6 {
				t.Fatalf("every = %d, %v; want 6, true", every, ok)
			}
		})
	}
}
