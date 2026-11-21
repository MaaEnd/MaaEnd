package essencefilter

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

const essenceFilterDataDir = "data/EssenceFilter"

func reportFocusByKey(ctx *maa.Context, key string, args ...any) {
	maafocus.Print(ctx, i18n.T("essencefilter."+key, args...))
}

func reportSimpleByKey(ctx *maa.Context, key string, args ...any) {
	LogMXUSimpleHTML(ctx, i18n.T("essencefilter."+key, args...))
}

func reportColoredByKey(ctx *maa.Context, color string, key string, args ...any) {
	LogMXUSimpleHTMLWithColor(ctx, i18n.T("essencefilter."+key, args...), color)
}

func buildMatchOptions(st *RunState) matchapi.EssenceFilterOptions {
	return matchOptsFromPipeline(&st.PipelineOpts)
}

func reportOCRSkills(ctx *maa.Context, skills []string, levels [3]int, matched bool) {
	color := "#00bfff"
	if matched {
		color = "#064d7c"
	}
	text := i18n.T("essencefilter.focus.ocr_skills",
		skills[0], levels[0], skills[1], levels[1], skills[2], levels[2])
	LogMXUSimpleHTMLWithColor(ctx, text, color)
}

func reportMatchedWeapons(ctx *maa.Context, weapons []matchapi.WeaponData) {
	LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.matched_weapons", map[string]any{
		"Weapons": weaponsToViews(weapons),
	}))
}

func reportExtRule(ctx *maa.Context, reason string, shouldLock bool) {
	if shouldLock {
		LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.ext_rule_lock", map[string]any{
			"Reason": escapeHTML(reason),
		}))
		return
	}
	LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.ext_rule_noop", map[string]any{
		"Reason": escapeHTML(reason),
	}))
}

func reportNoMatch(ctx *maa.Context, shouldDiscard bool) {
	if shouldDiscard {
		LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.no_match_discard", nil))
		return
	}
	LogMXUSimpleHTML(ctx, i18n.T("essencefilter.focus.no_match_skip"))
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

func reportInitSelection(ctx *maa.Context, weaponRarity []int, essenceTypes []string) {
	if len(weaponRarity) == 0 {
		reportSimpleByKey(ctx, "focus.init.no_weapon_rarity")
	} else {
		reportSimpleByKey(ctx, "focus.init.selected_rarity", rarityListToString(weaponRarity))
	}
	reportSimpleByKey(ctx, "focus.init.selected_essence", essenceListToString(essenceTypes))
}

func reportInitWeapons(ctx *maa.Context, weapons []matchapi.WeaponData) {
	if len(weapons) == 0 {
		reportSimpleByKey(ctx, "focus.init.filtered_count_ext_only")
		reportSimpleByKey(ctx, "focus.init.no_weapon_list")
		return
	}
	reportSimpleByKey(ctx, "focus.init.filtered_count", len(weapons))
	const columns = 3
	var rows [][]weaponColorView
	var row []weaponColorView
	for i, w := range weapons {
		row = append(row, weaponColorView{Name: w.ChineseName, Color: getColorForRarity(w.Rarity)})
		if (i+1)%columns == 0 || i == len(weapons)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.init_weapons", map[string]any{
		"Rows": rows,
	}))
}

func reportInitSkillList(ctx *maa.Context, slotSkills [3][]string) {
	total := len(slotSkills[0]) + len(slotSkills[1]) + len(slotSkills[2])
	if total == 0 {
		reportSimpleByKey(ctx, "focus.init.no_skill_list")
		return
	}

	const columns = 3
	slotColors := []string{"#47b5ff", "#11dd11", "#e877fe"}
	type slotView struct {
		Color  string
		Label  string
		Skills []string
		Rows   [][]string
	}
	var slots []slotView
	for i := 0; i < 3; i++ {
		if len(slotSkills[i]) == 0 {
			continue
		}
		var rows [][]string
		var row []string
		for j, name := range slotSkills[i] {
			row = append(row, name)
			if (j+1)%columns == 0 || j == len(slotSkills[i])-1 {
				rows = append(rows, row)
				row = nil
			}
		}
		slots = append(slots, slotView{
			Color:  slotColors[i],
			Label:  i18n.T("essencefilter.focus.init.slot_label", i+1),
			Skills: slotSkills[i],
			Rows:   rows,
		})
	}
	LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.init_skills", map[string]any{
		"Title": i18n.T("essencefilter.focus.init.skill_list_title"),
		"Slots": slots,
	}))
}

func reportDataVersionNotice(ctx *maa.Context, st *RunState) {
	if st == nil || st.MatchEngine == nil {
		return
	}
	v := strings.TrimSpace(st.MatchEngine.DataVersion())
	if v == "" {
		return
	}
	LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.data_version_notice", map[string]any{
		"Version": v,
	}))
}

func reportFinishExtRuleStats(ctx *maa.Context, st *RunState) {
	if st == nil {
		return
	}
	po := &st.PipelineOpts
	if po.KeepFuturePromising {
		reportColoredByKey(ctx, "#064d7c", "focus.finish.ext_future", st.ExtFuturePromisingCount)
	}
	if po.KeepSlot3Level3Practical {
		reportColoredByKey(ctx, "#064d7c", "focus.finish.ext_practical", st.ExtSlot3PracticalCount)
	}
}

func reportFinishArtifacts(ctx *maa.Context, st *RunState) {
	if st == nil {
		return
	}
	logMatchSummary(ctx, st)
	if st.PipelineOpts.ExportCalculatorScript {
		logCalculatorResult(ctx, st)
	}
}

type decisionNextNodes struct {
	Lock    string
	Discard string
	Skip    string
}

func (next decisionNextNodes) route(ctx *maa.Context, task, mark string) bool {
	node := next.Skip
	switch mark {
	case lockedMark:
		node = next.Lock
	case discarded:
		node = next.Discard
	}
	if err := ctx.OverrideNext(task, []maa.NextItem{{Name: node}}); err != nil {
		return inventoryFailed(err)
	}
	return true
}

func decideSkillMark(ctx *maa.Context, st *RunState, ocr matchapi.OCRInput) (string, bool) {
	matchResult, err := st.MatchEngine.MatchOCR(ocr, buildMatchOptions(st))
	if err != nil || matchResult == nil {
		if err != nil {
			reportFocusByKey(ctx, "focus.error.match_failed", err.Error())
		} else {
			reportFocusByKey(ctx, "focus.error.match_failed", "nil match result")
		}
		return "", false
	}
	return applySkillMark(ctx, st, ocr, matchResult)
}

// Mark from the same resolved weapon IDs that go into the report and target stock.
func decideInventoryMark(ctx *maa.Context, st *RunState, ocr matchapi.OCRInput, item inventoryItem) (string, bool) {
	result := &matchapi.MatchResult{
		Kind:          matchapi.MatchNone,
		ShouldDiscard: st.PipelineOpts.DiscardUnmatched && len(item.weaponIDs) == 0,
	}
	for _, weapon := range st.MatchEngine.Weapons() {
		if (weapon.Rarity == 5 || weapon.Rarity == 6) && slices.Contains(item.weaponIDs, weapon.InternalID) {
			result.Weapons = append(result.Weapons, weapon)
		}
	}
	if len(result.Weapons) > 0 {
		weapon := result.Weapons[0]
		result.Kind, result.ShouldLock = matchapi.MatchExact, true
		result.SkillIDs, result.SkillsChinese = weapon.SkillIDs, weapon.SkillsChinese
		result.Levels = item.groupRow().Levels
	}
	return applySkillMark(ctx, st, ocr, result)
}

func applySkillMark(ctx *maa.Context, st *RunState, ocr matchapi.OCRInput, matchResult *matchapi.MatchResult) (string, bool) {
	skills := ocr.Skills[:]
	reportOCRSkills(ctx, skills, ocr.Levels, matchResult.Kind != matchapi.MatchNone)

	switch matchResult.Kind {
	case matchapi.MatchExact:
		if verdict := st.LockLimit.applyToExact(matchResult, st.CurrentLocked); verdict.Quota > 0 {
			reportLockLimit(ctx, verdict, matchResult, st.CurrentLocked)
			if !matchResult.ShouldLock {
				break
			}
			matchResult.Weapons = slices.DeleteFunc(slices.Clone(matchResult.Weapons), func(weapon matchapi.WeaponData) bool {
				return !isLockLimitTarget(weapon, st.PipelineOpts.LockLimitWeaponIDs)
			})
		}
		reportMatchedWeapons(ctx, matchResult.Weapons)
		st.MatchedCount++

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
	case matchapi.MatchFuturePromising, matchapi.MatchSlot3Level3Practical:
		var reason string
		if matchResult.Kind == matchapi.MatchFuturePromising {
			st.ExtFuturePromisingCount++
			reason = i18n.T("essencefilter.reason.future_promising",
				matchResult.ExtLevelSum, matchResult.ExtMinTotal)
		} else {
			st.ExtSlot3PracticalCount++
			reason = i18n.T("essencefilter.reason.slot3_practical",
				matchResult.SkillsChinese[2], matchResult.ExtSlot3Lv, matchResult.ExtMinLevel)
		}

		if matchResult.ShouldLock {
			st.MatchedCount++
			// 与精准匹配相同，均用 skillCombinationKey（未来可期时 SkillIDs 为各槽池解析出的 ID，未识别槽为 0）。
			key := skillCombinationKey(matchResult.SkillIDs)
			if key != "" {
				if s, ok := st.MatchedCombinationSummary[key]; ok {
					s.Count++
				} else {
					weapons := append([]matchapi.WeaponData(nil), matchResult.Weapons...)
					if len(weapons) == 0 {
						// 无关联武器名时，用一条占位武器承载扩展规则说明（与 reportExtRule 同文案），沿用既有战利品摘要渲染
						weapons = []matchapi.WeaponData{{ChineseName: reason, Rarity: 3}}
					}
					st.MatchedCombinationSummary[key] = &matchapi.SkillCombinationSummary{
						SkillIDs:      append([]int(nil), matchResult.SkillIDs...),
						SkillsChinese: append([]string(nil), matchResult.SkillsChinese...),
						OCRSkills:     append([]string(nil), skills...),
						Weapons:       weapons,
						Count:         1,
					}
				}
			}
			reportExtRule(ctx, reason, true)
		} else {
			reportExtRule(ctx, reason, false)
		}

	case matchapi.MatchNone:
		reportNoMatch(ctx, matchResult.ShouldDiscard)
	}

	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
	switch {
	case matchResult.ShouldLock:
		return lockedMark, true
	case matchResult.ShouldDiscard:
		return discarded, true
	default:
		return unmarked, true
	}
}

func loadMatchEngine(ctx *maa.Context, ctrl *maa.Controller, opts *EssenceFilterOptions) (*matchapi.Engine, error) {
	locale := resolveInputLanguage(ctx, ctrl, opts.InputLanguage)
	opts.InputLanguage = locale
	engine, err := matchapi.NewEngineFromDirWithLocale(essenceFilterDataDir, locale)
	if err != nil {
		return nil, fmt.Errorf("load match engine: %w", err)
	}
	return engine, nil
}

func reportLockLimit(ctx *maa.Context, verdict lockLimitDecision, match *matchapi.MatchResult, locked bool) {
	levels := match.Levels
	switch {
	case locked:
		if verdict.NeedsUnlock {
			reportColoredByKey(ctx, "#ff4d4f", "lock_limit.unlock_needed",
				strings.Join(match.SkillsChinese, i18n.Separator()), levels[0], levels[1], levels[2])
		} else {
			reportSimpleByKey(ctx, "lock_limit.already_locked")
		}
	case !verdict.Lock:
		reportSimpleByKey(ctx, "lock_limit.not_in_quota", verdict.Quota)
	default:
		reportSimpleByKey(ctx, "lock_limit.in_quota", verdict.Quota)
	}
}

// reportFinishLockLimit lists the combinations holding locked suboptimal essences.
func reportFinishLockLimit(ctx *maa.Context, st *RunState) {
	if st.LockLimit == nil {
		return
	}
	summaries := st.LockLimit.overLimitSummaries()
	if len(summaries) == 0 {
		reportSimpleByKey(ctx, "lock_limit.summary_none")
		return
	}
	reportColoredByKey(ctx, "#ff9f43", "lock_limit.summary_title")
	for _, summary := range summaries {
		reportSimpleByKey(ctx, "lock_limit.summary_row",
			strings.Join(summary.Skills[:], i18n.Separator()),
			strings.Join(summary.Weapons, i18n.Separator()),
			summary.Quota,
			summary.Locked,
			summary.Excess,
		)
		for _, row := range summary.Rows {
			reportSimpleByKey(ctx, "lock_limit.summary_level",
				row.Levels[0], row.Levels[1], row.Levels[2], row.Count)
		}
	}
}
