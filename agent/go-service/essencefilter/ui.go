package essencefilter

import (
	"html"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// View types for HTML templates.
type (
	weaponColorView struct {
		Name  string
		Color string
	}
	lootSummaryRow struct {
		Weapons []weaponColorView
		Skills  []string
		Count   int
	}
	planSectionView struct {
		Name  string
		Color string
		Cards []string
	}
)

func LogMXUHTML(ctx *maa.Context, htmlText string) {
	htmlText = strings.TrimLeft(htmlText, " \t\r\n")
	maafocus.Print(ctx, htmlText)
}

// LogMXUSimpleHTMLWithColor logs a simple styled span, allowing a custom color.
func LogMXUSimpleHTMLWithColor(ctx *maa.Context, text string, color string) {
	LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.simple_message", map[string]any{
		"Text":  text,
		"Color": color,
	}))
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

// --- 战利品摘要与预刻写方案（同一 case：本次运行的结果展示）---

// logMatchSummary - 输出“战利品 summary”，按技能组合聚合统计
func logMatchSummary(ctx *maa.Context) {
	st := getRunState()
	if st == nil || len(st.MatchedCombinationSummary) == 0 {
		LogMXUSimpleHTML(ctx, i18n.T("essencefilter.no_locked"))
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

	rows := make([]lootSummaryRow, 0, len(items))
	for _, item := range items {
		weapons := make([]weaponColorView, 0, len(item.Weapons))
		for _, w := range item.Weapons {
			weapons = append(weapons, weaponColorView{Name: w.ChineseName, Color: getColorForRarity(w.Rarity)})
		}
		skillSource := item.OCRSkills
		if len(skillSource) == 0 {
			skillSource = item.SkillsChinese
		}
		rows = append(rows, lootSummaryRow{Weapons: weapons, Skills: skillSource, Count: item.Count})
	}
	LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.loot_summary", map[string]any{
		"Items": rows,
	}))
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

func planCardHTML(borderColor string, idx int, p calcPlan, fixedSlotLabel [4]string) string {
	return i18n.RenderHTML("essencefilter.plan_card", map[string]any{
		"BorderColor":    borderColor,
		"PlanIndex":      idx,
		"Slot1Text":      escapeHTML(strings.Join(p.slot1Names[:], i18n.Separator())),
		"FixedSlotLabel": fixedSlotLabel[p.fixedSlot],
		"FixedName":      escapeHTML(p.fixedName),
		"NeedsCount":     len(p.needs),
		"MatchedCount":   len(p.matched),
		"Needs":          weaponsToViews(p.needs),
		"Matched":        weaponsToViews(p.matched),
	})
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

// buildFeasibleWeaponSet returns weapon-name set that is feasible at one location.
// A weapon is feasible only when both target slot2 and slot3 IDs are available at this location.
func buildFeasibleWeaponSet(
	allTargets []matchapi.SkillCombination,
	slot2Set map[int]bool,
	slot3Set map[int]bool,
) map[string]bool {
	feasible := make(map[string]bool)
	for _, combo := range allTargets {
		if len(combo.SkillIDs) < 3 {
			continue
		}
		if slot2Set[combo.SkillIDs[1]] && slot3Set[combo.SkillIDs[2]] {
			feasible[combo.Weapon.ChineseName] = true
		}
	}
	return feasible
}

func weaponsToViews(weapons []matchapi.WeaponData) []weaponColorView {
	views := make([]weaponColorView, len(weapons))
	for i, w := range weapons {
		views[i] = weaponColorView{Name: w.ChineseName, Color: getColorForRarity(w.Rarity)}
	}
	return views
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
		LogMXUSimpleHTML(ctx, i18n.T("essencefilter.no_weapon_target"))
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
		LogMXUSimpleHTML(ctx, i18n.T("essencefilter.all_graduated"))
		return
	}

	slot1Pool := st.MatchEngine.SkillPools().Slot1
	slot2Pool := st.MatchEngine.SkillPools().Slot2
	slot3Pool := st.MatchEngine.SkillPools().Slot3
	n1 := len(slot1Pool)
	const maxPlansPerLocation = 2
	fixedSlotLabel := [4]string{"", "", i18n.T("essencefilter.slot_fixed_label_2"), i18n.T("essencefilter.slot_fixed_label_3")}
	idx2 := buildSkillIndex(allTargets, 1)
	idx3 := buildSkillIndex(allTargets, 2)

	lookupWeapons := func(
		idx skillIndex,
		s1Set [3]int,
		fixedID int,
		feasible map[string]bool,
	) (matched, needs []matchapi.WeaponData) {
		for _, s1ID := range s1Set {
			for _, w := range idx[s1ID][fixedID] {
				if feasible != nil && !feasible[w.ChineseName] {
					continue
				}
				matched = append(matched, w)
				if !graduated[w.ChineseName] {
					needs = append(needs, w)
				}
			}
		}
		return
	}
	enumPlans := func(
		availSlot2, availSlot3 []matchapi.SkillPool,
		feasible map[string]bool,
	) []calcPlan {
		var plans []calcPlan
		for i := 0; i < n1-2; i++ {
			for j := i + 1; j < n1-1; j++ {
				for k := j + 1; k < n1; k++ {
					s1Names := [3]string{slot1Pool[i].Chinese, slot1Pool[j].Chinese, slot1Pool[k].Chinese}
					s1IDs := [3]int{slot1Pool[i].ID, slot1Pool[j].ID, slot1Pool[k].ID}
					for _, s2 := range availSlot2 {
						matched, needs := lookupWeapons(idx2, s1IDs, s2.ID, feasible)
						if len(needs) > 0 {
							plans = append(plans, calcPlan{slot1Names: s1Names, fixedSlot: 2, fixedName: s2.Chinese, fixedID: s2.ID, needs: needs, matched: matched})
						}
					}
					for _, s3 := range availSlot3 {
						matched, needs := lookupWeapons(idx3, s1IDs, s3.ID, feasible)
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

	ungraduatedWeapons := make([]matchapi.WeaponData, 0, len(ungraduated))
	for _, c := range ungraduated {
		ungraduatedWeapons = append(ungraduatedWeapons, c.Weapon)
	}

	locations := st.MatchEngine.Locations()
	if len(locations) == 0 {
		log.Info().
			Str("component", "EssenceFilter").
			Str("step", "PlanRecommend").
			Msg("skip recommend output: no locations loaded")
		return
	}

	var sections []planSectionView
	for _, loc := range locations {
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
		feasible := buildFeasibleWeaponSet(allTargets, slot2Set, slot3Set)
		plans := enumPlans(locSlot2, locSlot3, feasible)
		if len(plans) == 0 {
			continue
		}
		show := maxPlansPerLocation
		if len(plans) < show {
			show = len(plans)
		}
		cards := make([]string, show)
		for idx, p := range plans[:show] {
			cards[idx] = planCardHTML("#c8960c", idx+1, p, fixedSlotLabel)
		}
		sections = append(sections, planSectionView{Name: loc.Name, Color: "#c8960c", Cards: cards})
	}

	if len(sections) == 0 {
		log.Info().
			Str("component", "EssenceFilter").
			Str("step", "PlanRecommend").
			Msg("skip recommend output: no feasible location plans")
		LogMXUSimpleHTML(ctx, i18n.T("essencefilter.focus.plan.no_feasible_location_plans"))
	}
	LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.plan_recommend", map[string]any{
		"UngraduatedCount":   len(ungraduated),
		"UngraduatedWeapons": weaponsToViews(ungraduatedWeapons),
		"Sections":           sections,
	}))
}
