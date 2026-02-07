package essence

import (
	"encoding/json"
	"image"
	"strings"

	"github.com/MaaXYZ/maa-framework-go/v4"
)

// EssenceScanGridAction 基质扫描主 Action。
// Go 负责：读配置 → 显示提示 → 注入武器开关 →
//
//	翻页循环（一次模板匹配 → 逐个 RunTask 点击 → 判定 → 滑动 → 底部检测）。
//
// EssenceScanGridAction is the main scan action.
// Go handles: read config → show tips → inject flags →
//
//	page loop (one template match → RunTask click each → judge → swipe → bottom detection).
type EssenceScanGridAction struct{}

// Run 实现 maa.CustomActionRunner 接口。
// Run implements maa.CustomActionRunner.
func (a *EssenceScanGridAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if stopping(ctx) {
		return true
	}

	// ---- 1. 从 attach 读取武器开关 / Read weapon flags from attach ----
	flags := readWeaponFlagsFromAttach(ctx, arg.CurrentTaskName)
	selected := extractPreferredWeaponsFromFlags(flags)
	essLog.Info().Int("selectedWeapons", len(selected)).Msg("scan start")

	// ---- 2. 显示选中武器摘要 / Show summary ----
	showSelectedWeaponTips(ctx, flags)
	if stopping(ctx) {
		return true
	}

	// ---- 3. 注入武器开关到 Pipeline 判定节点 / Inject flags into judge nodes ----
	if err := ctx.OverridePipeline(buildJudgeOverride(flags)); err != nil {
		essLog.Warn().Err(err).Msg("failed to override pipeline")
	}

	ctrl := ctx.GetTasker().GetController()

	// 底部 ROI，用于翻页前后截图比较（相似 = 已到底）
	// Bottom ROI for before/after swipe comparison (similar = reached bottom)
	bottomROI := image.Rect(33, 519, 33+926, 519+111)
	const similarityThreshold = 0.95
	const maxPages = 20

	// ---- 4. 翻页循环 / Page loop ----
	for page := 0; page < maxPages; page++ {
		if stopping(ctx) {
			return true
		}

		// 截图 → 找基质 / Screenshot → find tiles
		ctrl.PostScreencap().Wait()
		img, err := ctrl.CacheImage()
		if err != nil || img == nil {
			essLog.Warn().Err(err).Msg("screenshot failed")
			break
		}
		boxes := findTileBoxes(ctx, img)
		if len(boxes) == 0 {
			if page == 0 {
				essLog.Info().Msg("no tiles found")
			}
			break
		}
		essLog.Info().Int("page", page).Int("count", len(boxes)).Msg("tiles detected")

		// 逐个处理 / Process each tile
		for i, box := range boxes {
			if stopping(ctx) {
				return true
			}
			essLog.Debug().Int("index", i).Int("total", len(boxes)).Msg("processing tile")

			// 点击基质（RunTask + ROI override，Pipeline 在小范围内匹配并点击）
			// post_wait_freezes 会等 tooltip 动画结束，RunTask 完整执行节点属性。
			//
			// Click tile (RunTask + ROI override, Pipeline matches in tight ROI and clicks).
			// post_wait_freezes waits for tooltip animation; RunTask processes full node props.
			if _, err := ctx.RunTask("EssenceClickTile", buildClickOverride(box)); err != nil {
				essLog.Warn().Err(err).Int("index", i).Msg("click tile failed")
				continue
			}

			// Pipeline 判定→锁/解锁
			// Pipeline judge → lock/unlock
			ctx.RunTask("EssenceJudgeChain")
		}

		// ---- 翻页检测 / Pagination check ----
		// 翻页前截图，保存底部 ROI
		// Screenshot before swipe, save bottom ROI
		ctrl.PostScreencap().Wait()
		beforeImg, err := ctrl.CacheImage()
		if err != nil || beforeImg == nil {
			break
		}
		beforeCrop := cropROI(beforeImg, bottomROI)

		if stopping(ctx) {
			return true
		}

		// 向下滑动（Pipeline 节点含 post_wait_freezes 等滚动动画结束）
		// Swipe down (pipeline node has post_wait_freezes to wait for scroll animation)
		ctx.RunTask("EssenceSwipeDown")

		// 翻页后截图，比较底部 ROI
		// Screenshot after swipe, compare bottom ROI
		ctrl.PostScreencap().Wait()
		afterImg, err := ctrl.CacheImage()
		if err != nil || afterImg == nil {
			break
		}
		afterCrop := cropROI(afterImg, bottomROI)

		sim := imageSimilarity(beforeCrop, afterCrop)
		essLog.Info().Float64("similarity", sim).Int("page", page).Msg("page scroll check")

		if sim > similarityThreshold {
			essLog.Info().Int("pages", page+1).Msg("reached bottom")
			break
		}
	}

	return true
}

// ---------- Pipeline Override 构造 / Pipeline Override Builders ----------

// buildJudgeOverride 构造 OverridePipeline 参数，注入 OCR 节点名和武器开关到两个判定节点。
// buildJudgeOverride builds OverridePipeline params, injecting OCR node names and weapon flags.
func buildJudgeOverride(flags map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"EssenceJudge_Treasure": judgeNodeOverride("Treasure", flags),
		"EssenceJudge_Material": judgeNodeOverride("Material", flags),
	}
}

// judgeNodeOverride 为单个判定节点生成 override。
// judgeNodeOverride generates override for one judge node.
func judgeNodeOverride(decision string, flags map[string]string) map[string]interface{} {
	return map[string]interface{}{
		"recognition": map[string]interface{}{
			"param": map[string]interface{}{
				"custom_recognition_param": map[string]interface{}{
					"s1_node":                "EssenceTooltip_S1",
					"s2_node":                "EssenceTooltip_S2",
					"s3_node":                "EssenceTooltip_S3",
					"only_decision":          decision,
					"preferred_weapon_flags": flags,
				},
			},
		},
	}
}

// buildClickOverride 构造 EssenceClickTile 的 RunTask override，
// 将识别 ROI 缩小到目标 tile 周围（+margin），保证模板匹配瞬间完成。
//
// buildClickOverride creates RunTask override for EssenceClickTile,
// narrowing recognition ROI to the target tile area (+margin) for instant matching.
func buildClickOverride(box maa.Rect) map[string]interface{} {
	margin := 10
	x := box.X() - margin
	y := box.Y() - margin
	w := box.Width() + 2*margin
	h := box.Height() + 2*margin
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return map[string]interface{}{
		"EssenceClickTile": map[string]interface{}{
			"recognition": map[string]interface{}{
				"param": map[string]interface{}{
					"roi": []int{x, y, w, h},
				},
			},
		},
	}
}

// ---------- 基质定位 / Tile Detection ----------

// findTileBoxes 通过 EssenceTileMatch 一次模板匹配获取所有基质 box。
// 返回 maa.Rect 切片，后续用于 buildClickOverride 的 ROI。
//
// findTileBoxes runs EssenceTileMatch once and returns all tile boxes.
// Returns maa.Rect slice for ROI overrides via buildClickOverride.
func findTileBoxes(ctx *maa.Context, img image.Image) []maa.Rect {
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
	boxes := make([]maa.Rect, 0, len(tm.Filtered))
	for _, item := range tm.Filtered {
		boxes = append(boxes, maa.Rect{
			item.Box[0], item.Box[1],
			item.Box[2], item.Box[3],
		})
	}
	return boxes
}

// ---------- 图像比较 / Image Comparison ----------

// cropROI 从图像中裁剪指定 ROI 区域。
// cropROI extracts a sub-image for the given ROI rectangle.
func cropROI(img image.Image, roi image.Rectangle) image.Image {
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := img.(subImager); ok {
		return si.SubImage(roi)
	}
	return img
}

// imageSimilarity 计算两个等大小图像的像素相似度，返回 [0.0, 1.0]。
// 1.0 = 完全相同，用于翻页到底检测。
//
// imageSimilarity computes pixel similarity between two same-sized images.
// Returns [0.0, 1.0] where 1.0 = identical. Used for bottom-of-page detection.
func imageSimilarity(a, b image.Image) float64 {
	ba, bb := a.Bounds(), b.Bounds()
	w, h := ba.Dx(), ba.Dy()
	if w != bb.Dx() || h != bb.Dy() || w == 0 || h == 0 {
		return 0
	}
	var totalDiff float64
	count := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r1, g1, b1, _ := a.At(ba.Min.X+x, ba.Min.Y+y).RGBA()
			r2, g2, b2, _ := b.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			dr := float64(int(r1>>8) - int(r2>>8))
			dg := float64(int(g1>>8) - int(g2>>8))
			db := float64(int(b1>>8) - int(b2>>8))
			totalDiff += dr*dr + dg*dg + db*db
			count++
		}
	}
	if count == 0 {
		return 1.0
	}
	// 每像素最大差值: 255² × 3 = 195075
	// Max diff per pixel: 255² × 3 = 195075
	return 1.0 - totalDiff/float64(count)/195075.0
}

// ---------- attach 读取 / Attach Reader ----------

// readWeaponFlagsFromAttach 从节点 attach 的第一层 key 读取武器开关。
// readWeaponFlagsFromAttach reads weapon flags from attach top-level keys.
func readWeaponFlagsFromAttach(ctx *maa.Context, nodeName string) map[string]string {
	nodeJSON, err := ctx.GetNodeJSON(nodeName)
	if err != nil || nodeJSON == "" {
		essLog.Warn().Err(err).Str("node", nodeName).Msg("failed to get node JSON")
		return nil
	}
	var parsed struct {
		Attach map[string]string `json:"attach"`
	}
	if err := json.Unmarshal([]byte(nodeJSON), &parsed); err != nil {
		essLog.Warn().Err(err).Msg("failed to parse attach")
		return nil
	}
	essLog.Info().Int("totalFlags", len(parsed.Attach)).Msg("weapon flags from attach")
	return parsed.Attach
}

// ---------- UI 提示 / UI Tips ----------

// showSelectedWeaponTips 在 GUI 显示选中武器及所需词条。
// showSelectedWeaponTips displays selected weapons and required attributes in GUI.
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
// showMessage displays text in GUI via a lightweight pipeline task.
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
