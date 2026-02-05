package essence

import (
	"encoding/json"
	"image"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type scanGridParam struct {
	StartX int `json:"start_x"`
	StartY int `json:"start_y"`
	EndX   int `json:"end_x"`
	EndY   int `json:"end_y"`
	Width  int `json:"width"`
	Height int `json:"height"`
	Rows   int `json:"rows"`
	Cols   int `json:"cols"`

	LockCenterX int `json:"lock_center_x"`
	LockCenterY int `json:"lock_center_y"`

	ClickDelayMs int `json:"click_delay_ms"`
	TooltipDelayMs int `json:"tooltip_delay_ms"`
	OcrRetryCount int `json:"ocr_retry_count"`
	OcrRetryDelayMs int `json:"ocr_retry_delay_ms"`
	UseColorBars bool `json:"use_color_bars"`
	BarTemplates []string `json:"bar_templates"`
	BarThreshold float64 `json:"bar_threshold"`
	BarRoi []int `json:"bar_roi"`
	BarClickOffsetX int `json:"bar_click_offset_x"`
	BarClickOffsetY int `json:"bar_click_offset_y"`
	BarRowTolerance int `json:"bar_row_tolerance"`

	PreferredWeaponFlags map[string]string `json:"preferred_weapon_flags"`
}

// EssenceScanGridAction 扫描多格基质并执行锁定/解锁。
type EssenceScanGridAction struct{}

// Run 实现 CustomActionRunner 接口。
func (a *EssenceScanGridAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	param := scanGridParam{
		StartX: 35,
		StartY: 85,
		EndX:   863,
		EndY:   500,
		Width:  100,
		Height: 100,
		Rows:   5,
		Cols:   9,

		LockCenterX: 1218,
		LockCenterY: 191,

		ClickDelayMs: 120,
		TooltipDelayMs: 200,
		OcrRetryCount: 1,
		OcrRetryDelayMs: 200,
		UseColorBars: true,
		BarTemplates: []string{
			"EssenceTool/blueessence.png",
			"EssenceTool/greenessence.png",
			"EssenceTool/purpleessence.png",
			"EssenceTool/yellowessence.png",
		},
		BarThreshold: 0.9,
		BarClickOffsetX: 0,
		BarClickOffsetY: -30,
		BarRowTolerance: 12,
	}
	if arg.CustomActionParam != "" {
		log.Info().Str("param", arg.CustomActionParam).Msg("essence: scan grid action param raw")
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
			log.Warn().Err(err).Msg("essence: failed to parse scan grid param, using defaults")
		}
	}
	log.Info().
		Bool("useColorBars", param.UseColorBars).
		Msg("essence: scan grid config")

	if param.Rows <= 0 || param.Cols <= 0 {
		log.Warn().Msg("essence: invalid grid size")
		return false
	}

	showSelectedWeaponTips(ctx, param.PreferredWeaponFlags)

	stepX := float64(param.EndX-param.StartX) / float64(max(param.Cols-1, 1))
	stepY := float64(param.EndY-param.StartY) / float64(max(param.Rows-1, 1))

	ctrl := ctx.GetTasker().GetController()
	centers := []point{}
	barHits := []barHit{}
	if param.UseColorBars {
		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err == nil && img != nil {
			barHits = append(barHits, findBarHits(ctx, img, param)...)
		}
	}

	if param.UseColorBars && len(barHits) > 0 {
		rowGroups := groupBarHitsByRow(barHits, param.BarRowTolerance)
		for _, row := range rowGroups {
			for _, hit := range row {
				centers = append(centers, barClickPoint(hit, param))
			}
		}
	}

	if len(centers) == 0 {
		for r := 0; r < param.Rows; r++ {
			for c := 0; c < param.Cols; c++ {
				x := int(math.Round(float64(param.StartX) + float64(c)*stepX))
				y := int(math.Round(float64(param.StartY) + float64(r)*stepY))
				centers = append(centers, point{x: x + param.Width/2, y: y + param.Height/2})
			}
		}
	}

	for _, center := range centers {
		ctrl.PostClick(int32(center.x), int32(center.y))
		if param.ClickDelayMs > 0 {
			time.Sleep(time.Duration(param.ClickDelayMs) * time.Millisecond)
		}
		if !handleEssenceSelection(ctx, ctrl, param) {
			continue
		}
	}

	return true
}

type point struct {
	x int
	y int
}

type barHit struct {
	point
	box  [4]int // absolute [x, y, w, h]
}

func handleEssenceSelection(ctx *maa.Context, ctrl *maa.Controller, param scanGridParam) bool {
	if param.TooltipDelayMs > 0 {
		time.Sleep(time.Duration(param.TooltipDelayMs) * time.Millisecond)
	}

	var judgeDetail *maa.RecognitionDetail
	var judgeErr error
	var imgUsed image.Image
	for attempt := 0; attempt <= param.OcrRetryCount; attempt++ {
		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil || img == nil {
			log.Warn().Err(err).Msg("essence: failed to capture image")
			break
		}
		imgUsed = img
		judgeDetail, judgeErr = ctx.RunRecognition("EssenceTooltipJudge", img, buildTooltipOverride(param.PreferredWeaponFlags))
		if judgeErr == nil && judgeDetail != nil && judgeDetail.Hit {
			break
		}
		if attempt < param.OcrRetryCount && param.OcrRetryDelayMs > 0 {
			time.Sleep(time.Duration(param.OcrRetryDelayMs) * time.Millisecond)
		}
	}
	if judgeErr != nil || judgeDetail == nil || !judgeDetail.Hit {
		return false
	}

	result, ok := parseJudgeResult(judgeDetail.DetailJson)
	if !ok {
		log.Warn().Str("detail", judgeDetail.DetailJson).Msg("essence: failed to parse judge detail")
		return false
	}

	if result.Decision == "Treasure" {
		if imgUsed != nil {
			unlockedDetail, _ := ctx.RunRecognition("EssenceLockStateUnlocked_Lock", imgUsed, nil)
			if unlockedDetail != nil && unlockedDetail.Hit {
				_, _ = ctx.RunTask("EssenceLockStateUnlocked_Lock")
			}
		}
	} else {
		if imgUsed != nil {
			lockedDetail, _ := ctx.RunRecognition("EssenceLockStateLocked_Unlock", imgUsed, nil)
			if lockedDetail != nil && lockedDetail.Hit {
				_, _ = ctx.RunTask("EssenceLockStateLocked_Unlock")
			}
		}
	}
	return true
}

func buildTooltipOverride(flags map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"EssenceTooltipJudge": map[string]interface{}{
			"recognition": map[string]interface{}{
				"type": "Custom",
				"param": map[string]interface{}{
					"custom_recognition": "EssenceTooltipJudge",
					"custom_recognition_param": map[string]interface{}{
						"s1_node":                "EssenceTooltip_S1",
						"s2_node":                "EssenceTooltip_S2",
						"s3_node":                "EssenceTooltip_S3",
						"preferred_weapon_flags": flags,
					},
				},
			},
		},
	}
}

func findBarHits(ctx *maa.Context, img image.Image, param scanGridParam) []barHit {
	roiX, roiY, roiW, roiH := resolveBarRoi(param)
	if roiW <= 0 || roiH <= 0 {
		return nil
	}

	threshold := param.BarThreshold
	if threshold <= 0 {
		threshold = 0.9
	}

	hits := make([]barHit, 0)
	seen := map[int64]struct{}{}
	for _, tmpl := range param.BarTemplates {
		detail, err := ctx.RunRecognitionDirect("TemplateMatch", maa.NodeTemplateMatchParam{
			Threshold: []float64{threshold},
			Template:  []string{tmpl},
			ROI:       maa.NewTargetRect(maa.Rect{roiX, roiY, roiW, roiH}),
		}, img)
		if err != nil || detail == nil || !detail.Hit || detail.DetailJson == "" {
			continue
		}
		for _, box := range extractTemplateBoxes(detail.DetailJson) {
			absBox := [4]int{roiX + box[0], roiY + box[1], box[2], box[3]}
			x := absBox[0] + absBox[2]/2
			y := absBox[1] + absBox[3]/2
			key := (int64(x) << 32) | int64(uint32(y))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			hits = append(hits, barHit{point: point{x: x, y: y}, box: absBox})
		}
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].y == hits[j].y {
			return hits[i].x < hits[j].x
		}
		return hits[i].y < hits[j].y
	})
	return hits
}

func resolveBarRoi(param scanGridParam) (int, int, int, int) {
	if len(param.BarRoi) >= 4 {
		return param.BarRoi[0], param.BarRoi[1], param.BarRoi[2], param.BarRoi[3]
	}
	return param.StartX, param.StartY, param.EndX - param.StartX + param.Width, param.EndY - param.StartY + param.Height
}

func extractTemplateBoxes(detail string) [][4]int {
	var tm struct {
		Filtered []struct {
			Box [4]int `json:"box"`
		} `json:"filtered"`
	}
	if err := json.Unmarshal([]byte(detail), &tm); err != nil {
		return nil
	}
	boxes := make([][4]int, 0, len(tm.Filtered))
	for _, item := range tm.Filtered {
		boxes = append(boxes, item.Box)
	}
	return boxes
}

func barClickPoint(hit barHit, param scanGridParam) point {
	x := hit.box[0] + hit.box[2]/2
	y := hit.box[1] + hit.box[3]/2
	if param.Height > 0 {
		// Click above the color bar, roughly item center.
		y = hit.box[1] - param.Height/2 + hit.box[3]
	}
	return point{
		x: x + param.BarClickOffsetX,
		y: y + param.BarClickOffsetY,
	}
}

func groupBarHitsByRow(hits []barHit, tolerance int) [][]barHit {
	if len(hits) == 0 {
		return nil
	}
	if tolerance <= 0 {
		tolerance = 12
	}
	rows := make([][]barHit, 0)
	current := []barHit{hits[0]}
	rowY := hits[0].y
	for i := 1; i < len(hits); i++ {
		if abs(hits[i].y-rowY) <= tolerance {
			current = append(current, hits[i])
		} else {
			rows = append(rows, current)
			current = []barHit{hits[i]}
			rowY = hits[i].y
		}
	}
	rows = append(rows, current)
	return rows
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseJudgeResult(detail string) (JudgeResult, bool) {
	if detail == "" {
		return JudgeResult{}, false
	}
	var result JudgeResult
	if err := json.Unmarshal([]byte(detail), &result); err == nil && result.Decision != "" {
		return result, true
	}

	var wrappedRaw struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail), &wrappedRaw); err == nil && len(wrappedRaw.Best.Detail) > 0 {
		if err := json.Unmarshal(wrappedRaw.Best.Detail, &result); err == nil && result.Decision != "" {
			return result, true
		}
	}

	var wrappedString struct {
		Best struct {
			Detail string `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail), &wrappedString); err == nil && wrappedString.Best.Detail != "" {
		if err := json.Unmarshal([]byte(wrappedString.Best.Detail), &result); err == nil && result.Decision != "" {
			return result, true
		}
		if unquoted, err := strconv.Unquote(wrappedString.Best.Detail); err == nil {
			if err := json.Unmarshal([]byte(unquoted), &result); err == nil && result.Decision != "" {
				return result, true
			}
		}
	}

	return JudgeResult{}, false
}

func showSelectedWeaponTips(ctx *maa.Context, flags map[string]string) {
	selected := extractPreferredWeaponsFromFlags(flags)
	if len(selected) == 0 {
		runMessageTask(ctx, "未选择武器，按默认规则判定宝藏/养成材料")
		return
	}

	_ = EnsureDataReady()

	attrSet := map[string]struct{}{}
	for _, w := range allWeapons() {
		if _, ok := flags[w.Name]; ok && isTruthy(flags[w.Name]) {
			attrSet[normalizeAttr(w.S1)] = struct{}{}
			attrSet[normalizeAttr(w.S2)] = struct{}{}
			attrSet[normalizeAttr(w.S3)] = struct{}{}
		}
	}

	attrs := make([]string, 0, len(attrSet))
	for attr := range attrSet {
		if attr != "" {
			attrs = append(attrs, attr)
		}
	}

	msg := "选中武器: " + joinShort(selected) + "\n需要词条: " + joinShort(attrs)
	runMessageTask(ctx, msg)
}

func runMessageTask(ctx *maa.Context, text string) {
	ctx.RunTask("[Essence]TaskShowMessage", map[string]interface{}{
		"[Essence]TaskShowMessage": map[string]interface{}{
			"recognition": "DirectHit",
			"action":      "DoNothing",
			"focus": map[string]interface{}{
				"Node.Action.Starting": text,
			},
		},
	})
}


func joinShort(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	if len(items) > 10 {
		return strings.Join(items[:10], "、") + "…"
	}
	return strings.Join(items, "、")
}
