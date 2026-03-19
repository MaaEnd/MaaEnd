package essencefilter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var levelParseRe = regexp.MustCompile(`\+?(\d+)`)

// essenceMaxSinglePageInventory is the max items visible on one screen row grid (and tail-scan threshold when total is known).
const essenceMaxSinglePageInventory = 45

// --- Init ---

// EssenceFilterInitAction - initialize filter
type EssenceFilterInitAction struct{}

func (a *EssenceFilterInitAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Msg("init start")
	base := getResourceBase()
	if base == "" {
		base = "data"
	}
	gameDataDir := filepath.Join(base, "EssenceFilter")
	engine, err := matchapi.NewEngineFromDir(gameDataDir)
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadMatchEngine").Msg("load match data failed")
		return false
	}
	LogMXUSimpleHTML(ctx, "武器数据加载完成")
	logSkillPools(engine)

	opts, err := getOptionsFromAttach(ctx, arg.CurrentTaskName)
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadOptions").Msg("load options failed")
		return false
	}
	var weaponRarity []int
	if opts.Rarity6Weapon {
		weaponRarity = append(weaponRarity, 6)
	}
	if opts.Rarity5Weapon {
		weaponRarity = append(weaponRarity, 5)
	}
	if opts.Rarity4Weapon {
		weaponRarity = append(weaponRarity, 4)
	}
	var essenceTypes []EssenceMeta
	if opts.FlawlessEssence {
		essenceTypes = append(essenceTypes, FlawlessEssenceMeta)
	}
	if opts.PureEssence {
		essenceTypes = append(essenceTypes, PureEssenceMeta)
	}
	if len(essenceTypes) == 0 {
		log.Error().Str("component", "EssenceFilter").Str("step", "ValidatePresets").Msg("no essence type selected")
		LogMXUSimpleHTMLWithColor(ctx, "未选择任何基质类型，请至少选择一个基质类型作为筛选条件", "#ff0000")
		return false
	}

	if len(weaponRarity) == 0 {
		LogMXUSimpleHTML(ctx, "未选择武器稀有度，仅使用扩展规则")
	} else {
		LogMXUSimpleHTML(ctx, fmt.Sprintf("已选择稀有度：%s", rarityListToString(weaponRarity)))
	}
	LogMXUSimpleHTML(ctx, fmt.Sprintf("已选择基质类型：%s", essenceListToString(essenceTypes)))

	st := &RunState{MaxItemsPerRow: 9, EssenceTypes: essenceTypes}
	st.Reset()
	st.PipelineOpts = *opts
	st.MatchEngine = engine

	matchOpts := matchOptsFromPipeline(opts)
	st.TargetSkillCombinations = engine.BuildTargets(matchOpts)
	st.MatchedCombinationSummary = make(map[string]*matchapi.SkillCombinationSummary)
	st.EssenceTypes = essenceTypes
	setRunState(st)

	// Build filtered skill stats for logging/UI.
	for i := range st.FilteredSkillStats {
		st.FilteredSkillStats[i] = make(map[int]int)
	}
	for _, combo := range st.TargetSkillCombinations {
		for slotIdx, id := range combo.SkillIDs {
			if slotIdx >= 0 && slotIdx < 3 {
				st.FilteredSkillStats[slotIdx][id]++
			}
		}
	}

	filteredWeapons := make([]matchapi.WeaponData, 0, len(st.TargetSkillCombinations))
	names := make([]string, 0, len(st.TargetSkillCombinations))
	for _, combo := range st.TargetSkillCombinations {
		filteredWeapons = append(filteredWeapons, combo.Weapon)
		names = append(names, combo.Weapon.ChineseName)
	}
	log.Info().Str("component", "EssenceFilter").Str("step", "FilterWeapons").Int("filtered_count", len(filteredWeapons)).Strs("weapons", names).Msg("weapons filtered")

	if len(filteredWeapons) == 0 {
		LogMXUSimpleHTML(ctx, "符合条件的武器数量：0（仅扩展规则）")
	} else {
		LogMXUSimpleHTML(ctx, fmt.Sprintf("符合条件的武器数量：%d", len(filteredWeapons)))
	}
	sort.Slice(filteredWeapons, func(i, j int) bool { return filteredWeapons[i].Rarity > filteredWeapons[j].Rarity })
	if len(filteredWeapons) > 0 {
		var builder strings.Builder
		const columns = 3
		builder.WriteString(`<table style="width: 100%; border-collapse: collapse;">`)
		for i, w := range filteredWeapons {
			if i%columns == 0 {
				builder.WriteString("<tr>")
			}
			builder.WriteString(fmt.Sprintf(`<td style="padding: 2px 8px; color: %s; font-size: 11px;">%s</td>`, getColorForRarity(w.Rarity), w.ChineseName))
			if i%columns == columns-1 || i == len(filteredWeapons)-1 {
				builder.WriteString("</tr>")
			}
		}
		builder.WriteString("</table>")
		LogMXUHTML(ctx, builder.String())
	} else {
		LogMXUSimpleHTML(ctx, "未选择武器，无目标武器列表")
	}

	log.Info().Str("component", "EssenceFilter").Str("step", "BuildSkillCombinations").Int("combinations", len(st.TargetSkillCombinations)).Msg("skill combinations built")
	log.Info().Str("component", "EssenceFilter").Msg("init done")

	if len(st.TargetSkillCombinations) > 0 {
		const columns = 3
		uniqueNameSlots := [3]map[int]string{}
		for i := 0; i < 3; i++ {
			uniqueNameSlots[i] = make(map[int]string)
		}
		for _, c := range st.TargetSkillCombinations {
			for i, skillID := range c.SkillIDs {
				if i >= 0 && i < 3 && i < len(c.SkillsChinese) {
					uniqueNameSlots[i][skillID] = c.SkillsChinese[i]
				}
			}
		}
		var skillBuilder strings.Builder
		skillBuilder.WriteString(`<div style="color: #00bfff; font-weight: 900;">目标技能列表：</div>`)
		slotColors := []string{"#47b5ff", "#11dd11", "#e877fe"}
		for i := 0; i < 3; i++ {
			idToName := uniqueNameSlots[i]
			skillNames := make([]string, 0, len(idToName))
			for _, name := range idToName {
				if name != "" {
					skillNames = append(skillNames, name)
				}
			}
			sort.Strings(skillNames)
			if len(skillNames) == 0 {
				continue
			}
			slotColor := slotColors[i]
			skillBuilder.WriteString(fmt.Sprintf(`<div style="color: %s; font-weight: 700;">词条 %d:</div>`, slotColor, i+1))
			skillBuilder.WriteString(fmt.Sprintf(`<table style="width: 100%%; color: %s; border-collapse: collapse;">`, slotColor))
			for j, name := range skillNames {
				if j%columns == 0 {
					skillBuilder.WriteString("<tr>")
				}
				skillBuilder.WriteString(fmt.Sprintf(`<td style="padding: 2px 8px; font-size: 12px;">%s</td>`, name))
				if j%columns == columns-1 || j == len(skillNames)-1 {
					skillBuilder.WriteString("</tr>")
				}
			}
			skillBuilder.WriteString("</table>")
		}
		LogMXUHTML(ctx, skillBuilder.String())
	} else {
		LogMXUSimpleHTML(ctx, "未选择武器，无目标技能列表")
	}
	return true
}

// --- OCR 库存数量 / Trace（同一 case：轻量辅助 action）---

// OCREssenceInventoryNumberAction - OCR inventory count and override next if single page
type OCREssenceInventoryNumberAction struct{}

func (a *OCREssenceInventoryNumberAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	text, ok := firstOCRText(arg.RecognitionDetail)
	if !ok {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckTotal").Msg("OCR text empty")
		return false
	}
	re := regexp.MustCompile(`\d+`)
	nums := re.FindAllString(text, -1)
	if len(nums) == 0 {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckTotal").Str("text", text).Msg("no number found")
		return false
	}
	n, err := strconv.Atoi(nums[0])
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("action", "CheckTotal").Str("text", text).Msg("parse failed")
		return false
	}
	log.Info().Str("component", "EssenceFilter").Str("action", "CheckTotal").Int("count", n).Int("max_single_page", essenceMaxSinglePageInventory).Str("raw", text).Msg("total parsed")
	msg := fmt.Sprintf("库存中共 <span style=\"color: #ff7000; font-weight: 900;\">%d</span> 个基质", n)
	if st := getRunState(); st != nil && st.MatchEngine != nil {
		if v := st.MatchEngine.DataVersion(); v != "" {
			msg += fmt.Sprintf(" <span style=\"color: #ff0000; font-weight: 900;\">当前数据日期：%s</span>(如果更新了请注意)", v)
		}
	}
	LogMXUSimpleHTML(ctx, msg)
	if st := getRunState(); st != nil {
		st.TotalCount = n
	}
	if n <= essenceMaxSinglePageInventory {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceDetectFinal"}})
	}
	return true
}

// EssenceFilterTraceAction - log node/step
type EssenceFilterTraceAction struct{}

func (a *EssenceFilterTraceAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Step string `json:"step"`
	}
	_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	if params.Step == "" {
		params.Step = arg.CurrentTaskName
	}
	log.Info().Str("component", "EssenceFilter").Str("step", params.Step).Str("node", arg.CurrentTaskName).Msg("trace")
	return true
}

// --- CheckItem / CheckItemLevel / SkillDecision（同一 case：单格技能识别与决策）---

// EssenceFilterCheckItemAction - OCR skills and match
type EssenceFilterCheckItemAction struct{}

func (a *EssenceFilterCheckItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Str("action", "CheckItem").Msg("start")
	st := getRunState()
	if st == nil {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckItem").Msg("no run state")
		return false
	}
	if !st.StatsLogged {
		logFilteredSkillStats()
		st.StatsLogged = true
	}
	var params struct {
		Slot   int  `json:"slot"`
		IsLast bool `json:"is_last"`
	}
	if arg.CustomActionParam != "" {
		_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	}
	if params.Slot < 1 || params.Slot > 3 {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("invalid slot param")
		return false
	}
	if params.Slot == 1 {
		st.CurrentSkills = [3]string{}
		st.CurrentSkillLevels = [3]int{}
	}
	rawText, ok := firstOCRText(arg.RecognitionDetail)
	if !ok {
		log.Error().Str("component", "EssenceFilter").Msg("OCR detail missing from pipeline")
		return false
	}
	text := cleanChinese(rawText)
	if text == "" {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("raw", rawText).Msg("OCR empty")
		return false
	}
	st.CurrentSkills[params.Slot-1] = text
	log.Info().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("skill", rawText).Bool("is_last", params.IsLast).Msg("OCR ok")
	if !params.IsLast {
		return true
	}
	for i, s := range st.CurrentSkills {
		if s == "" {
			log.Error().Str("component", "EssenceFilter").Int("slot", i+1).Msg("missing skill for slot")
			return false
		}
	}
	return true
}

// EssenceFilterCheckItemLevelAction - 识别技能等级（独立 level ROI）
type EssenceFilterCheckItemLevelAction struct{}

func (a *EssenceFilterCheckItemLevelAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		Slot int `json:"slot"`
	}
	if arg.CustomActionParam != "" {
		_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	}
	if params.Slot < 1 || params.Slot > 3 {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("invalid level slot param")
		return false
	}
	rawText, ok := firstOCRText(arg.RecognitionDetail)
	if !ok {
		log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Msg("level OCR detail missing or empty")
		return false
	}
	st := getRunState()
	if st == nil {
		return false
	}
	if m := levelParseRe.FindStringSubmatch(rawText); len(m) >= 2 {
		if lv, err := strconv.Atoi(m[1]); err == nil && lv >= 1 && lv <= 6 {
			st.CurrentSkillLevels[params.Slot-1] = lv
			log.Info().Str("component", "EssenceFilter").Int("slot", params.Slot).Int("level", lv).Str("raw", rawText).Msg("OCR level ok")
			return true
		}
	}
	log.Error().Str("component", "EssenceFilter").Int("slot", params.Slot).Str("raw", rawText).Msg("level parse fail")
	return false
}

// EssenceFilterSkillDecisionAction - match skills then decide lock or skip
type EssenceFilterSkillDecisionAction struct{}

func (a *EssenceFilterSkillDecisionAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := getRunState()
	if st == nil {
		return false
	}
	ocr := matchapi.OCRInput{
		Skills: [3]string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]},
		Levels: [3]int{st.CurrentSkillLevels[0], st.CurrentSkillLevels[1], st.CurrentSkillLevels[2]},
	}
	skills := []string{ocr.Skills[0], ocr.Skills[1], ocr.Skills[2]}

	if st.MatchEngine == nil {
		return false
	}
	matchOpts := matchOptsFromPipeline(&st.PipelineOpts)

	matchResult, err := st.MatchEngine.MatchOCR(ocr, matchOpts)
	if err != nil || matchResult == nil {
		return false
	}

	extendedReason := matchResult.Reason
	MatchedMessageColor := "#00bfff"
	if matchResult.Kind != matchapi.MatchNone {
		MatchedMessageColor = "#064d7c"
	}
	LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("OCR到技能：%s(+%d) | %s(+%d) | %s(+%d)", skills[0], st.CurrentSkillLevels[0], skills[1], st.CurrentSkillLevels[1], skills[2], st.CurrentSkillLevels[2]), MatchedMessageColor)

	switch matchResult.Kind {
	case matchapi.MatchExact:
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
				st.MatchedCombinationSummary[key] = &matchapi.SkillCombinationSummary{
					SkillIDs:      append([]int(nil), matchResult.SkillIDs...),
					SkillsChinese: append([]string(nil), matchResult.SkillsChinese...),
					OCRSkills:     append([]string(nil), skills...),
					Weapons:       append([]matchapi.WeaponData(nil), matchResult.Weapons...),
					Count:         1,
				}
			}
		}
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterLockItemLog"}})

	case matchapi.MatchFuturePromising, matchapi.MatchSlot3Level3Practical:
		if matchResult.Kind == matchapi.MatchFuturePromising {
			st.ExtFuturePromisingCount++
			log.Info().Str("component", "EssenceFilter").Str("rule", "MatchFuturePromising").Strs("skills", skills).Ints("levels", st.CurrentSkillLevels[:]).Msg("keep future promising essence")
		} else {
			st.ExtSlot3PracticalCount++
			log.Info().Str("component", "EssenceFilter").Str("rule", "MatchSlot3Level3Practical").Str("slot3_skill", matchResult.SkillsChinese[2]).Ints("levels", st.CurrentSkillLevels[:]).Msg("keep practical essence")
		}

		if matchResult.ShouldLock {
			st.MatchedCount++
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Str("reason", extendedReason).Int("matched_count", st.MatchedCount).Msg("extended rule hit, lock next")
			LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #064d7c; font-weight: 900;">🔒 扩展规则命中并锁定：%s</div>`, escapeHTML(extendedReason)))
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterLockItemLog"}})
		} else {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Str("reason", extendedReason).Msg("extended rule hit, no operation")
			LogMXUHTML(ctx, fmt.Sprintf(`<div style="color: #d18b00; font-weight: 900;">🗂️ 扩展规则命中（不操作）：%s</div>`, escapeHTML(extendedReason)))
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterRowNextItem"}})
		}

	case matchapi.MatchNone:
		if matchResult.ShouldDiscard {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Msg("not matched, discard item")
			LogMXUHTML(ctx, `<div style="color: #ff6b6b; font-weight: 900;">🗑️ 未匹配到目标技能组合，废弃该物品</div>`)
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterDiscardItemLog"}})
		} else {
			log.Info().Str("component", "EssenceFilter").Strs("skills", skills).Msg("not matched, skip to next item")
			LogMXUSimpleHTML(ctx, "未匹配到目标技能组合，跳过该物品")
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterRowNextItem"}})
		}
	}
	st.CurrentSkills = [3]string{}
	st.CurrentSkillLevels = [3]int{}
	return true
}

// --- RowCollect / RowNextItem / Finish / SwipeCalibrate（同一 case：行遍历与网格）---

// EssenceFilterRowCollectAction - collect boxes in a row (TemplateMatch + ColorMatch), then RowNextItem
type EssenceFilterRowCollectAction struct{}

func (a *EssenceFilterRowCollectAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil || !arg.RecognitionDetail.Hit {
		log.Error().Str("component", "EssenceFilter").Str("action", "RowCollect").Msg("recognition detail empty")
		return false
	}
	st := getRunState()
	if st == nil {
		return false
	}
	results := arg.RecognitionDetail.Results.Filtered
	if len(results) == 0 {
		results = arg.RecognitionDetail.Results.All
	}
	controller := ctx.GetTasker().GetController()
	if controller == nil {
		log.Error().Str("component", "EssenceFilter").Str("action", "RowCollect").Msg("controller nil")
		return false
	}
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("action", "RowCollect").Msg("get screenshot failed")
		return false
	}
	st.RowBoxes = st.RowBoxes[:0]
	st.PhysicalItemCount = len(results)

	skipMarked := st.PipelineOpts.SkipLockedRow

	for _, res := range results {
		tm, ok := res.AsTemplateMatch()
		if !ok {
			continue
		}
		b := tm.Box
		boxArr := [4]int{b.X(), b.Y(), b.Width(), b.Height()}
		colorMatchROIW := boxArr[2]
		colorMatchROIH := boxArr[3] - 90
		if colorMatchROIW <= 0 || colorMatchROIH <= 0 {
			continue
		}
		roi := maa.Rect{boxArr[0], boxArr[1] + 90, colorMatchROIW, colorMatchROIH}

		colorMatched := false
		for _, et := range st.EssenceTypes {
			cDetail, err := ctx.RunRecognition("EssenceColorMatch", img, map[string]any{
				// 直接传递 roi 切片
				"EssenceColorMatch": map[string]any{"roi": roi, "lower": et.Range.Lower, "upper": et.Range.Upper},
			})
			if err != nil {
				continue
			}
			if cDetail != nil && cDetail.Hit {
				colorMatched = true
				break
			}
		}

		if colorMatched {
			isMarked := false
			if skipMarked {
				margin := 10
				bx1, by1 := boxArr[0]-margin, boxArr[1]-margin
				if bx1 < 0 {
					bx1 = 0
				}
				if by1 < 0 {
					by1 = 0
				}
				bw, bh := boxArr[2]+margin*2, boxArr[3]+margin*2

				roiX := bx1
				roiY := by1 + int(float64(bh)*0.65)
				roiW := int(float64(bw) * 0.30)
				roiH := int(float64(bh) * 0.35)

				thumbDetail, err := ctx.RunRecognition("EssenceThumbMarked", img, map[string]any{
					"EssenceThumbMarked": map[string]any{
						"roi": []int{roiX, roiY, roiW, roiH},
					},
				})
				if err == nil && thumbDetail != nil && thumbDetail.Hit {
					isMarked = true
				}
			}

			if !isMarked {
				st.RowBoxes = append(st.RowBoxes, boxArr)
			}
		}
	}

	sort.Slice(st.RowBoxes, func(i, j int) bool {
		if st.RowBoxes[i][1] == st.RowBoxes[j][1] {
			return st.RowBoxes[i][0] < st.RowBoxes[j][0]
		}
		return st.RowBoxes[i][1] < st.RowBoxes[j][1]
	})

	log.Info().Str("component", "EssenceFilter").Str("action", "RowCollect").Int("len_results", len(results)).Int("valid_boxes", len(st.RowBoxes)).Msg("color match done")

	if skipMarked && len(st.RowBoxes) == 0 && st.PhysicalItemCount == st.MaxItemsPerRow {
		LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("第 %d 行已全部标记，跳过", st.CurrentRow), "#11cf00")
	}

	isFallbackScan := arg.CurrentTaskName == "EssenceDetectFinal"
	st.InFinalScan = isFallbackScan
	if isFallbackScan && !st.FinalLargeScanUsed {
		st.FinalLargeScanUsed = true
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceDetectFinal"}})
		LogMXUSimpleHTMLWithColor(ctx, "尾扫完成，收集所有剩余基质格子", "#1a01fd")
		return true
	}
	if (st.PhysicalItemCount > st.MaxItemsPerRow) && !isFallbackScan {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterFinish"}})
		return true
	}
	if st.PhysicalItemCount == 0 {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterFinish"}})
		return true
	}
	st.RowIndex = 0
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterRowNextItem"}})
	return true
}

// EssenceFilterRowNextItemAction - proceed to next box or swipe/finish
type EssenceFilterRowNextItemAction struct{}

func (a *EssenceFilterRowNextItemAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := getRunState()
	if st == nil {
		return false
	}
	if st.PendingFinalScan {
		st.PendingFinalScan = false
		st.InFinalScan = true
		log.Info().Str("component", "EssenceFilter").Str("action", "RowNextItem").Msg("补 swipe 完成，进入尾扫")
		LogMXUSimpleHTML(ctx, "补 swipe 完成，进入尾扫")
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceDetectFinal"}})
		return true
	}
	if st.RowIndex >= len(st.RowBoxes) {
		if (st.PhysicalItemCount == st.MaxItemsPerRow) && !st.FinalLargeScanUsed {
			rowsDone := st.CurrentRow
			remaining := st.TotalCount - st.MaxItemsPerRow*rowsDone
			if st.TotalCount > 0 && remaining <= essenceMaxSinglePageInventory {
				st.PendingFinalScan = true
				LogMXUSimpleHTML(ctx, fmt.Sprintf("剩余 %d 个 ≤ %d，先补一次滑动再尾扫（总 %d，已 %d 行）", remaining, essenceMaxSinglePageInventory, st.TotalCount, rowsDone))
			}
			nextNode := "EssenceFilterSwipeNext"
			if !st.FirstRowSwipeDone {
				st.FirstRowSwipeDone = true
				nextNode = "EssenceFilterSwipeFirst"
			}
			// 最后一次补滑（remaining <= 45）不走校准：避免 SwipeCalibrate 识别失败导致流程中断
			if st.PendingFinalScan {
				if nextNode == "EssenceFilterSwipeFirst" {
					nextNode = "EssenceFilterSwipeFirstNoCalibrate"
				} else {
					nextNode = "EssenceFilterSwipeNextNoCalibrate"
				}
			}
			ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: nextNode}})
			LogMXUSimpleHTML(ctx, fmt.Sprintf("滑动到第 %d 行", st.CurrentRow+1))
			st.CurrentRow++
			return true
		}
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterFinish"}})
		return true
	}

	box := st.RowBoxes[st.RowIndex]
	log.Info().Str("component", "EssenceFilter").Str("action", "RowNextItem").Ints("box", box[:]).Msg("click next box")
	clickingBox := [4]int{box[0] + 10, box[1] + 10, box[2] - 20, box[3] - 20}
	ctx.RunTask("NodeClick", map[string]any{
		"NodeClick": map[string]any{
			"action": map[string]any{"param": map[string]any{"target": clickingBox}},
		},
	})
	st.VisitedCount++
	st.RowIndex++
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterCheckItemSlot1"}})
	return true
}

// EssenceFilterFinishAction - finish and reset
type EssenceFilterFinishAction struct{}

func (a *EssenceFilterFinishAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	log.Info().Str("component", "EssenceFilter").Msg("finish")
	st := getRunState()
	if st != nil {
		log.Info().Str("component", "EssenceFilter").Int("matched_total", st.MatchedCount).Msg("locked items")
		LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("筛选完成！共历遍物品：%d，确认锁定物品：%d", st.VisitedCount, st.MatchedCount), "#11cf00")
		logMatchSummary(ctx)
		po := &st.PipelineOpts
		if po.KeepFuturePromising {
			LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("扩展规则「未来可期」命中：%d 个", st.ExtFuturePromisingCount), "#064d7c")
		}
		if po.KeepSlot3Level3Practical {
			LogMXUSimpleHTMLWithColor(ctx, fmt.Sprintf("扩展规则「实用基质」命中：%d 个", st.ExtSlot3PracticalCount), "#064d7c")
		}
		if po.ExportCalculatorScript {
			logCalculatorResult(ctx)
		}
	}
	setRunState(nil)
	return true
}

const firstRowTargetY = 86       //首行Y
const calibrateTolerance = 8     //校准误差
const calibrateScrollRatio = 1.1 //校准滑动比例
const calibrateSwipeMin = 8      //校准滑动最小值
const calibrateSwipeMax = 40     //校准滑动最大值

// EssenceFilterSwipeCalibrateAction - 根据首个 box 的 Y 校准到基准 firstRowTargetY
type EssenceFilterSwipeCalibrateAction struct{}

func (a *EssenceFilterSwipeCalibrateAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	st := getRunState()
	if st == nil {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	if st.SwipeCalibrateRetry >= 5 {
		st.SwipeCalibrateRetry = 0
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	if arg.RecognitionDetail == nil || arg.RecognitionDetail.Results == nil || !arg.RecognitionDetail.Hit {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	results := arg.RecognitionDetail.Results.Filtered
	if len(results) == 0 {
		results = arg.RecognitionDetail.Results.All
	}
	if len(results) == 0 {
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	boxes := make([][4]int, 0, len(results))
	for _, res := range results {
		tm, ok := res.AsTemplateMatch()
		if !ok {
			continue
		}
		b := tm.Box
		boxes = append(boxes, [4]int{b.X(), b.Y(), b.Width(), b.Height()})
	}
	sort.Slice(boxes, func(i, j int) bool { return boxes[i][0] < boxes[j][0] })
	firstBoxY := boxes[0][1]
	if firstBoxY >= firstRowTargetY-calibrateTolerance && firstBoxY <= firstRowTargetY+calibrateTolerance {
		st.SwipeCalibrateRetry = 0
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceRowDetect"}, {Name: "EssenceDetectFinal"}})
		return true
	}
	delta := firstBoxY - firstRowTargetY
	swipeDist := int(float64(abs(delta)) * calibrateScrollRatio)
	if swipeDist < calibrateSwipeMin {
		swipeDist = calibrateSwipeMin
	}
	if swipeDist > calibrateSwipeMax {
		swipeDist = calibrateSwipeMax
	}
	centerX, beginY := 135, 191
	var endY int
	if delta > 0 {
		endY = beginY - swipeDist
	} else {
		endY = beginY + swipeDist
	}
	ctx.RunTask("EssenceFilterSwipeCalibrateCorrect", map[string]any{
		"EssenceFilterSwipeCalibrateCorrect": map[string]any{
			"action": map[string]any{"param": map[string]any{"begin": []int{centerX, beginY}, "end": []int{centerX, endY}}},
		},
	})
	st.SwipeCalibrateRetry++
	ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceFilterSwipeCalibrate"}})
	return true
}
