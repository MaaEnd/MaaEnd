package essencefilter

import (
	"fmt"
	"html"
	"sort"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func LogMXU(ctx *maa.Context, content string) bool {
	LogMXUOverrideParam := map[string]any{
		"LogMXU": map[string]any{
			"focus": map[string]any{
				"Node.Action.Starting": content,
			},
		},
	}
	ctx.RunTask("LogMXU", LogMXUOverrideParam)
	return true
}

func LogMXUHTML(ctx *maa.Context, htmlText string) bool {
	htmlText = strings.TrimLeft(htmlText, " \t\r\n")
	return LogMXU(ctx, htmlText)
}

// LogMXUSimpleHTMLWithColor logs a simple styled span, allowing a custom color.
func LogMXUSimpleHTMLWithColor(ctx *maa.Context, text string, color string) bool {
	HTMLTemplate := fmt.Sprintf(`<span style="color: %s; font-weight: 500;">%%s</span>`, color)
	return LogMXUHTML(ctx, fmt.Sprintf(HTMLTemplate, text))
}

// LogMXUSimpleHTML logs a simple styled span with a default color.
func LogMXUSimpleHTML(ctx *maa.Context, text string) bool {
	// Call the more specific function with the default color "#00bfff".
	return LogMXUSimpleHTMLWithColor(ctx, text, "#00bfff")
}

// logMatchSummary - 输出“战利品 summary”，按技能组合聚合统计
func logMatchSummary(ctx *maa.Context) {
	if len(matchedCombinationSummary) == 0 {
		LogMXUSimpleHTML(ctx, "本次未锁定任何目标基质。")
		return
	}

	type viewItem struct {
		Key string
		*SkillCombinationSummary
	}

	items := make([]viewItem, 0, len(matchedCombinationSummary))
	for k, v := range matchedCombinationSummary {
		items = append(items, viewItem{Key: k, SkillCombinationSummary: v})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Key < items[j].Key
	})

	var b strings.Builder
	b.WriteString(`<div style="color: #00bfff; font-weight: 900; margin-top: 4px;">战利品摘要：</div>`)
	b.WriteString(`<table style="width: 100%; border-collapse: collapse; font-size: 12px;">`)
	b.WriteString(`<tr><th style="text-align:left; padding: 2px 4px;">武器</th><th style="text-align:left; padding: 2px 4px;">技能组合</th><th style="text-align:right; padding: 2px 4px;">锁定数量</th></tr>`)

	for _, item := range items {
		weaponText := formatWeaponNamesColoredHTML(item.Weapons)
		// 为了和前面 OCR 日志一致，summary 优先展示实际 OCR 到的技能文本
		skillSource := item.OCRSkills
		if len(skillSource) == 0 {
			// 兜底：如果没有 OCR 文本（理论上不会发生），退回到静态配置的技能中文名
			skillSource = item.SkillsChinese
		}

		formattedSkills := make([]string, len(skillSource))

		for i, s := range skillSource {
			escapedSkill := escapeHTML(s)
			formattedSkills[i] = fmt.Sprintf(`<span style="color: #064d7c;">%s</span>`, escapedSkill)
		}

		skillText := strings.Join(formattedSkills, " | ")
		b.WriteString("<tr>")
		b.WriteString(fmt.Sprintf(`<td style="padding: 2px 4px;">%s</td>`, weaponText))
		b.WriteString(fmt.Sprintf(`<td style="padding: 2px 4px;">%s</td>`, skillText))
		b.WriteString(fmt.Sprintf(`<td style="padding: 2px 4px; text-align: right;">%d</td>`, item.Count))
		b.WriteString("</tr>")
	}

	b.WriteString(`</table>`)
	LogMXUHTML(ctx, b.String())
}

// formatWeaponNamesColoredHTML - 按稀有度为每把武器着色并拼接成 HTML 片段
func formatWeaponNamesColoredHTML(weapons []WeaponData) string {
	if len(weapons) == 0 {
		return ""
	}
	var b strings.Builder
	for i, w := range weapons {
		if i > 0 {
			b.WriteString("、")
		}
		color := getColorForRarity(w.Rarity)
		b.WriteString(fmt.Sprintf(
			`<span style="color: %s;">%s</span>`,
			color, escapeHTML(w.ChineseName),
		))
	}
	return b.String()
}

func getColorForRarity(rarity int) string {
	switch rarity {
	case 6:
		return "#ff7000" // rarity 6
	case 5:
		return "#ffba03" // rarity 5
	case 4:
		return "#9451f8" // rarity 4
	case 3:
		return "#26bafb" // rarity 3
	default:
		return "#493a3a" // Default color
	}
}

// escapeHTML - 简单封装 html.EscapeString，便于后续统一替换/扩展
func escapeHTML(s string) string {
	return html.EscapeString(s)
}

// formatWeaponNames - 将多把武器名格式化为展示字符串（UI 层负责拼接与本地化）
func formatWeaponNames(weapons []WeaponData) string {
	if len(weapons) == 0 {
		return ""
	}
	names := make([]string, 0, len(weapons))
	for _, w := range weapons {
		names = append(names, w.ChineseName)
	}
	// 这里采用顿号拼接，更符合中文习惯；如需本地化，可进一步抽象
	return strings.Join(names, "、")
}
