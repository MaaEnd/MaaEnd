package essencefilter

import (
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

// 预刻写方案 / 840 全收集板块的 HTML 落盘相对路径（go-service 工作目录下）。
const planRecommendHTMLPath = "./EssencePlan.html"

// wrapHTMLDocument wraps one rendered MXU fragment into a minimal HTML5 document
// so it can be opened directly in a browser.
func wrapHTMLDocument(title, notice, fragment string) string {
	var b strings.Builder
	b.Grow(len(fragment) + 256)
	b.WriteString("<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</title><style>body{font-family:system-ui,sans-serif}</style></head><body>
<p style="color:#666;font-size:12px;margin:0 0 8px 0;">`)
	b.WriteString(html.EscapeString(notice))
	b.WriteString(`</p><hr style="border:none;border-top:1px solid #333;margin:8px 0"/>`)
	b.WriteString(fragment)
	b.WriteString("\n</body></html>\n")
	return b.String()
}

// wrapPlanRecommendHTML wraps the MXU plan_recommend fragment in a minimal HTML5 document for opening in a browser.
func wrapPlanRecommendHTML(fragment string) string {
	return wrapHTMLDocument(
		i18n.T("essencefilter.focus.plan.html_title"),
		i18n.T("essencefilter.focus.plan.html_notice"),
		fragment,
	)
}

func writeHTMLFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// writePlanRecommendHTMLFile writes a minimal HTML5 document containing fragment to path.
func writePlanRecommendHTMLFile(path, fragment string) error {
	return writeHTMLFile(path, wrapPlanRecommendHTML(fragment))
}

// appendPlanSectionHTML appends one rendered section to the end of the plan HTML file,
// keeping whatever earlier steps of the same run already wrote.
//
// 840 全收集板块按需求「追加」在文件末尾（现有预刻写方案板块保持不动）。文件尚不存在时
// 才创建最小 HTML5 文档。
func appendPlanSectionHTML(path, fragment string) error {
	notice := html.EscapeString(i18n.T("essencefilter.collection.html.notice"))
	section := "<hr style=\"border:none;border-top:1px dashed #555;margin:12px 0\"/>\n" +
		"<p style=\"color:#666;font-size:12px;margin:0 0 8px 0;\">" + notice + "</p>\n" +
		fragment

	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return writeHTMLFile(path, wrapHTMLDocument(
				i18n.T("essencefilter.collection.report.title"), notice, section))
		}
		return err
	}

	content := string(existing)
	const bodyEnd = "</body>"
	if idx := strings.LastIndex(content, bodyEnd); idx >= 0 {
		content = content[:idx] + section + "\n" + content[idx:]
	} else {
		content += "\n" + section + "\n"
	}
	return writeHTMLFile(path, content)
}
