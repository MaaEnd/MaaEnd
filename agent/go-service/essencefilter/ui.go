package essencefilter

import (
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

func LogMXUHTML(ctx *maa.Context, htmlText string) {
	htmlText = strings.TrimLeft(htmlText, " \t\r\n")
	maafocus.NodeActionStarting(ctx, htmlText)
}

// LogMXUSimpleHTMLWithColor logs a simple styled span, allowing a custom color.
func LogMXUSimpleHTMLWithColor(ctx *maa.Context, text string, color string) {
	HTMLTemplate := fmt.Sprintf(`<span style="color: %s; font-weight: 500;">%%s</span>`, color)
	LogMXUHTML(ctx, fmt.Sprintf(HTMLTemplate, text))
}

// LogMXUSimpleHTML logs a simple styled span with a default color.
func LogMXUSimpleHTML(ctx *maa.Context, text string) {
	// Call the more specific function with the default color "#00bfff".
	LogMXUSimpleHTMLWithColor(ctx, text, "#00bfff")
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
func formatWeaponNames(weapons []matchapi.WeaponData) string {
	if len(weapons) == 0 {
		return ""
	}
	names := make([]string, 0, len(weapons))
	for _, w := range weapons {
		names = append(names, w.ChineseName)
	}
	return strings.Join(names, "、")
}

// --- 战利品摘要与预刻写方案（同一 case：本次运行的结果展示）---

// logMatchSummary - 输出“战利品 summary”，按技能组合聚合统计
func logMatchSummary(ctx *maa.Context) {
	st := getRunState()
	if st == nil || len(st.MatchedCombinationSummary) == 0 {
		LogMXUSimpleHTML(ctx, "本次未锁定任何目标基质。")
		return
	}
	summary := st.MatchedCombinationSummary
	type viewItem struct {
		Key string
		*matchapi.SkillCombinationSummary
	}
	items := make([]viewItem, 0, len(summary))
	for k, v := range summary {
		items = append(items, viewItem{Key: k, SkillCombinationSummary: v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })

	var b strings.Builder
	b.WriteString(`<div style="color: #00bfff; font-weight: 900; margin-top: 4px;">战利品摘要：</div>`)
	b.WriteString(`<table style="width: 100%; border-collapse: collapse; font-size: 12px;">`)
	b.WriteString(`<tr><th style="text-align:left; padding: 2px 4px;">武器</th><th style="text-align:left; padding: 2px 4px;">技能组合</th><th style="text-align:right; padding: 2px 4px;">锁定数量</th></tr>`)
	for _, item := range items {
		weaponText := formatWeaponNamesColoredHTML(item.Weapons)
		skillSource := item.OCRSkills
		if len(skillSource) == 0 {
			skillSource = item.SkillsChinese
		}
		formattedSkills := make([]string, len(skillSource))
		for i, s := range skillSource {
			formattedSkills[i] = fmt.Sprintf(`<span style="color: #064d7c;">%s</span>`, escapeHTML(s))
		}
		b.WriteString("<tr>")
		b.WriteString(fmt.Sprintf(`<td style="padding: 2px 4px;">%s</td><td style="padding: 2px 4px;">%s</td><td style="padding: 2px 4px; text-align: right;">%d</td>`,
			weaponText, strings.Join(formattedSkills, " | "), item.Count))
		b.WriteString("</tr>")
	}
	b.WriteString(`</table>`)
	LogMXUHTML(ctx, b.String())
}

func formatWeaponNamesColoredHTML(weapons []matchapi.WeaponData) string {
	if len(weapons) == 0 {
		return ""
	}
	var b strings.Builder
	for i, w := range weapons {
		if i > 0 {
			b.WriteString("、")
		}
		b.WriteString(fmt.Sprintf(`<span style="color: %s;">%s</span>`, getColorForRarity(w.Rarity), escapeHTML(w.ChineseName)))
	}
	return b.String()
}

func logSkillPools(engine *matchapi.Engine) {
	if engine == nil {
		return
	}
	for _, entry := range []struct {
		slot string
		pool []matchapi.SkillPool
	}{
		{"Slot1", engine.SkillPools().Slot1},
		{"Slot2", engine.SkillPools().Slot2},
		{"Slot3", engine.SkillPools().Slot3},
	} {
		for _, s := range entry.pool {
			log.Info().
				Str("component", "EssenceFilter").
				Str("slot", entry.slot).
				Int("id", s.ID).
				Str("skill", s.Chinese).
				Msg("skill pool")
		}
	}
}

func logFilteredSkillStats() {
	st := getRunState()
	if st == nil {
		return
	}
	if st.MatchEngine == nil {
		return
	}
	for slotIdx, stat := range st.FilteredSkillStats {
		slot := slotIdx + 1
		var pool []matchapi.SkillPool
		switch slot {
		case 1:
			pool = st.MatchEngine.SkillPools().Slot1
		case 2:
			pool = st.MatchEngine.SkillPools().Slot2
		case 3:
			pool = st.MatchEngine.SkillPools().Slot3
		}
		idToName := make(map[int]string, len(pool))
		for _, s := range pool {
			idToName[s.ID] = s.Chinese
		}
		ids := make([]int, 0, len(stat))
		for id := range stat {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		for _, id := range ids {
			name := idToName[id]
			log.Info().
				Str("component", "EssenceFilter").
				Int("slot", slot).
				Int("skill_id", id).
				Str("skill", name).
				Int("count", stat[id]).
				Msg("filtered skill stats")
		}
	}
}

// --- 预刻写方案推荐（同上 case）---

type calcPlan struct {
	slot1Names [3]string
	fixedSlot  int
	fixedID    int
	fixedName  string
	needs      []matchapi.WeaponData
	matched    []matchapi.WeaponData
}

func spanColor(color, text string) string {
	return fmt.Sprintf(`<span style="color:%s;">%s</span>`, color, text)
}

func planCardHTML(borderColor string, idx int, p calcPlan, fixedSlotLabel [4]string) string {
	return fmt.Sprintf(
		`<div style="margin-top:3px;border-left:3px solid %s;padding-left:6px;">%s 基础属性：%s | 选择%s：%s<br>满足 <b>%d</b> 个需求 / 匹配 <b>%d</b> 件目标武器<br>满足的需求：%s<br>匹配的武器：%s</div>`,
		borderColor,
		spanColor("#98c379", fmt.Sprintf("方案 %d", idx)),
		spanColor("#47b5ff", escapeHTML(strings.Join(p.slot1Names[:], "，"))),
		fixedSlotLabel[p.fixedSlot], spanColor("#e877fe", escapeHTML(p.fixedName)),
		len(p.needs), len(p.matched),
		weaponListHTML(p.needs), weaponListHTML(p.matched),
	)
}

type skillIndex map[int]map[int][]matchapi.WeaponData

func buildSkillIndex(allTargets []matchapi.SkillCombination, slotIdx int) skillIndex {
	idx := make(skillIndex)
	for _, combo := range allTargets {
		s1, sN := combo.SkillIDs[0], combo.SkillIDs[slotIdx]
		if idx[s1] == nil {
			idx[s1] = make(map[int][]matchapi.WeaponData)
		}
		idx[s1][sN] = append(idx[s1][sN], combo.Weapon)
	}
	return idx
}

func weaponListHTML(weapons []matchapi.WeaponData) string {
	if len(weapons) == 0 {
		return "（无）"
	}
	parts := make([]string, len(weapons))
	for i, w := range weapons {
		parts[i] = fmt.Sprintf(`<span style="color:%s;">%s</span>`, getColorForRarity(w.Rarity), escapeHTML(w.ChineseName))
	}
	return strings.Join(parts, "，")
}

func logCalculatorResult(ctx *maa.Context) {
	st := getRunState()
	if st == nil {
		return
	}
	po := &st.PipelineOpts
	selectedRarities := make(map[int]bool)
	if po.Rarity4Weapon {
		selectedRarities[4] = true
	}
	if po.Rarity5Weapon {
		selectedRarities[5] = true
	}
	if po.Rarity6Weapon {
		selectedRarities[6] = true
	}
	if st.MatchEngine == nil {
		return
	}
	if len(st.TargetSkillCombinations) == 0 {
		LogMXUSimpleHTML(ctx, "未选择武器目标，不生成预刻写方案。")
		return
	}
	graduated := make(map[string]bool)
	for _, s := range st.MatchedCombinationSummary {
		for _, w := range s.Weapons {
			graduated[w.ChineseName] = true
		}
	}
	var allTargets, ungraduated []matchapi.SkillCombination
	seenTarget := make(map[string]bool)
	for _, combo := range st.TargetSkillCombinations {
		if len(selectedRarities) > 0 && !selectedRarities[combo.Weapon.Rarity] {
			continue
		}
		name := combo.Weapon.ChineseName
		if seenTarget[name] {
			continue
		}
		seenTarget[name] = true
		allTargets = append(allTargets, combo)
		if !graduated[name] {
			ungraduated = append(ungraduated, combo)
		}
	}
	if len(ungraduated) == 0 {
		LogMXUSimpleHTML(ctx, "所有目标武器本次均已命中，无需推荐预刻写方案。")
		return
	}

	slot1Pool := st.MatchEngine.SkillPools().Slot1
	slot2Pool := st.MatchEngine.SkillPools().Slot2
	slot3Pool := st.MatchEngine.SkillPools().Slot3
	n1 := len(slot1Pool)
	const maxPlansPerLocation = 2
	fixedSlotLabel := [4]string{"", "", "附加属性", "技能属性"}
	idx2 := buildSkillIndex(allTargets, 1)
	idx3 := buildSkillIndex(allTargets, 2)

	lookupWeapons := func(idx skillIndex, s1Set [3]int, fixedID int) (matched, needs []matchapi.WeaponData) {
		for _, s1ID := range s1Set {
			for _, w := range idx[s1ID][fixedID] {
				matched = append(matched, w)
				if !graduated[w.ChineseName] {
					needs = append(needs, w)
				}
			}
		}
		return
	}
	enumPlans := func(availSlot2, availSlot3 []matchapi.SkillPool) []calcPlan {
		var plans []calcPlan
		for i := 0; i < n1-2; i++ {
			for j := i + 1; j < n1-1; j++ {
				for k := j + 1; k < n1; k++ {
					s1Names := [3]string{slot1Pool[i].Chinese, slot1Pool[j].Chinese, slot1Pool[k].Chinese}
					s1IDs := [3]int{slot1Pool[i].ID, slot1Pool[j].ID, slot1Pool[k].ID}
					for _, s2 := range availSlot2 {
						matched, needs := lookupWeapons(idx2, s1IDs, s2.ID)
						if len(needs) > 0 {
							plans = append(plans, calcPlan{slot1Names: s1Names, fixedSlot: 2, fixedName: s2.Chinese, fixedID: s2.ID, needs: needs, matched: matched})
						}
					}
					for _, s3 := range availSlot3 {
						matched, needs := lookupWeapons(idx3, s1IDs, s3.ID)
						if len(needs) > 0 {
							plans = append(plans, calcPlan{slot1Names: s1Names, fixedSlot: 3, fixedName: s3.Chinese, fixedID: s3.ID, needs: needs, matched: matched})
						}
					}
				}
			}
		}
		sort.Slice(plans, func(i, j int) bool {
			if len(plans[i].needs) != len(plans[j].needs) {
				return len(plans[i].needs) > len(plans[j].needs)
			}
			return len(plans[i].matched) > len(plans[j].matched)
		})
		return plans
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<div style="color:#00bfff;font-weight:900;margin-top:8px;">预刻写方案推荐（%d 个未毕业需求）：</div>`, len(ungraduated)))
	b.WriteString(weaponListHTML(func() []matchapi.WeaponData {
		ws := make([]matchapi.WeaponData, 0, len(ungraduated))
		for _, c := range ungraduated {
			ws = append(ws, c.Weapon)
		}
		return ws
	}()))
	b.WriteString(`<br>`)

	if len(st.MatchEngine.Locations()) > 0 {
		for _, loc := range st.MatchEngine.Locations() {
			slot2Set := make(map[int]bool)
			for _, id := range loc.Slot2IDs {
				slot2Set[id] = true
			}
			slot3Set := make(map[int]bool)
			for _, id := range loc.Slot3IDs {
				slot3Set[id] = true
			}
			var locSlot2, locSlot3 []matchapi.SkillPool
			for _, s := range slot2Pool {
				if slot2Set[s.ID] {
					locSlot2 = append(locSlot2, s)
				}
			}
			for _, s := range slot3Pool {
				if slot3Set[s.ID] {
					locSlot3 = append(locSlot3, s)
				}
			}
			plans := enumPlans(locSlot2, locSlot3)
			if len(plans) == 0 {
				continue
			}
			b.WriteString(fmt.Sprintf(`<div style="color:#c8960c;font-weight:900;margin-top:6px;">%s</div>`, escapeHTML(loc.Name)))
			show := maxPlansPerLocation
			if len(plans) < show {
				show = len(plans)
			}
			for idx, p := range plans[:show] {
				b.WriteString(planCardHTML("#c8960c", idx+1, p, fixedSlotLabel))
			}
		}
	} else {
		plans := enumPlans(slot2Pool, slot3Pool)
		show := 10
		if len(plans) < show {
			show = len(plans)
		}
		for idx, p := range plans[:show] {
			b.WriteString(planCardHTML("#00bfff", idx+1, p, fixedSlotLabel))
		}
	}
	LogMXUHTML(ctx, b.String())
}
