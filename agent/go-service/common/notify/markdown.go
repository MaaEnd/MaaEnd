package notify

import (
	"strconv"
	"strings"
)

// 轻量 Markdown 子集解析器与 Telegram 渲染。
//
// 只实现通知场景够用的语法，刻意不做完整 CommonMark：
//   - 块级：段落、标题（#~######）、引用（>）、列表（- / * / 1.）、围栏代码块（```lang）、分隔线（---）
//   - 行内：*斜体*、**粗体**、_斜体_、__下划线__、~~删除线~~、||剧透||、`代码`、[文本](URL)、![图](URL)
//   - 反斜杠转义：\x（ASCII 标点）视为字面量
//
// 渲染时各 parse_mode 目标产生各自方言：MarkdownV2 全量转义且实体内部
// 豁免；legacy Markdown 只支持粗/斜/链接/行内码/代码块且禁止嵌套；
// HTML 用标签表达结构。除 Telegram 外其余渠道一律原样透传正文（各渠道
// 官方协议不支持改写，见 README 富文本矩阵）。

// mdMaxDepth 行内嵌套递归上限，防止病态输入（如长串 ****）递归爆炸。
const mdMaxDepth = 8

// mdKind 行内节点类型。
type mdKind uint8

const (
	mdText mdKind = iota
	mdBold
	mdItalic
	mdUnderline
	mdStrike
	mdSpoiler
	mdCode
	mdLink
	mdImage
)

// mdInline 行内节点：text=文本/代码内容，href=链接地址，children=嵌套行内。
type mdInline struct {
	kind     mdKind
	text     string
	href     string
	children []mdInline
}

// mdBlockKind 块级节点类型。
type mdBlockKind uint8

const (
	mdPara mdBlockKind = iota
	mdHeading
	mdQuote
	mdList
	mdCodeBlock
	mdRule
)

// mdBlock 块级节点：约化为四类负载（inline / items / code）。
// mdList 每项一行 inline；mdCodeBlock 原文整体保存。
type mdBlock struct {
	kind    mdBlockKind
	level   int          // mdHeading 层数（1~6）；mdQuote 恒为 1（不支持多级渲染）
	ordered bool         // mdList 有序/无序
	lang    string       // mdCodeBlock 语言标记（可为空）
	inline  []mdInline   // mdPara/mdHeading/mdQuote 内容
	items   [][]mdInline // mdList 各条目
	code    string       // mdCodeBlock 原文
}

// ---------------------------------------------------------------------------
// 解析
// ---------------------------------------------------------------------------

// mdParseText 把整段文本解析为块列表。每行按块级特征识别，段落按空行/新块切分。
func mdParseText(src string) []mdBlock {
	lines := strings.Split(src, "\n")
	var blocks []mdBlock
	var para []string
	flushPara := func() {
		if len(para) == 0 {
			return
		}
		blocks = append(blocks, mdBlock{kind: mdPara, inline: mdParseInline(strings.Join(para, "\n"), 0)})
		para = nil
	}
	i := 0
	for i < len(lines) {
		line := lines[i]
		switch {
		case strings.TrimSpace(line) == "":
			flushPara()
			i++
		case isFenceStart(line):
			flushPara()
			lang := mdFenceLang(line)
			i++
			var buf []string
			for i < len(lines) && !isFenceEnd(lines[i], lang != "") {
				buf = append(buf, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // 吃掉闭合围栏行
			}
			blocks = append(blocks, mdBlock{kind: mdCodeBlock, lang: lang, code: strings.Join(buf, "\n")})
		case isHeadingLine(line):
			flushPara()
			level := mdHeadingLevel(line)
			rest := strings.TrimSpace(line[level:])
			blocks = append(blocks, mdBlock{kind: mdHeading, level: level, inline: mdParseInline(rest, 0)})
			i++
		case isQuoteLine(line):
			flushPara()
			var q []string
			for i < len(lines) && isQuoteLine(lines[i]) {
				q = append(q, mdStripQuote(lines[i]))
				i++
			}
			blocks = append(blocks, mdBlock{kind: mdQuote, inline: mdParseInline(strings.Join(q, "\n"), 0)})
		case isListLine(line):
			flushPara()
			ordered, _ := mdListMarker(line)
			var items [][]mdInline
			for i < len(lines) && isListLine(lines[i]) {
				if ord, mLen := mdListMarker(lines[i]); ord == ordered && mLen > 0 {
					items = append(items, mdParseInline(strings.TrimSpace(lines[i][mLen:]), 0))
					i++
					continue
				}
				break // 列表类型切换 / 非列表行：结束
			}
			blocks = append(blocks, mdBlock{kind: mdList, ordered: ordered, items: items})
		case isHRLine(line):
			flushPara()
			blocks = append(blocks, mdBlock{kind: mdRule})
			i++
		default:
			para = append(para, line)
			i++
		}
	}
	flushPara()
	return blocks
}

func isFenceStart(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") && !strings.HasPrefix(t, "````") ||
		strings.HasPrefix(t, "~~~") && !strings.HasPrefix(t, "~~~~")
}

// isFenceEnd 闭合围栏：同字符满 3 个、行尾无语言标记。
func isFenceEnd(line string, hasLang bool) bool {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "```") {
		rest := strings.TrimSpace(t[3:])
		return rest == ""
	}
	if strings.HasPrefix(t, "~~~") {
		return strings.TrimSpace(t[3:]) == ""
	}
	return false
}

func mdFenceLang(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "```") {
		return strings.TrimSpace(t[3:])
	}
	if strings.HasPrefix(t, "~~~") {
		return strings.TrimSpace(t[3:])
	}
	return ""
}

func isHeadingLine(line string) bool {
	if line == "" || line[0] != '#' {
		return false
	}
	n := 0
	for n < len(line) && n < 6 && line[n] == '#' {
		n++
	}
	return n <= len(line) && (n == len(line) || line[n] == ' ' || line[n] == '\t')
}

func mdHeadingLevel(line string) int {
	n := 0
	for n < len(line) && n < 6 && line[n] == '#' {
		n++
	}
	return n
}

func isQuoteLine(line string) bool {
	return strings.HasPrefix(line, ">")
}

// mdStripQuote 剥掉行首的 >（含连续 >>>，支持一层空格）。
func mdStripQuote(line string) string {
	i := 0
	for i < len(line) && line[i] == '>' {
		i++
	}
	if i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[i:]
}

func isListLine(line string) bool {
	ordered, _ := mdListMarker(line)
	return ordered || strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ")
}

// mdListMarker 识别列表标记并返回是否有序与标记长度（含空格，供剥除）。
func mdListMarker(line string) (ordered bool, n int) {
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ") {
		return false, 2
	}
	if line == "" {
		return false, 0
	}
	j := 0
	for j < len(line) && line[j] >= '0' && line[j] <= '9' {
		j++
	}
	if j > 0 && j < len(line) && (line[j] == '.' || line[j] == ')') && j+1 < len(line) && line[j+1] == ' ' {
		return true, j + 2
	}
	return false, 0
}

func isHRLine(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 {
		return false
	}
	c := t[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != c && t[i] != ' ' && t[i] != '\t' {
			return false
		}
	}
	return true
}

func isWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}

func isAsciiPunct(b byte) bool {
	return b >= 0x21 && b <= 0x2F || b >= 0x3A && b <= 0x40 || b >= 0x5B && b <= 0x60 || b >= 0x7B && b <= 0x7E
}

func countRun(s string, i int, c byte) int {
	j := i
	for j < len(s) && s[j] == c {
		j++
	}
	return j - i
}

// findClosingRun 从 i+n 起找第一个连续标记数 >= n 的位置；找不到返回 -1。
func findClosingRun(s string, i, n int, c byte) int {
	j := i + n
	for j < len(s) {
		if s[j] == c {
			k := countRun(s, j, c)
			if k >= n {
				return j
			}
			j += k
		} else {
			j++
		}
	}
	return -1
}

// findClosingParen 找配对的右括号，支持一层嵌套与 `\)` 转义。
func findClosingParen(s string, i int) int {
	depth := 0
	for i < len(s) {
		switch s[i] {
		case '\\':
			i += 2
			continue
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
		i++
	}
	return -1
}

// mdParseInline 行内解析：输出节点序列，未闭合/非法结构回退为字面文本。
func mdParseInline(s string, depth int) []mdInline {
	if depth >= mdMaxDepth || s == "" {
		return []mdInline{{kind: mdText, text: s}}
	}
	var out []mdInline
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, mdInline{kind: mdText, text: text.String()})
			text.Reset()
		}
	}
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isAsciiPunct(s[i+1]):
			text.WriteByte(s[i+1])
			i += 2
		case c == '`':
			n := countRun(s, i, '`')
			if j := findClosingRun(s, i, n, '`'); j >= 0 {
				content := s[i+n : j]
				if len(content) > 2 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.TrimSpace(content) != "" {
					content = content[1 : len(content)-1]
				}
				flush()
				out = append(out, mdInline{kind: mdCode, text: content})
				i = j + n
				continue
			}
			text.WriteString(s[i : i+n])
			i += n
		case c == '[':
			// 链接：[label](url)，label 不允许嵌套 [（简化）
			end := strings.IndexByte(s[i+1:], ']')
			if end < 0 {
				text.WriteByte('[')
				i++
				continue
			}
			labelEnd := i + 1 + end
			if labelEnd+1 >= len(s) || s[labelEnd+1] != '(' {
				text.WriteByte('[')
				i++
				continue
			}
			if closeParen := findClosingParen(s, labelEnd+2); closeParen >= 0 {
				label := s[i+1 : labelEnd]
				rawURL := s[labelEnd+2 : closeParen]
				rawURL = strings.ReplaceAll(rawURL, `\)`, ")")
				rawURL = strings.ReplaceAll(rawURL, `\(`, "(")
				flush()
				out = append(out, mdInline{kind: mdLink, href: rawURL, children: mdParseInline(label, depth+1)})
				i = closeParen + 1
				continue
			}
			text.WriteByte('[')
			i++
		case c == '!' && i+1 < len(s) && s[i+1] == '[':
			end := strings.IndexByte(s[i+2:], ']')
			if end < 0 || i+2+end+1 >= len(s) || s[i+2+end+1] != '(' {
				text.WriteByte('!')
				i++
				continue
			}
			labelEnd := i + 2 + end
			if closeParen := findClosingParen(s, labelEnd+2); closeParen >= 0 {
				alt := s[i+2 : labelEnd]
				rawURL := s[labelEnd+2 : closeParen]
				rawURL = strings.ReplaceAll(rawURL, `\)`, ")")
				flush()
				out = append(out, mdInline{kind: mdImage, text: alt, href: rawURL})
				i = closeParen + 1
				continue
			}
			text.WriteByte('!')
			i++
		case c == '*':
			n := countRun(s, i, '*')
			if n == 2 {
				if j := findClosingRun(s, i, n, '*'); j >= 0 {
					flush()
					out = append(out, mdInline{kind: mdBold, children: mdParseInline(s[i+n:j], depth+1)})
					i = j + n
					continue
				}
			} else if n == 1 {
				if j := findClosingRun(s, i, n, '*'); j >= 0 {
					flush()
					out = append(out, mdInline{kind: mdItalic, children: mdParseInline(s[i+n:j], depth+1)})
					i = j + n
					continue
				}
			}
			text.WriteString(s[i : i+n])
			i += n
		case c == '_':
			// intraword 下划线不开启强调（保护 snake_case / 路径）
			if i > 0 && isWordByte(s[i-1]) {
				text.WriteByte('_')
				i++
				continue
			}
			n := countRun(s, i, '_')
			if n == 2 {
				if j := findClosingRun(s, i, n, '_'); j >= 0 {
					if j+n == len(s) || !isWordByte(s[j+n]) {
						flush()
						out = append(out, mdInline{kind: mdUnderline, children: mdParseInline(s[i+n:j], depth+1)})
						i = j + n
						continue
					}
				}
			} else if n == 1 {
				// 闭合需右 flanking：其后不能紧跟字母数字（保护 snake_case 场景）
				if j := findClosingRun(s, i, 1, '_'); j >= 0 && j > i+1 && (j == len(s)-1 || !isWordByte(s[j+1])) {
					flush()
					out = append(out, mdInline{kind: mdItalic, children: mdParseInline(s[i+1:j], depth+1)})
					i = j + 1
					continue
				}
				text.WriteString(s[i : i+n])
				i += n
			} else {
				text.WriteString(s[i : i+n])
				i += n
			}
		case c == '~':
			n := countRun(s, i, '~')
			if n == 2 {
				if j := findClosingRun(s, i, 2, '~'); j >= 0 {
					flush()
					out = append(out, mdInline{kind: mdStrike, children: mdParseInline(s[i+2:j], depth+1)})
					i = j + 2
					continue
				}
			}
			text.WriteString(s[i : i+n])
			i += n
		case c == '|':
			n := countRun(s, i, '|')
			if n == 2 {
				if j := findClosingRun(s, i, 2, '|'); j >= 0 {
					flush()
					out = append(out, mdInline{kind: mdSpoiler, children: mdParseInline(s[i+2:j], depth+1)})
					i = j + 2
					continue
				}
			}
			text.WriteString(s[i : i+n])
			i += n
		default:
			text.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

// ---------------------------------------------------------------------------
// 渲染
// ---------------------------------------------------------------------------

// renderTelegramV2 把正文渲染为 MarkdownV2 方言：普通文本全量转义，
// code/pre 内只转义反引号与反斜杠，URL 内转义右括号。
func renderTelegramV2(src string) string {
	return renderBlocks(mdParseText(src), false, false)
}

// renderTelegramLegacy 渲染为 legacy Markdown：只支持粗/斜/链接/行内码/代码块，
// 实体外转义 `_ * ` [`，实体内禁止转义，嵌套实体拍平为文本。
func renderTelegramLegacy(src string) string {
	return renderBlocks(mdParseText(src), true, false)
}

// renderTelegramHTML 渲染为 HTML 方言：结构用标签，文本段转义 < > &。
func renderTelegramHTML(src string) string {
	return renderBlocks(mdParseText(src), false, true)
}

func renderBlocks(blocks []mdBlock, legacy, html bool) string {
	var b strings.Builder
	for idx, blk := range blocks {
		if idx > 0 {
			if blk.kind == mdPara && blocks[idx-1].kind == mdPara {
				b.WriteString("\n\n") // 段落之间保留空行（标题与正文、多段正文）
			} else {
				b.WriteByte('\n')
			}
		}
		switch blk.kind {
		case mdPara:
			b.WriteString(renderInline(blk.inline, legacy, html))
		case mdHeading:
			inner := renderInline(blk.inline, legacy, html)
			if html {
				b.WriteString("<b>")
				b.WriteString(inner)
				b.WriteString("</b>")
			} else {
				b.WriteByte('*')
				b.WriteString(inner)
				b.WriteByte('*')
			}
		case mdQuote:
			b.WriteString(renderQuote(blk.inline, legacy, html))
		case mdList:
			b.WriteString(renderList(blk, legacy, html))
		case mdCodeBlock:
			b.WriteString(renderCodeBlock(blk, legacy, html))
		case mdRule:
			b.WriteString("―――")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderQuote(inline []mdInline, legacy, html bool) string {
	inner := renderInline(inline, legacy, html)
	if html {
		return "<blockquote>" + inner + "</blockquote>"
	}
	if legacy {
		return inner // legacy 无引用语法：剥掉标记按文本
	}
	// MarkdownV2 blockquote：逐行前缀 >（含空行）
	var b strings.Builder
	for _, line := range strings.Split(inner, "\n") {
		b.WriteString("> ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderList(blk mdBlock, legacy, html bool) string {
	var b strings.Builder
	for i, item := range blk.items {
		inner := renderInline(item, legacy, html)
		if html {
			b.WriteString("• ")
		} else if blk.ordered {
			// V2 中 "." 为保留字符需转义；legacy 无需
			if legacy {
				b.WriteString(strconv.Itoa(i + 1))
				b.WriteString(". ")
			} else {
				b.WriteString(strconv.Itoa(i + 1))
				b.WriteString(`\. `)
			}
		} else {
			b.WriteString("• ")
		}
		b.WriteString(inner)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderCodeBlock(blk mdBlock, legacy, html bool) string {
	if html {
		var b strings.Builder
		b.WriteString("<pre>")
		b.WriteString(escapeTelegramHTML(blk.code))
		b.WriteString("</pre>")
		return b.String()
	}
	// V2 / legacy 都用围栏
	fence := "```"
	if blk.lang != "" {
		fence += blk.lang
	}
	var b strings.Builder
	b.WriteString(fence)
	b.WriteByte('\n')
	if legacy {
		b.WriteString(blk.code)
	} else {
		b.WriteString(escapeTelegramV2Code(blk.code))
	}
	b.WriteString("\n```")
	return b.String()
}

// renderInline 行内渲染；legacy 模式下嵌套实体拍平为文本。
func renderInline(nodes []mdInline, legacy, html bool) string {
	flatten := func(n mdInline) string {
		var b strings.Builder
		flattenInline(&b, n.children)
		return b.String()
	}
	var b strings.Builder
	for _, n := range nodes {
		switch n.kind {
		case mdText:
			if html {
				b.WriteString(escapeTelegramHTML(n.text))
			} else if legacy {
				b.WriteString(escapeTelegramMarkdown(n.text))
			} else {
				b.WriteString(escapeTelegramMarkdownV2(n.text))
			}
		case mdCode:
			if html {
				b.WriteString("<code>")
				b.WriteString(escapeTelegramHTML(n.text))
				b.WriteString("</code>")
			} else if legacy {
				b.WriteByte('`')
				b.WriteString(n.text)
				b.WriteByte('`')
			} else {
				b.WriteByte('`')
				b.WriteString(escapeTelegramV2Code(n.text))
				b.WriteByte('`')
			}
		case mdBold, mdItalic, mdUnderline, mdStrike, mdSpoiler:
			if legacy {
				switch n.kind {
				case mdBold:
					b.WriteByte('*')
					b.WriteString(flatten(n))
					b.WriteByte('*')
				case mdItalic:
					b.WriteByte('_')
					b.WriteString(flatten(n))
					b.WriteByte('_')
				default:
					b.WriteString(flatten(n)) // 下划线/删除线/剧透 legacy 不支持 → 降级文本
				}
				continue
			}
			if html {
				switch n.kind {
				case mdBold:
					b.WriteString("<b>")
				case mdItalic:
					b.WriteString("<i>")
				case mdUnderline:
					b.WriteString("<u>")
				case mdStrike:
					b.WriteString("<s>")
				case mdSpoiler:
					b.WriteString("<tg-spoiler>")
				}
				b.WriteString(renderInline(n.children, false, true))
				switch n.kind {
				case mdBold:
					b.WriteString("</b>")
				case mdItalic:
					b.WriteString("</i>")
				case mdUnderline:
					b.WriteString("</u>")
				case mdStrike:
					b.WriteString("</s>")
				case mdSpoiler:
					b.WriteString("</tg-spoiler>")
				}
				continue
			}
			// MarkdownV2
			inner := renderInline(n.children, false, false)
			switch n.kind {
			case mdBold:
				b.WriteByte('*')
				b.WriteString(inner)
				b.WriteByte('*')
			case mdItalic:
				b.WriteByte('_')
				b.WriteString(inner)
				b.WriteByte('_')
			case mdUnderline:
				b.WriteString("__")
				b.WriteString(inner)
				b.WriteString("__")
			case mdStrike:
				b.WriteString("~~")
				b.WriteString(inner)
				b.WriteString("~~")
			case mdSpoiler:
				b.WriteString("||")
				b.WriteString(inner)
				b.WriteString("||")
			}
		case mdLink, mdImage:
			label := n.children
			if n.kind == mdImage {
				label = []mdInline{{kind: mdText, text: n.text}}
			}
			if html {
				b.WriteString(`<a href="`)
				b.WriteString(escapeTelegramHTMLAttr(n.href))
				b.WriteString(`">`)
				b.WriteString(renderInline(label, false, true))
				b.WriteString("</a>")
				continue
			}
			if legacy {
				b.WriteByte('[')
				b.WriteString(renderInline(label, true, false))
				b.WriteString("](")
				b.WriteString(n.href)
				b.WriteString(")")
				continue
			}
			b.WriteByte('[')
			b.WriteString(renderInline(label, false, false))
			b.WriteString("](")
			b.WriteString(escapeTelegramV2URL(n.href))
			b.WriteByte(')')
		}
	}
	return b.String()
}

func flattenInline(b *strings.Builder, nodes []mdInline) {
	for _, n := range nodes {
		switch n.kind {
		case mdCode:
			b.WriteString(n.text)
		case mdLink, mdImage:
			if len(n.children) > 0 {
				flattenInline(b, n.children)
			} else if n.text != "" {
				b.WriteString(n.text)
			}
		default:
			if len(n.children) > 0 {
				flattenInline(b, n.children)
			} else {
				b.WriteString(n.text)
			}
		}
	}
}

// escapeTelegramV2Code pre/code 实体内部：只转义反引号与反斜杠。
func escapeTelegramV2Code(s string) string {
	return strings.NewReplacer("\\", `\\`, "`", "\\`").Replace(s)
}

// escapeTelegramV2URL 链接 URL 内部：转义右括号（结束符）与反斜杠。
func escapeTelegramV2URL(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\\", `\\`), ")", `\)`)
}

// escapeTelegramHTMLAttr HTML 属性值：转义 & 与双引号。
func escapeTelegramHTMLAttr(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "&", "&amp;"), `"`, "&quot;")
}
