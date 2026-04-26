package essencefilter

import (
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
)

// 预刻写方案 HTML 落盘相对路径（go-service 工作目录下，每次覆盖）
const planRecommendHTMLPath = "./EssencePlan.html"

// wrapPlanRecommendHTML wraps the MXU plan_recommend fragment in a minimal HTML5 document for opening in a browser.
func wrapPlanRecommendHTML(fragment string) string {
	title := html.EscapeString(i18n.T("essencefilter.focus.plan.html_title"))
	notice := html.EscapeString(i18n.T("essencefilter.focus.plan.html_notice"))
	var b strings.Builder
	b.Grow(len(fragment) + 256)
	b.WriteString("<!DOCTYPE html>\n<html><head><meta charset=\"utf-8\"><title>")
	b.WriteString(title)
	b.WriteString(`</title><style>body{font-family:system-ui,sans-serif}</style></head><body>
<p style="color:#666;font-size:12px;margin:0 0 8px 0;">`)
	b.WriteString(notice)
	b.WriteString(`</p><hr style="border:none;border-top:1px solid #333;margin:8px 0"/>`)
	b.WriteString(fragment)
	b.WriteString("\n</body></html>\n")
	return b.String()
}

// writePlanRecommendHTMLFile writes a minimal HTML5 document containing fragment to path.
func writePlanRecommendHTMLFile(path, fragment string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(wrapPlanRecommendHTML(fragment)), 0644)
}
