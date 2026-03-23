package essencefilter

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

func dataDirFromResourceBase() string {
	base := getResourceBase()
	if base == "" {
		base = "data"
	}
	return filepath.Join(base, "EssenceFilter")
}

func getLocaleFromState(st *RunState) string {
	if st == nil {
		return matchapi.LocaleCN
	}
	loc := matchapi.NormalizeInputLocale(st.InputLanguage)
	if loc == "" {
		return matchapi.LocaleCN
	}
	return loc
}

func reportFocusByKey(ctx *maa.Context, st *RunState, key string, args ...any) {
	maafocus.NodeActionStarting(ctx, matchapi.FormatMessage(getLocaleFromState(st), key, args...))
}

func reportSimpleByKey(ctx *maa.Context, st *RunState, key string, args ...any) {
	LogMXUSimpleHTML(ctx, matchapi.FormatMessage(getLocaleFromState(st), key, args...))
}

func reportColoredByKey(ctx *maa.Context, st *RunState, color string, key string, args ...any) {
	LogMXUSimpleHTMLWithColor(ctx, matchapi.FormatMessage(getLocaleFromState(st), key, args...), color)
}

func buildMatchOptions(st *RunState) matchapi.EssenceFilterOptions {
	if st == nil {
		return matchapi.EssenceFilterOptions{}
	}
	return matchOptsFromPipeline(&st.PipelineOpts)
}

func reportOCRSkills(ctx *maa.Context, engine *matchapi.Engine, skills []string, levels [3]int, matched bool) {
	if engine == nil {
		return
	}
	color := "#00bfff"
	if matched {
		color = "#064d7c"
	}
	LogMXUSimpleHTMLWithColor(ctx, engine.FocusOCRSkills(skills, levels), color)
}

func reportMatchedWeapons(ctx *maa.Context, engine *matchapi.Engine, weapons []matchapi.WeaponData) {
	if engine == nil {
		return
	}
	var weaponsHTML strings.Builder
	for i, w := range weapons {
		if i > 0 {
			weaponsHTML.WriteString("、")
		}
		weaponsHTML.WriteString(fmt.Sprintf(`<span style="color: %s;">%s</span>`, getColorForRarity(w.Rarity), escapeHTML(w.ChineseName)))
	}
	LogMXUHTML(ctx, engine.FocusMatchedWeapons(weaponsHTML.String()))
}

func reportExtRule(ctx *maa.Context, engine *matchapi.Engine, reason string, shouldLock bool) {
	if engine == nil {
		return
	}
	if shouldLock {
		LogMXUHTML(ctx, engine.FocusExtRuleLock(escapeHTML(reason)))
		return
	}
	LogMXUHTML(ctx, engine.FocusExtRuleNoop(escapeHTML(reason)))
}

func reportNoMatch(ctx *maa.Context, engine *matchapi.Engine, shouldDiscard bool) {
	if engine == nil {
		return
	}
	if shouldDiscard {
		LogMXUHTML(ctx, engine.FocusNoMatchDiscard())
		return
	}
	LogMXUSimpleHTML(ctx, engine.FocusNoMatchSkip())
}

type InitViewModel struct {
	FilteredWeapons []matchapi.WeaponData
	SlotSkills      [3][]string
}

func buildInitViewModel(st *RunState) InitViewModel {
	vm := InitViewModel{
		FilteredWeapons: make([]matchapi.WeaponData, 0, len(st.TargetSkillCombinations)),
	}
	if st == nil {
		return vm
	}

	uniqueNameSlots := [3]map[int]string{}
	for i := 0; i < 3; i++ {
		uniqueNameSlots[i] = make(map[int]string)
	}

	for _, combo := range st.TargetSkillCombinations {
		vm.FilteredWeapons = append(vm.FilteredWeapons, combo.Weapon)
		for i, skillID := range combo.SkillIDs {
			if i >= 0 && i < 3 && i < len(combo.SkillsChinese) {
				uniqueNameSlots[i][skillID] = combo.SkillsChinese[i]
			}
		}
	}

	sort.Slice(vm.FilteredWeapons, func(i, j int) bool { return vm.FilteredWeapons[i].Rarity > vm.FilteredWeapons[j].Rarity })

	for i := 0; i < 3; i++ {
		skillNames := make([]string, 0, len(uniqueNameSlots[i]))
		for _, name := range uniqueNameSlots[i] {
			if name != "" {
				skillNames = append(skillNames, name)
			}
		}
		sort.Strings(skillNames)
		vm.SlotSkills[i] = skillNames
	}
	return vm
}

func reportInitSelection(ctx *maa.Context, st *RunState, weaponRarity []int, essenceTypes []EssenceMeta) {
	if len(weaponRarity) == 0 {
		reportSimpleByKey(ctx, st, "focus.init.no_weapon_rarity")
	} else {
		reportSimpleByKey(ctx, st, "focus.init.selected_rarity", rarityListToString(weaponRarity))
	}
	reportSimpleByKey(ctx, st, "focus.init.selected_essence", essenceListToString(essenceTypes))
}

func reportInitWeapons(ctx *maa.Context, st *RunState, weapons []matchapi.WeaponData) {
	if len(weapons) == 0 {
		reportSimpleByKey(ctx, st, "focus.init.filtered_count_ext_only")
		reportSimpleByKey(ctx, st, "focus.init.no_weapon_list")
		return
	}
	reportSimpleByKey(ctx, st, "focus.init.filtered_count", len(weapons))
	var b strings.Builder
	const columns = 3
	b.WriteString(`<table style="width: 100%; border-collapse: collapse;">`)
	for i, w := range weapons {
		if i%columns == 0 {
			b.WriteString("<tr>")
		}
		b.WriteString(fmt.Sprintf(`<td style="padding: 2px 8px; color: %s; font-size: 11px;">%s</td>`, getColorForRarity(w.Rarity), w.ChineseName))
		if i%columns == columns-1 || i == len(weapons)-1 {
			b.WriteString("</tr>")
		}
	}
	b.WriteString("</table>")
	LogMXUHTML(ctx, b.String())
}

func reportInitSkillList(ctx *maa.Context, st *RunState, slotSkills [3][]string) {
	total := len(slotSkills[0]) + len(slotSkills[1]) + len(slotSkills[2])
	if total == 0 {
		reportSimpleByKey(ctx, st, "focus.init.no_skill_list")
		return
	}

	const columns = 3
	slotColors := []string{"#47b5ff", "#11dd11", "#e877fe"}
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<div style="color: #00bfff; font-weight: 900;">%s</div>`, matchapi.FormatMessage(getLocaleFromState(st), "focus.init.skill_list_title")))
	for i := 0; i < 3; i++ {
		if len(slotSkills[i]) == 0 {
			continue
		}
		slotColor := slotColors[i]
		b.WriteString(fmt.Sprintf(`<div style="color: %s; font-weight: 700;">%s</div>`, slotColor, matchapi.FormatMessage(getLocaleFromState(st), "focus.init.slot_label", i+1)))
		b.WriteString(fmt.Sprintf(`<table style="width: 100%%; color: %s; border-collapse: collapse;">`, slotColor))
		for j, name := range slotSkills[i] {
			if j%columns == 0 {
				b.WriteString("<tr>")
			}
			b.WriteString(fmt.Sprintf(`<td style="padding: 2px 8px; font-size: 12px;">%s</td>`, name))
			if j%columns == columns-1 || j == len(slotSkills[i])-1 {
				b.WriteString("</tr>")
			}
		}
		b.WriteString("</table>")
	}
	LogMXUHTML(ctx, b.String())
}

func reportDataVersionNotice(ctx *maa.Context, st *RunState) {
	if st == nil || st.MatchEngine == nil {
		return
	}
	v := strings.TrimSpace(st.MatchEngine.DataVersion())
	if v == "" {
		return
	}
	LogMXUHTML(ctx, matchapi.FormatMessage(getLocaleFromState(st), "focus.data_version.notice", v))
}

func reportFinishExtRuleStats(ctx *maa.Context, st *RunState) {
	if st == nil {
		return
	}
	po := &st.PipelineOpts
	if po.KeepFuturePromising {
		reportColoredByKey(ctx, st, "#064d7c", "focus.finish.ext_future", st.ExtFuturePromisingCount)
	}
	if po.KeepSlot3Level3Practical {
		reportColoredByKey(ctx, st, "#064d7c", "focus.finish.ext_practical", st.ExtSlot3PracticalCount)
	}
}

func reportFinishArtifacts(ctx *maa.Context, st *RunState) {
	if st == nil {
		return
	}
	logMatchSummary(ctx)
	if st.PipelineOpts.ExportCalculatorScript {
		logCalculatorResult(ctx)
	}
}

type decisionNextNodes struct {
	Lock    string
	Discard string
	Skip    string
}

func runUnifiedSkillDecision(
	ctx *maa.Context,
	arg *maa.CustomActionArg,
	st *RunState,
	engine *matchapi.Engine,
	ocr matchapi.OCRInput,
	next decisionNextNodes,
) bool {
	skills := []string{ocr.Skills[0], ocr.Skills[1], ocr.Skills[2]}

	matchResult, err := engine.MatchOCR(ocr, buildMatchOptions(st))
	if err != nil || matchResult == nil {
		if err != nil {
			reportFocusByKey(ctx, st, "focus.error.match_failed", err.Error())
		} else {
			reportFocusByKey(ctx, st, "focus.error.match_failed", "nil match result")
		}
		return false
	}

	extendedReason := matchResult.Reason
	reportOCRSkills(ctx, engine, skills, ocr.Levels, matchResult.Kind != matchapi.MatchNone)

	switch matchResult.Kind {
	case matchapi.MatchExact:
		st.MatchedCount++
		reportMatchedWeapons(ctx, engine, matchResult.Weapons)

		key := skillCombinationKey(matchResult.SkillIDs)
		if key != "" {
			if s, ok := st.MatchedCombinationSummary[key]; ok {
				s.Count++
			} else {
				st.MatchedCombinationSummary[key] = &matchapi.SkillCombinationSummary{
					SkillIDs:      append([]int(nil), matchResult.SkillIDs...),
					SkillsChinese: append([]string(nil), matchResult.SkillsChinese...),
					OCRSkills:     append([]string(nil), skills...),
					Weapons:       append([]matchapi.WeaponData(nil), matchResult.Weapons...),
					Count:         1,
				}
			}
		}
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: next.Lock}})

	case matchapi.MatchFuturePromising, matchapi.MatchSlot3Level3Practical:
		if matchResult.Kind == matchapi.MatchFuturePromising {
			st.ExtFuturePromisingCount++
		} else {
			st.ExtSlot3PracticalCount++
		}

		if matchResult.ShouldLock {
			st.MatchedCount++
			reportExtRule(ctx, engine, extendedReason, true)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: next.Lock}})
		} else {
			reportExtRule(ctx, engine, extendedReason, false)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: next.Skip}})
		}

	case matchapi.MatchNone:
		if matchResult.ShouldDiscard {
			reportNoMatch(ctx, engine, true)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: next.Discard}})
		} else {
			reportNoMatch(ctx, engine, false)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: next.Skip}})
		}
	}

	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
	return true
}

// EnsureMatchEngine centralizes engine initialization and reuse logic.
// If run state already has an engine, it is reused directly.
// Otherwise, options + locale are read from node attach and an engine is loaded.
func EnsureMatchEngine(ctx *maa.Context, st *RunState, nodeName string) (*matchapi.Engine, *EssenceFilterOptions, error) {
	if st != nil && st.MatchEngine != nil {
		opts := st.PipelineOpts
		return st.MatchEngine, &opts, nil
	}

	opts, err := getOptionsFromAttach(ctx, nodeName)
	if err != nil {
		return nil, nil, fmt.Errorf("load options from %s: %w", nodeName, err)
	}

	locale := matchapi.NormalizeInputLocale(opts.InputLanguage)
	engine, err := matchapi.NewEngineFromDirWithLocale(dataDirFromResourceBase(), locale)
	if err != nil {
		return nil, nil, fmt.Errorf("load match engine: %w", err)
	}

	if st != nil {
		st.PipelineOpts = *opts
		st.InputLanguage = locale
		st.MatchEngine = engine
	}
	return engine, opts, nil
}
