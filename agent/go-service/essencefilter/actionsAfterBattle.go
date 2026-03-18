package essencefilter

import (
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type EssenceFilterAfterBattleSkillDecisionAction struct{}

func (a *EssenceFilterAfterBattleSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	// 获取当前运行状态，如果状态为空则无法继续，直接返回
	st := getRunState()
	if st == nil {
		return false
	}

	// 将识别到的三个技能名称存入切片，方便后续比对
	skills := []string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]}

	// 从 Context 中获取用户配置的选项（如是否开启未来可期等），若获取不到则使用默认配置
	opts, _ := getOptionsFromAttach(ctx, "EssenceFilterAfterBattleInit")
	if opts == nil {
		opts = &EssenceFilterOptions{}
	}

	// 调用核心匹配函数，检查当前技能是否符合用户预设的目标组合
	matchResult, matched := MatchEssenceSkills(ctx, skills, st.TargetSkillCombinations)
	extendedReason := ""

	// 如果基础组合没匹配上，则进入扩展规则检查（如“未来可期”或“实用基质”检查）
	if !matched && opts != nil {
		// 检查“未来可期”规则：如果总等级达到阈值，即使技能组合不对也保留
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

		// 设置词条3的最低等级门槛，默认是3级
		slot3MinLv := opts.Slot3MinLevel
		if slot3MinLv <= 0 {
			slot3MinLv = 3
		}

		// 检查“实用基质”规则：如果第3个槽位的技能等级够高且符合实用标准，则保留
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

	// 根据是否匹配成功，给控制台日志信息设置不同的颜色（匹配成功用深蓝，普通识别用亮蓝）
	MatchedMessageColor := "#00bfff"
	if matched {
		MatchedMessageColor = "#064d7c"
	}
	// 在 MAA UI 界面上显示当前 OCR 识别到的技能和等级信息
	LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("OCR到技能：%s(+%d) | %s(+%d) | %s(+%d)", skills[0], st.CurrentSkillLevels[0], skills[1], st.CurrentSkillLevels[1], skills[2], st.CurrentSkillLevels[2]), MatchedMessageColor)

	// 情况 A：由扩展规则（未来可期/实用基质）触发的匹配成功
	if matched && extendedReason != "" {
		st.MatchedCount++
		log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Str("reason", extendedReason).Int("matched_count", st.MatchedCount).Msg("extended rule hit, lock next")
		LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 扩展规则命中：%s</div>`, escapeHTML(extendedReason)))
		// 强制修改 Pipeline 的下一步任务，跳转到“执行锁定”逻辑
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleLockItemLog"}})

		// 情况 B：由预设的技能/武器组合触发的匹配成功
	} else if matched {
		st.MatchedCount++
		weaponNames := make([]string, 0, len(matchResult.Weapons))
		for _, w := range matchResult.Weapons {
			weaponNames = append(weaponNames, w.ChineseName)
		}
		log.Info().Str("component", "EssenceFilter").Strs("weapons", weaponNames).Strs("skills", skills).Ints("skill_ids", matchResult.SkillIDs).Int("matched_count", st.MatchedCount).Msg("match ok, lock next")

		// 构建带有稀有度颜色的 HTML 文本，展示匹配到的武器名
		var weaponsHTML strings.Builder
		for i, w := range matchResult.Weapons {
			if i > 0 {
				weaponsHTML.WriteString("、")
			}
			weaponsHTML.WriteString(fmt.Sprintf(`<span style="color: %s;">%s</span>`, getColorForRarity(w.Rarity), escapeHTML(w.ChineseName)))
		}
		LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">匹配到武器：%s</div>`, weaponsHTML.String()))

		// 统计命中次数，将该组合记录到统计摘要中
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
		// 强制修改 Pipeline 的下一步任务，跳转到“执行锁定”逻辑
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleLockItemLog"}})

		// 情况 C：没有任何规则命中
	} else {
		// 如果配置要求废弃不匹配的物品，则跳转到“执行废弃（回收）”逻辑
		if opts.DiscardUnmatched {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Msg("not matched, discard item")
			LogMXUHTML(ctx, `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 未匹配到目标技能组合，废弃该物品</div>`)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleDiscardItemLog"}})
			// 否则只是简单跳过，继续看下一个物品
		} else {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Msg("not matched, skip to next item")
			LogMXUSimpleHTML(ctx, "未匹配到目标技能组合，跳过该物品")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterAfterBattleCloseDetail"}})
		}
	}

	// 清空当前识别缓存，准备处理下一个掉落物
	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
	return true
}
