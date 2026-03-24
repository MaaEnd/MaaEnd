package essencefilter

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/essencefilter/matchapi"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
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
	engine, opts, err := EnsureMatchEngine(ctx, nil, arg.CurrentTaskName)
	if err != nil {
		log.Error().Err(err).Str("component", "EssenceFilter").Str("step", "LoadMatchEngine").Msg("load match data failed")
		reportFocusByKey(ctx, nil, "focus.error.load_engine_failed", err.Error())
		return false
	}
	inputLocale := matchapi.NormalizeInputLocale(opts.InputLanguage)

	log.Info().Str("component", "EssenceFilter").Str("input_language", inputLocale).Msg("match engine ready")
	reportSimpleByKey(ctx, nil, "focus.init.data_loaded")
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
		reportColoredByKey(ctx, nil, "#ff0000", "focus.init.no_essence_type")
		return false
	}

	st := &RunState{MaxItemsPerRow: 9, EssenceTypes: essenceTypes}
	st.Reset()
	st.PipelineOpts = *opts
	st.InputLanguage = inputLocale
	st.MatchEngine = engine

	matchOpts := matchOptsFromPipeline(opts)
	st.TargetSkillCombinations = engine.BuildTargets(matchOpts)
	st.MatchedCombinationSummary = make(map[string]*matchapi.SkillCombinationSummary)
	st.EssenceTypes = essenceTypes
	setRunState(st)
	reportInitSelection(ctx, st, weaponRarity, essenceTypes)

	names := make([]string, 0, len(st.TargetSkillCombinations))
	for _, combo := range st.TargetSkillCombinations {
		names = append(names, combo.Weapon.ChineseName)
	}
	vm := buildInitViewModel(st)
	filteredWeapons := vm.FilteredWeapons
	log.Info().Str("component", "EssenceFilter").Str("step", "FilterWeapons").Int("filtered_count", len(filteredWeapons)).Strs("weapons", names).Msg("weapons filtered")
	reportInitWeapons(ctx, st, filteredWeapons)

	log.Info().Str("component", "EssenceFilter").Str("step", "BuildSkillCombinations").Int("combinations", len(st.TargetSkillCombinations)).Msg("skill combinations built")
	log.Info().Str("component", "EssenceFilter").Msg("init done")

	reportInitSkillList(ctx, st, vm.SlotSkills)
	reportDataVersionNotice(ctx, st)
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
	if st := getRunState(); st != nil {
		LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.inventory_count", map[string]any{"Count": n}))
		st.TotalCount = n
	} else {
		LogMXUHTML(ctx, i18n.RenderHTML("essencefilter.inventory_count", map[string]any{"Count": n}))
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
	var params struct {
		Slot   int  `json:"slot"`
		IsLast bool `json:"is_last"`
	}
	if arg.CustomActionParam != "" {
		_ = json.Unmarshal([]byte(arg.CustomActionParam), &params)
	}
	log.Info().Str("component", "EssenceFilter").Str("action", "CheckItem").Msg("start")
	st := getRunState()
	if st == nil {
		log.Error().Str("component", "EssenceFilter").Str("action", "CheckItem").Msg("no run state")
		return false
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
	text := matchapi.NormalizeInputForMatch(rawText, st.InputLanguage)
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
		reportFocusByKey(ctx, nil, "focus.error.no_run_state")
		return false
	}
	ocr := matchapi.OCRInput{
		Skills: [3]string{st.CurrentSkills[0], st.CurrentSkills[1], st.CurrentSkills[2]},
		Levels: [3]int{st.CurrentSkillLevels[0], st.CurrentSkillLevels[1], st.CurrentSkillLevels[2]},
	}

	if st.MatchEngine == nil {
		reportFocusByKey(ctx, st, "focus.error.no_match_engine")
		return false
	}
	return runUnifiedSkillDecision(ctx, arg, st, st.MatchEngine, ocr, decisionNextNodes{
		Lock:    "EssenceFilterLockItemLog",
		Discard: "EssenceFilterDiscardItemLog",
		Skip:    "EssenceFilterRowNextItem",
	})
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
		reportColoredByKey(ctx, st, "#11cf00", "focus.row.all_marked", st.CurrentRow)
	}

	isFallbackScan := arg.CurrentTaskName == "EssenceDetectFinal"
	st.InFinalScan = isFallbackScan
	if isFallbackScan && !st.FinalLargeScanUsed {
		st.FinalLargeScanUsed = true
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceDetectFinal"}})
		reportColoredByKey(ctx, st, "#1a01fd", "focus.row.tail_scan_done")
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
		reportSimpleByKey(ctx, st, "focus.row.enter_final_scan")
		ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: "EssenceDetectFinal"}})
		return true
	}
	if st.RowIndex >= len(st.RowBoxes) {
		if (st.PhysicalItemCount == st.MaxItemsPerRow) && !st.FinalLargeScanUsed {
			rowsDone := st.CurrentRow
			remaining := st.TotalCount - st.MaxItemsPerRow*rowsDone
			if st.TotalCount > 0 && remaining <= essenceMaxSinglePageInventory {
				st.PendingFinalScan = true
				reportSimpleByKey(ctx, st, "focus.row.pending_final_swipe", remaining, essenceMaxSinglePageInventory, st.TotalCount, rowsDone)
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
			reportSimpleByKey(ctx, st, "focus.row.swipe_to", st.CurrentRow+1)
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
		reportColoredByKey(ctx, st, "#11cf00", "focus.finish.summary", st.VisitedCount, st.MatchedCount)
		reportFinishExtRuleStats(ctx, st)
		reportFinishArtifacts(ctx, st)
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

func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Compile-time interface checks
var (
	_ maa.CustomActionRunner = &EssenceFilterInitAction{}
	_ maa.CustomActionRunner = &OCREssenceInventoryNumberAction{}
	_ maa.CustomActionRunner = &EssenceFilterTraceAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemLevelAction{}
	_ maa.CustomActionRunner = &EssenceFilterSkillDecisionAction{}
	_ maa.CustomActionRunner = &EssenceFilterRowCollectAction{}
	_ maa.CustomActionRunner = &EssenceFilterRowNextItemAction{}
	_ maa.CustomActionRunner = &EssenceFilterFinishAction{}
	_ maa.CustomActionRunner = &EssenceFilterSwipeCalibrateAction{}
)

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
	swipeDist := int(float64(intAbs(delta)) * calibrateScrollRatio)
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
