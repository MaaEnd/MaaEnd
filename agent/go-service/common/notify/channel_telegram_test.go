package notify

import "testing"

// 保留三个底层转义原语测试（渲染器直接复用）。

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

func TestEscapeTelegramMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", "hello"},
		{"underscore", "a_b", `a\_b`},
		{"star", "a*b", `a\*b`},
		{"bracket", "[x]", `\[x]`},
		{"backtick", "a`b", "a\\`b"},
		{"close bracket", "x]", "x]"},         // legacy 官方只转义 _ * ` [，不转义 ]
		{"backslash", `C:\Users`, `C:\Users`}, // legacy 不支持 \\ 语义，反斜杠原样
	}
	for _, c := range cases {
		if got := escapeTelegramMarkdown(c.in); got != c.want {
			t.Errorf("%s: escapeTelegramMarkdown(%q) = %q, want %q", c.name, c.in, got, c.want)
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

// MarkdownV2 渲染整合测试：结构保留 + 实体外转义 + 实体内部豁免。

func TestRenderTelegramV2(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello world", "hello world"},
		{"plain escaped", "a_b*c[d]e", `a\_b\*c\[d\]e`},
		{"bold", "**重要通知**", `*重要通知*`}, // V2 粗体是单 *
		{"italic", "_斜体_", `_斜体_`},
		{"underline", "__重点__", `__重点__`},
		{"strike", "~~删除~~", `~~删除~~`},
		{"spoiler", "||剧透||", `||剧透||`}, // spoiler 实体保留
		{"code", "`go build`", "`go build`"},
		{"code inner backtick", "``a ` b``", "`a \\` b`"}, // 实体内部转义反引号
		{"link", "[文档](https://e.com)", `[文档](https://e.com)`},
		{"link paren url", "[文档](https://e.com/a(b))", `[文档](https://e.com/a(b\))`},
		{"image", "![alt](https://e.com/i.png)", `[alt](https://e.com/i.png)`}, // 图片降级为链接
		{"escaping source", `\*字面\*`, `\*字面\*`},
		{"snake_case", "snake_case", `snake\_case`}, // intraword 不下划线强调，仅转义
		{"intraword underscore", "a_b_c", `a\_b\_c`},
		{"unclosed", "**abc", `\*\*abc`}, // 不闭合 → 字面量转义
	}
	for _, c := range cases {
		if got := renderTelegramV2(c.in); got != c.want {
			t.Errorf("%s: renderTelegramV2(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestRenderTelegramV2Blocks(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"heading", "# 标题", "*标题*"},
		{"quote", "> 引用", "> 引用"},
		{"list", "- 一\n- 二", "• 一\n• 二"},
		{"ordered list", "1. 一\n2. 二", `1\. 一` + "\n" + `2\. 二`},
		{"code block", "```go\nfmt.Println(1)\n```", "```go\nfmt.Println(1)\n```"},
		{"rule", "---", "―――"},
	}
	for _, c := range cases {
		if got := renderTelegramV2(c.in); got != c.want {
			t.Errorf("%s: renderTelegramV2(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// legacy 渲染：只保留粗/斜/链接/行内码/代码块，其它降级文本。

func TestRenderTelegramLegacy(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", "hello"},
		{"bold", "**重要通知**", "*重要通知*"},
		{"italic", "_斜体_", "_斜体_"},
		{"underline degrade", "__重点__", "重点"},
		{"strike degrade", "~~删除~~", "删除"},
		{"spoiler degrade", "||剧透||", "剧透"},
		{"code", "`go build`", "`go build`"},
		{"link", "[文档](https://e.com)", "[文档](https://e.com)"},
		{"heading", "# 标题", "*标题*"},
		{"quote", "> 引用", "引用"}, // legacy 无引用语法，剥标记
		{"list", "- 一\n- 二", "• 一\n• 二"},
		{"escape", "a_b c*d", `a\_b c\*d`},
	}
	for _, c := range cases {
		if got := renderTelegramLegacy(c.in); got != c.want {
			t.Errorf("%s: renderTelegramLegacy(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// HTML 渲染：结构用标签，文本转义。

func TestRenderTelegramHTML(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "hello", "hello"},
		{"plain escaped", "a & b", "a &amp; b"},
		{"bold", "**重要通知**", "<b>重要通知</b>"},
		{"italic", "_斜体_", "<i>斜体</i>"},
		{"underline", "__重点__", "<u>重点</u>"},
		{"strike", "~~删除~~", "<s>删除</s>"},
		{"spoiler", "||剧透||", "<tg-spoiler>剧透</tg-spoiler>"},
		{"code", "`go build & deploy`", "<code>go build &amp; deploy</code>"},
		{"link", "[文档](https://e.com?a=1&b=2)", `<a href="https://e.com?a=1&amp;b=2">文档</a>`},
		{"heading", "# 标题", "<b>标题</b>"},
		{"quote", "> 引用", "<blockquote>引用</blockquote>"},
		{"code block", "```\na < b\n```", "<pre>a &lt; b</pre>"},
	}
	for _, c := range cases {
		if got := renderTelegramHTML(c.in); got != c.want {
			t.Errorf("%s: renderTelegramHTML(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// 解析单测：块级与行内结构。

func TestMDParseText(t *testing.T) {
	blocks := mdParseText("# 标题\n\n段落一\n\n- a\n- b\n")
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3: %+v", len(blocks), blocks)
	}
	if blocks[0].kind != mdHeading || blocks[0].level != 1 {
		t.Errorf("block0 = %+v, want heading level 1", blocks[0])
	}
	if blocks[1].kind != mdPara {
		t.Errorf("block1 = %+v, want para", blocks[1])
	}
	if blocks[2].kind != mdList || blocks[2].ordered || len(blocks[2].items) != 2 {
		t.Errorf("block2 = %+v, want unordered list of 2", blocks[2])
	}

	// 行内粗体
	para := mdParseText("**粗体** 和 `code`")[0]
	if len(para.inline) != 3 || para.inline[0].kind != mdBold || para.inline[1].kind != mdText {
		t.Errorf("inline = %+v, want bold+text+code", para.inline)
	}
}

func TestMDParseIntraword(t *testing.T) {
	// snake_case 不应解析为斜体
	nodes := mdParseInline("snake_case_case", 0)
	if len(nodes) != 1 || nodes[0].kind != mdText || nodes[0].text != "snake_case_case" {
		t.Errorf("intraword = %+v, want single text snake_case_case", nodes)
	}
}

func TestMDParseUnclosed(t *testing.T) {
	nodes := mdParseInline("a **b", 0)
	if len(nodes) != 1 || nodes[0].kind != mdText || nodes[0].text != "a **b" {
		t.Errorf("unclosed = %+v, want literal text", nodes)
	}
}

func TestMDParseEscape(t *testing.T) {
	nodes := mdParseInline(`\*x\*`, 0)
	if len(nodes) != 1 || nodes[0].kind != mdText || nodes[0].text != "*x*" {
		t.Errorf("escape = %+v, want literal *x*", nodes)
	}
}

func TestMDParseDeptLimit(t *testing.T) {
	// 嵌套深度上限：8 层后不再解析结构（防止递归爆炸）
	long := "**a**" // 实际解析 1 层
	nodes := mdParseInline(long, 7)
	if len(nodes) != 1 || nodes[0].kind != mdBold {
		t.Errorf("depth limit: %+v, want bold at depth 7", nodes)
	}
}

func TestMDParseUTF8(t *testing.T) {
	nodes := mdParseInline("中文**粗体**完成", 0)
	if len(nodes) != 3 || nodes[1].kind != mdBold {
		t.Errorf("utf8 = %+v, want text+bold+text", nodes)
	}
}
