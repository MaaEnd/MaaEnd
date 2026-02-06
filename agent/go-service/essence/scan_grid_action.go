package essence

import (
	"encoding/json"
	"image"
	"strconv"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// scanGridParam controls click timing and OCR retry behavior.
// scanGridParam 控制点击节奏与 OCR 重试。
type scanGridParam struct {
	ClickDelayMs int `json:"click_delay_ms"`
	TooltipDelayMs int `json:"tooltip_delay_ms"`
	OcrRetryCount int `json:"ocr_retry_count"`
	OcrRetryDelayMs int `json:"ocr_retry_delay_ms"`

	PreferredWeaponFlags map[string]string `json:"preferred_weapon_flags"`
}

// EssenceScanGridAction scans visible items and toggles lock state.
// EssenceScanGridAction 扫描可见基质并执行锁/解锁。
type EssenceScanGridAction struct{}

// Run 实现 CustomActionRunner 接口。
func (a *EssenceScanGridAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if taskerStopping(ctx) {
		log.Info().Msg("essence: task stopping before scan start")
		return true
	}
	// Defaults are tuned for 1280x720 UI timing.
	// 默认参数按 1280x720 UI 调整。
	param := scanGridParam{
		ClickDelayMs: 120,
		TooltipDelayMs: 200,
		OcrRetryCount: 1,
		OcrRetryDelayMs: 200,
	}
	if arg.CustomActionParam != "" {
		log.Info().Str("param", arg.CustomActionParam).Msg("essence: scan grid action param raw")
		if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
			log.Warn().Err(err).Msg("essence: failed to parse scan grid param, using defaults")
		}
	}
	log.Info().Msg("essence: scan grid config")

	if taskerStopping(ctx) {
		log.Info().Msg("essence: task stopping before showing tips")
		return true
	}
	// Capture once, then locate all items via template match.
	// 截图一次后用模板匹配定位所有基质。
	ctrl := ctx.GetTasker().GetController()
	centers := []point{}
	barHits := []barHit{}
	if taskerStopping(ctx) {
		log.Info().Msg("essence: task stopping before tile scan")
		return true
	}
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err == nil && img != nil {
		barHits = append(barHits, findBarHits(ctx, img, param)...)
	}

	if len(barHits) > 0 {
		for _, hit := range barHits {
			centers = append(centers, hit.point)
		}
	}

	if len(centers) == 0 {
		log.Info().Msg("essence: no tiles found, skip grid clicks")
		return true
	}

	// Click each detected item and run tooltip/lock logic.
	// 逐个点击并进行 OCR/锁定逻辑。
	for _, center := range centers {
		if taskerStopping(ctx) {
			log.Info().Msg("essence: task stopping during scan loop")
			return true
		}
		runTileClickTask(ctx, center.x, center.y)
		if param.ClickDelayMs > 0 {
			time.Sleep(time.Duration(param.ClickDelayMs) * time.Millisecond)
		}
		if taskerStopping(ctx) {
			log.Info().Msg("essence: task stopping after click delay")
			return true
		}
		if !handleEssenceSelection(ctx, ctrl, param) {
			if taskerStopping(ctx) {
				log.Info().Msg("essence: task stopping after selection handling")
				return true
			}
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

// handleEssenceSelection OCRs the tooltip and applies lock/unlock if needed.
// handleEssenceSelection OCR 读取词条并按结果锁/解锁。
func handleEssenceSelection(ctx *maa.Context, ctrl *maa.Controller, param scanGridParam) bool {
	if taskerStopping(ctx) {
		return false
	}
	if param.TooltipDelayMs > 0 {
		time.Sleep(time.Duration(param.TooltipDelayMs) * time.Millisecond)
	}
	if taskerStopping(ctx) {
		return false
	}

	var judgeDetail *maa.RecognitionDetail
	var judgeErr error
	var imgUsed image.Image
	// Retry OCR briefly in case tooltip animation is late.
	// Tooltip 可能延迟出现，因此做短暂重试。
	for attempt := 0; attempt <= param.OcrRetryCount; attempt++ {
		if taskerStopping(ctx) {
			return false
		}
		if taskerStopping(ctx) {
			return false
		}
		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil || img == nil {
			log.Warn().Err(err).Msg("essence: failed to capture image")
			break
		}
		imgUsed = img
		if taskerStopping(ctx) {
			return false
		}
		judgeDetail, judgeErr = ctx.RunRecognition("EssenceTooltipJudge", img, buildTooltipOverride(param.PreferredWeaponFlags))
		if taskerStopping(ctx) {
			return false
		}
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
		if taskerStopping(ctx) {
			return false
		}
		if imgUsed != nil {
			if taskerStopping(ctx) {
				return false
			}
			unlockedDetail, _ := ctx.RunRecognition("EssenceLockStateUnlocked_Lock", imgUsed, nil)
			if unlockedDetail != nil && unlockedDetail.Hit {
				if taskerStopping(ctx) {
					return false
				}
				_, _ = ctx.RunTask("EssenceLockStateUnlocked_Lock")
			}
		}
	} else {
		if taskerStopping(ctx) {
			return false
		}
		if imgUsed != nil {
			if taskerStopping(ctx) {
				return false
			}
			lockedDetail, _ := ctx.RunRecognition("EssenceLockStateLocked_Unlock", imgUsed, nil)
			if lockedDetail != nil && lockedDetail.Hit {
				if taskerStopping(ctx) {
					return false
				}
				_, _ = ctx.RunTask("EssenceLockStateLocked_Unlock")
			}
		}
	}
	return true
}

// buildTooltipOverride injects preferred weapon flags into tooltip judge.
// buildTooltipOverride 注入偏好武器开关到判定节点。
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

// findBarHits resolves item positions from EssenceTileMatch.
// findBarHits 从 EssenceTileMatch 得到基质位置。
func findBarHits(ctx *maa.Context, img image.Image, param scanGridParam) []barHit {
	if taskerStopping(ctx) {
		return nil
	}

	hits := make([]barHit, 0)
	if taskerStopping(ctx) {
		return hits
	}
	detail, err := ctx.RunRecognition("EssenceTileMatch", img, nil)
	if taskerStopping(ctx) {
		return hits
	}
	if err != nil || detail == nil || !detail.Hit || detail.DetailJson == "" {
		return hits
	}
	if taskerStopping(ctx) {
		return hits
	}
	for _, box := range extractTemplateBoxes(detail.DetailJson) {
		x := box[0]
		y := box[1]
		hits = append(hits, barHit{point: point{x: x, y: y}, box: box})
	}

	return hits
}

// extractTemplateBoxes parses filtered boxes from TemplateMatch detail JSON.
// extractTemplateBoxes 解析模板匹配的 filtered box。
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

// parseJudgeResult handles different JSON wrappers from recognition detail.
// parseJudgeResult 兼容不同识别返回格式。
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

// showSelectedWeaponTips prints selected weapons and derived attributes.
// showSelectedWeaponTips 输出选中武器与词条提示。
// runTileClickTask clicks a single tile via pipeline action override.
// runTileClickTask 通过 pipeline 点击一个基质坐标。
func runTileClickTask(ctx *maa.Context, x, y int) {
	if taskerStopping(ctx) {
		return
	}
	_, _ = ctx.RunTask("EssenceTileClick", map[string]interface{}{
		"EssenceTileClick": map[string]interface{}{
			"recognition": "DirectHit",
			"action": map[string]interface{}{
				"type": "Click",
				"param": map[string]interface{}{
					"target": []int{x, y},
				},
			},
		},
	})
}

// taskerStopping checks if the task is stopping or already stopped.
// taskerStopping 判断任务是否停止/将停止。
func taskerStopping(ctx *maa.Context) bool {
	if ctx == nil {
		return true
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return true
	}
	return tasker.Stopping() || !tasker.Running()
}

