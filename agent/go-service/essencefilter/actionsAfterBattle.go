package essencefilter

import (
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type EssenceFilterAfterBattleSkillDecisionAction struct{}

func (a *EssenceFilterAfterBattleSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := getRunState()
	if st == nil {
		return false
	}
	skills := []string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]}
	opts, _ := getOptionsFromAttach(ctx, "EssenceFilterInit")
	if opts == nil {
		opts = &EssenceFilterOptions{}
	}
	matchResult, matched := MatchEssenceSkills(ctx, skills, st.TargetSkillCombinations)
	extendedReason := ""
	if !matched && opts != nil {
		if opts.KeepFuturePromising && opts.FuturePromisingMinTotal > 0 {
			if MatchFuturePromising(skills, st.CurrentSkillLevels, opts.FuturePromisingMinTotal) {
				matched = true
				sum := st.CurrentSkillLevels[0] + st.CurrentSkillLevels[1] + st.CurrentSkillLevels[2]
				matchResult = &SkillCombinationMatch{
					SkillIDs:      []int{0, 0, 0},
					SkillsChinese: []string{skills[0], skills[1], skills[2]},
					Weapons:       []WeaponData{},
				}
				extendedReason = fmt.Sprintf("未来可期：总等级 %d ≥ %d", sum, opts.FuturePromisingMinTotal)
				st.ExtFuturePromisingCount++
				log.Info().Str("component", "EssenceFilter").Str("rule", "MatchFuturePromising").Strs("skills", skills).Ints("levels", st.CurrentSkillLevels[:]).Int("sum", sum).Int("min_total", opts.FuturePromisingMinTotal).Msg("keep future promising essence")
			}
		}
		slot3MinLv := opts.Slot3MinLevel
		if slot3MinLv <= 0 {
			slot3MinLv = 3
		}
		if !matched && opts.KeepSlot3Level3Practical {
			var slot3Lv int
			var slot3Match bool
			matchResult, slot3Lv, slot3Match = MatchSlot3Level3Practical(skills, st.CurrentSkillLevels, slot3MinLv)
			if slot3Match {
				matched = true
				extendedReason = fmt.Sprintf("实用基质：词条3(%s)等级 %d ≥ %d", matchResult.SkillsChinese[2], slot3Lv, slot3MinLv)
				st.ExtSlot3PracticalCount++
				log.Info().Str("component", "EssenceFilter").Str("rule", "MatchSlot3Level3Practical").Str("slot3_skill", matchResult.SkillsChinese[2]).Int("slot3_level", slot3Lv).Int("min_level", slot3MinLv).Msg("keep practical essence")
			}
		}
	}
	MatchedMessageColor := "#00bfff"
	if matched {
		MatchedMessageColor = "#064d7c"
	}
	LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("OCR到技能：%s(+%d) | %s(+%d) | %s(+%d)", skills[0], st.CurrentSkillLevels[0], skills[1], st.CurrentSkillLevels[1], skills[2], st.CurrentSkillLevels[2]), MatchedMessageColor)

	if matched && extendedReason != "" {
		st.MatchedCount++
		log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Str("reason", extendedReason).Int("matched_count", st.MatchedCount).Msg("extended rule hit, lock next")
		LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 扩展规则命中：%s</div>`, escapeHTML(extendedReason)))
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleLockItemLog"}})
	} else if matched {
		st.MatchedCount++
		weaponNames := make([]string, 0, len(matchResult.Weapons))
		for _, w := range matchResult.Weapons {
			weaponNames = append(weaponNames, w.ChineseName)
		}
		log.Info().Str("component", "EssenceFilter").Strs("weapons", weaponNames).Strs("skills", skills).Ints("skill_ids", matchResult.SkillIDs).Int("matched_count", st.MatchedCount).Msg("match ok, lock next")
		var weaponsHTML strings.Builder
		for i, w := range matchResult.Weapons {
			if i > 0 {
				weaponsHTML.WriteString("、")
			}
			weaponsHTML.WriteString(fmt.Sprintf(`<span style="color: %s;">%s</span>`, getColorForRarity(w.Rarity), escapeHTML(w.ChineseName)))
		}
		LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">匹配到武器：%s</div>`, weaponsHTML.String()))
		key := skillCombinationKey(matchResult.SkillIDs)
		if key != "" {
			if s, ok := st.MatchedCombinationSummary[key]; ok {
				s.Count++
			} else {
				st.MatchedCombinationSummary[key] = &SkillCombinationSummary{
					SkillIDs:      append([]int(nil), matchResult.SkillIDs...),
					SkillsChinese: append([]string(nil), matchResult.SkillsChinese...),
					OCRSkills:     append([]string(nil), skills...),
					Weapons:       append([]WeaponData(nil), matchResult.Weapons...),
					Count:         1,
				}
			}
		}
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleLockItemLog"}})
	} else {
		if opts.DiscardUnmatched {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Msg("not matched, discard item")
			LogMXUHTML(ctx, `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 未匹配到目标技能组合，废弃该物品</div>`)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleDiscardItemLog"}})
		} else {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Msg("not matched, skip to next item")
			LogMXUSimpleHTML(ctx, "未匹配到目标技能组合，跳过该物品")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleRowNextItem"}})
		}
	}
	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
	return true
}
