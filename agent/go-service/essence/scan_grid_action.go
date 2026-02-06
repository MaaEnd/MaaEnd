package essence

import (
	"encoding/json"
	"image"
	"strings"
	"time"

	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ---------- 常量 / Constants ----------

// clickDelay 点击基质后等待 tooltip 出现的时间。
// clickDelay is the pause after clicking an item, allowing the tooltip to appear.
const clickDelay = 200 * time.Millisecond

// ---------- 主结构 / Main Action ----------

// EssenceScanGridAction 扫描当前页面的基质并逐个判定 + 锁/解锁。
// EssenceScanGridAction scans visible essences and toggles lock per judgment.
type EssenceScanGridAction struct{}

// Run 实现 maa.CustomActionRunner 接口，由 pipeline EssenceToolMain 触发。
// Run implements maa.CustomActionRunner; triggered by pipeline EssenceToolMain.
func (a *EssenceScanGridAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if stopping(ctx) {
		return true
	}

	// ---- 1. 读取武器开关 / Read weapon flags from node attach ----
	// attach 第一层 key = 武器名, value = "Yes"；dict merge 保证多选不互相覆盖。
	// Top-level attach keys are weapon names; dict merge keeps all keys.
	flags := readWeaponFlagsFromAttach(ctx, arg.CurrentTaskName)
	selected := extractPreferredWeaponsFromFlags(flags)
	log.Info().Int("selectedWeapons", len(selected)).Msg("essence: scan start")

	// ---- 2. 显示选中武器摘要 / Show selected weapon summary ----
	showSelectedWeaponTips(ctx, flags)
	if stopping(ctx) {
		return true
	}

	// ---- 3. 截图 + 模板匹配定位基质 / Screenshot + locate items ----
	ctrl := ctx.GetTasker().GetController()
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Warn().Err(err).Msg("essence: screenshot failed")
		return true
	}
	tiles := findTiles(ctx, img)
	if len(tiles) == 0 {
		log.Info().Msg("essence: no tiles found")
		return true
	}
	log.Info().Int("count", len(tiles)).Msg("essence: tiles detected")

	// ---- 4. 逐个点击 → OCR → 锁/解锁 / Click each tile → OCR → lock/unlock ----
	for i, pt := range tiles {
		if stopping(ctx) {
			return true
		}
		log.Debug().Int("index", i).Int("x", pt.x).Int("y", pt.y).Msg("essence: clicking tile")
		ctrl.PostClick(int32(pt.x), int32(pt.y))
		time.Sleep(clickDelay)
		if stopping(ctx) {
			return true
		}
		handleTile(ctx, ctrl, flags)
	}

	return true
}

// ---------- 子流程 / Sub-routines ----------

// handleTile 截图 → 判定 → 按结果锁/解锁。
// handleTile captures screen, judges the essence, then locks or unlocks.
func handleTile(ctx *maa.Context, ctrl *maa.Controller, flags map[string]string) {
	if stopping(ctx) {
		return
	}
	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil || img == nil {
		log.Warn().Err(err).Msg("essence: capture failed")
		return
	}

	// 调用 Go 自定义识别判定宝藏/材料
	// Run custom recognition to judge Treasure vs Material
	override := buildJudgeOverride(flags)
	detail, err := ctx.RunRecognition("EssenceTooltipJudge", img, override)
	if err != nil || detail == nil || !detail.Hit {
		return
	}
	result, ok := parseJudgeResult(detail.DetailJson)
	if !ok {
		return
	}

	// 根据判定结果执行锁/解锁
	// Lock or unlock based on judgment
	if result.Decision == "Treasure" {
		// 宝藏 → 确保锁定 / Treasure → ensure locked
		tryToggleLock(ctx, img, "EssenceLockStateUnlocked_Lock")
	} else {
		// 材料 → 确保解锁 / Material → ensure unlocked
		tryToggleLock(ctx, img, "EssenceLockStateLocked_Unlock")
	}
}

// tryToggleLock 先识别是否需要切换锁状态，再执行点击。
// tryToggleLock checks if a lock toggle is needed, then clicks if so.
func tryToggleLock(ctx *maa.Context, img image.Image, nodeName string) {
	if stopping(ctx) {
		return
	}
	det, _ := ctx.RunRecognition(nodeName, img, nil)
	if det != nil && det.Hit {
		ctx.RunTask(nodeName)
	}
}

// ---------- 基质定位 / Tile Detection ----------

// point 表示屏幕上一个坐标点。
// point represents a screen coordinate.
type point struct{ x, y int }

// findTiles 通过 EssenceTileMatch 模板匹配找到所有基质位置。
// findTiles locates all essence tiles via EssenceTileMatch template matching.
func findTiles(ctx *maa.Context, img image.Image) []point {
	if stopping(ctx) {
		return nil
	}
	detail, err := ctx.RunRecognition("EssenceTileMatch", img, nil)
	if err != nil || detail == nil || !detail.Hit || detail.DetailJson == "" {
		return nil
	}

	var tm struct {
		Filtered []struct {
			Box [4]int `json:"box"`
		} `json:"filtered"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &tm); err != nil {
		return nil
	}

	pts := make([]point, 0, len(tm.Filtered))
	for _, item := range tm.Filtered {
		// box = [x, y, w, h]，取左上角作为点击坐标
		// box = [x, y, w, h]; use top-left as click target
		pts = append(pts, point{x: item.Box[0], y: item.Box[1]})
	}
	return pts
}

// ---------- 判定结果解析 / Judge Result Parsing ----------

// buildJudgeOverride 构造 RunRecognition 的 override，注入 OCR 节点名和武器开关。
// buildJudgeOverride builds the override map for EssenceTooltipJudge recognition.
func buildJudgeOverride(flags map[string]string) map[string]interface{} {
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

// parseJudgeResult 解析自定义识别返回的 JSON，提取判定结果。
// parseJudgeResult extracts JudgeResult from the recognition detail JSON.
func parseJudgeResult(detail string) (JudgeResult, bool) {
	if detail == "" {
		return JudgeResult{}, false
	}
	// 直接解析 / Direct parse
	var result JudgeResult
	if err := json.Unmarshal([]byte(detail), &result); err == nil && result.Decision != "" {
		return result, true
	}
	// 兼容 best.detail 包裹格式 / Fallback: wrapped in best.detail
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail), &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
		if err := json.Unmarshal(wrapped.Best.Detail, &result); err == nil && result.Decision != "" {
			return result, true
		}
	}
	return JudgeResult{}, false
}

// ---------- attach 读取 / Attach Reader ----------

// readWeaponFlagsFromAttach 从节点 attach 的第一层 key 读取武器开关。
// 每个 GUI switch 在 attach 第一层写一个 key（武器名 → "Yes"），
// dict merge 保证多选时所有 key 并存。
//
// readWeaponFlagsFromAttach reads weapon flags from attach top-level keys.
// Each GUI switch sets one key (weapon name → "Yes") in attach;
// dict merge preserves all keys across multiple overrides.
func readWeaponFlagsFromAttach(ctx *maa.Context, nodeName string) map[string]string {
	nodeJSON, err := ctx.GetNodeJSON(nodeName)
	if err != nil || nodeJSON == "" {
		log.Warn().Err(err).Str("node", nodeName).Msg("essence: failed to get node JSON")
		return nil
	}
	var parsed struct {
		Attach map[string]string `json:"attach"`
	}
	if err := json.Unmarshal([]byte(nodeJSON), &parsed); err != nil {
		log.Warn().Err(err).Msg("essence: failed to parse attach")
		return nil
	}
	log.Info().Int("totalFlags", len(parsed.Attach)).Msg("essence: weapon flags from attach")
	return parsed.Attach
}

// ---------- UI 提示 / UI Tips ----------

// showSelectedWeaponTips 在 GUI 显示选中武器及所需词条。
// showSelectedWeaponTips displays selected weapons and required attributes in the GUI.
func showSelectedWeaponTips(ctx *maa.Context, flags map[string]string) {
	if stopping(ctx) {
		return
	}
	selected := extractPreferredWeaponsFromFlags(flags)
	if len(selected) == 0 {
		showMessage(ctx, "未选择武器，按默认规则判定宝藏/养成材料")
		return
	}

	if err := EnsureDataReady(); err != nil {
		showMessage(ctx, "选中武器: "+joinShort(selected)+"\n武器数据加载失败")
		return
	}

	// 收集选中武器涉及的所有词条
	// Collect all attributes from selected weapons
	attrSet := map[string]struct{}{}
	for _, w := range allWeapons() {
		if isTruthy(flags[w.Name]) {
			for _, a := range []string{w.S1, w.S2, w.S3} {
				if n := normalizeAttr(a); n != "" {
					attrSet[n] = struct{}{}
				}
			}
		}
	}
	attrs := make([]string, 0, len(attrSet))
	for a := range attrSet {
		attrs = append(attrs, a)
	}

	showMessage(ctx, "选中武器: "+joinShort(selected)+"\n需要词条: "+joinShort(attrs))
}

// showMessage 通过轻量 pipeline 任务在 GUI 显示文本。
// showMessage displays text in the GUI via a lightweight pipeline task.
func showMessage(ctx *maa.Context, text string) {
	if stopping(ctx) {
		return
	}
	ctx.RunTask("[Essence]TaskShowMessage", map[string]interface{}{
		"[Essence]TaskShowMessage": map[string]interface{}{
			"recognition": "DirectHit",
			"action":      "DoNothing",
			"focus":       map[string]interface{}{"Node.Action.Starting": text},
		},
	})
}

// ---------- 工具函数 / Utilities ----------

// stopping 检查任务是否正在停止或已停止。
// stopping checks whether the task is stopping or already stopped.
func stopping(ctx *maa.Context) bool {
	if ctx == nil {
		return true
	}
	t := ctx.GetTasker()
	if t == nil {
		return true
	}
	return t.Stopping() || !t.Running()
}

// joinShort 将字符串列表缩略展示（超过 10 个截断）。
// joinShort abbreviates a string slice for display (truncated after 10).
func joinShort(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	if len(items) > 10 {
		return strings.Join(items[:10], "、") + "…"
	}
	return strings.Join(items, "、")
}
