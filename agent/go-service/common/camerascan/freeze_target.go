package camerascan

import (
	"encoding/json"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	// nodeROIMaxDepth 限制沿 And / Or 子节点展开的层数，避免节点自引用时无限递归。
	nodeROIMaxDepth = 4
	// 判定画面是否静止所用的基准分辨率，与 Pipeline ROI 一致。
	baseWidth  = 1280
	baseHeight = 720
)

// freezeTarget 是「等待画面静止」的比较区域，取自 wait_nodes 的识别 ROI。
// fullScreen 表示该节点识别整屏（roi 为 [0,0,0,0]），此时沿用全屏比较。
type freezeTarget struct {
	x, y, w, h int
	fullScreen bool
}

func (t freezeTarget) rect() []int {
	if t.fullScreen {
		return []int{0, 0, 0, 0}
	}
	return []int{t.x, t.y, t.w, t.h}
}

// resolveFreezeTarget 把 wait_nodes 的识别区域并起来，作为等待画面静止的比较区域。
// 单个节点解析不到 ROI 时跳过；全都不行时返回 false，调用方退回节点自身的配置。
func resolveFreezeTarget(ctx *maa.Context, waitNodes []string) (freezeTarget, bool) {
	var merged freezeTarget
	found := false
	for _, node := range waitNodes {
		roi, ok := nodeFreezeROI(ctx, node, 0)
		if !ok {
			continue
		}
		if !found {
			merged, found = roi, true
			continue
		}
		merged = mergeFreezeTarget(merged, roi)
	}
	return merged, found
}

// nodeFreezeROI 读出一个节点的识别区域；And / Or 组合节点取子节点区域的并集。
func nodeFreezeROI(ctx *maa.Context, node string, depth int) (freezeTarget, bool) {
	if depth > nodeROIMaxDepth {
		return freezeTarget{}, false
	}
	raw, err := ctx.GetNodeJSON(node)
	if err != nil {
		log.Warn().Err(err).Str("component", componentName).Str("node", node).
			Msg("freeze target: failed to read node")
		return freezeTarget{}, false
	}

	var parsed struct {
		Recognition struct {
			Param struct {
				ROI   []int    `json:"roi"`
				AllOf []string `json:"all_of"`
				AnyOf []string `json:"any_of"`
			} `json:"param"`
		} `json:"recognition"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		log.Warn().Err(err).Str("component", componentName).Str("node", node).
			Msg("freeze target: failed to parse node")
		return freezeTarget{}, false
	}

	param := parsed.Recognition.Param
	children := make([]string, 0, len(param.AllOf)+len(param.AnyOf))
	children = append(children, param.AllOf...)
	children = append(children, param.AnyOf...)

	var merged freezeTarget
	found := false
	for _, child := range children {
		roi, ok := nodeFreezeROI(ctx, child, depth+1)
		if !ok {
			continue
		}
		if !found {
			merged, found = roi, true
			continue
		}
		merged = mergeFreezeTarget(merged, roi)
	}
	if found {
		return merged, true
	}
	return roiFromList(param.ROI)
}

// roiFromList 把 Pipeline 的 roi 转成比较区域：[x, y, w, h]，[0, 0, 0, 0] 视为整屏。
func roiFromList(roi []int) (freezeTarget, bool) {
	if len(roi) < 4 {
		return freezeTarget{}, false
	}
	x, y, w, h := roi[0], roi[1], roi[2], roi[3]
	if w <= 0 || h <= 0 {
		return freezeTarget{fullScreen: true}, true
	}
	if x < 0 {
		w += x
		x = 0
	}
	if y < 0 {
		h += y
		y = 0
	}
	if x >= baseWidth || y >= baseHeight || w <= 0 || h <= 0 {
		return freezeTarget{}, false
	}
	if x+w > baseWidth {
		w = baseWidth - x
	}
	if y+h > baseHeight {
		h = baseHeight - y
	}
	return freezeTarget{x: x, y: y, w: w, h: h}, true
}

func mergeFreezeTarget(a, b freezeTarget) freezeTarget {
	if a.fullScreen || b.fullScreen {
		return freezeTarget{fullScreen: true}
	}
	x1, y1 := min(a.x, b.x), min(a.y, b.y)
	x2, y2 := max(a.x+a.w, b.x+b.w), max(a.y+a.h, b.y+b.h)
	return freezeTarget{x: x1, y: y1, w: x2 - x1, h: y2 - y1}
}

// freezeOverride 以节点自身的 post_wait_freezes 为模板，只替换比较区域，
// 等多久、阈值多少仍由 Pipeline 决定；节点没配时用内置默认值兜底。
func freezeOverride(ctx *maa.Context, node string, target freezeTarget) map[string]any {
	params := nodeFreezeParams(ctx, node)
	params["target"] = target.rect()
	return map[string]any{node: map[string]any{"post_wait_freezes": params}}
}

func nodeFreezeParams(ctx *maa.Context, node string) map[string]any {
	params := map[string]any{
		"time":       40,
		"timeout":    200,
		"threshold":  0.95,
		"rate_limit": 40,
	}
	raw, err := ctx.GetNodeJSON(node)
	if err != nil {
		return params
	}

	var parsed struct {
		PostWaitFreezes json.RawMessage `json:"post_wait_freezes"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil || len(parsed.PostWaitFreezes) == 0 {
		return params
	}

	object := map[string]any{}
	if err := json.Unmarshal(parsed.PostWaitFreezes, &object); err != nil {
		// 数字写法（post_wait_freezes: 500）只表示等待时间
		var millis float64
		if json.Unmarshal(parsed.PostWaitFreezes, &millis) == nil {
			params["time"] = int(millis)
		}
		return params
	}
	for key, value := range object {
		params[key] = value
	}
	return params
}
