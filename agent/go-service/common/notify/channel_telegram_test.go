package notify

import "testing"

func TestEscapeTelegramMarkdownV2(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"underscore", "a_b", `a\_b`},
		{"star", "a*b", `a\*b`},
		{"bracket", "[x]", `\[x\]`},
		{"paren", "(x)", `\(x\)`},
		{"tilde", "~x~", `\~x\~`},
		{"backtick", "a`b", "a\\`b"},
		{"gt", "a>b", `a\>b`},
		{"hash", "#x", `\#x`},
		{"plus", "a+b", `a\+b`},
		{"minus", "a-b", `a\-b`},
		{"eq", "a=b", `a\=b`},
		{"pipe", "a|b", `a\|b`},
		{"brace", "{x}", `\{x\}`},
		{"dot", "a.b", `a\.b`},
		{"bang", "!x", `\!x`},
		{"backslash", `C:\Users`, `C:\\Users`}, // 反斜杠是转义前缀，需自身转义
	}
	for _, c := range cases {
		if got := escapeTelegramMarkdownV2(c.in); got != c.want {
			t.Errorf("%s: escapeTelegramMarkdownV2(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestEscapeTelegramMarkdownV2Keep(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "完成 **重要通知** 了", "完成 **重要通知** 了"},
		{"italic", "_斜体_ 和 __下划线__", "_斜体_ 和 __下划线__"},
		{"strike", "~~删除~~ 与 ||剧透||", "~~删除~~ 与 ||剧透||"},
		{"link", "见 [文档](https://example.com?a=1&b=2)", "见 [文档](https://example.com?a=1&b=2)"},
		{"code", "执行 `go build` 完成", "执行 `go build` 完成"},
		{"plain escaped", `路径 C:\Users\a 的 _x_`, `路径 C:\\Users\\a 的 _x_`},
	}
	for _, c := range cases {
		if got := escapeTelegramMarkdownV2Keep(c.in); got != c.want {
			t.Errorf("%s: escapeTelegramMarkdownV2Keep(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestEscapeTelegramMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", "hello"},
		{"underscore", "a_b", `a\_b`},
		{"star", "a*b", `a\*b`},
		{"bracket", "[x]", `\[x\]`},
		{"backtick", "a`b", "a\\`b"},
		{"backslash", `C:\Users`, `C:\\Users`},
	}
	for _, c := range cases {
		if got := escapeTelegramMarkdown(c.in); got != c.want {
			t.Errorf("%s: escapeTelegramMarkdown(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestEscapeTelegramMarkdownKeep(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bold", "**加粗** 和 *斜体*", "**加粗** 和 *斜体*"},
		{"link", "[x](https://e.com)", "[x](https://e.com)"},
		{"code", "执行 `go build` 完成", "执行 `go build` 完成"},
		{"plain escaped", `a_b 和 [x] 和 路径 C:\tmp`, `a\_b 和 \[x\] 和 路径 C:\\tmp`},
	}
	for _, c := range cases {
		if got := escapeTelegramMarkdownKeep(c.in); got != c.want {
			t.Errorf("%s: escapeTelegramMarkdownKeep(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestEscapeTelegramHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"amp", "a & b", "a &amp; b"},
		{"lt", "a < b", "a &lt; b"},
		{"gt", "a > b", "a &gt; b"},
	}
	for _, c := range cases {
		if got := escapeTelegramHTML(c.in); got != c.want {
			t.Errorf("%s: escapeTelegramHTML(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestEscapeTelegramHTMLKeep(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"tag", "<b>a & b</b>", "<b>a &amp; b</b>"},
		{"link", `<a href="https://x.com?a=1&b=2">x</a> & 完`, `<a href="https://x.com?a=1&b=2">x</a> &amp; 完`},
	}
	for _, c := range cases {
		if got := escapeTelegramHTMLKeep(c.in); got != c.want {
			t.Errorf("%s: escapeTelegramHTMLKeep(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}