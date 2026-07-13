package itemtransfer

import (
	"encoding/json"
	"image"
	"sort"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const itemCacheRuntimeImagePrefix = "__MaaEndRuntimeImageCacheV1__/ItemTransfer/Items/"

// selectBagRankingTemplateNode 优先选择同侧背包模板；首次进入背包尚无同侧模板时，
// 使用仓库模板为当前页格子排序。模板分数只影响 OCR 顺序，不用于直接搬运。
func selectBagRankingTemplateNode(bagNodeJSON, repoNodeJSON string) string {
	if runtimeItemCacheImageName(bagNodeJSON) != "" {
		return itemCacheBagNode
	}
	if runtimeItemCacheImageName(repoNodeJSON) != "" {
		return itemCacheRepoNode
	}
	return ""
}

func runtimeItemCacheImageName(raw string) string {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ""
	}
	var find func(any) string
	find = func(current any) string {
		switch typed := current.(type) {
		case string:
			if len(typed) >= len(itemCacheRuntimeImagePrefix) && typed[:len(itemCacheRuntimeImagePrefix)] == itemCacheRuntimeImagePrefix {
				return typed
			}
		case []any:
			for _, child := range typed {
				if found := find(child); found != "" {
					return found
				}
			}
		case map[string]any:
			for _, child := range typed {
				if found := find(child); found != "" {
					return found
				}
			}
		}
		return ""
	}
	return find(value)
}

func rankGridItemsByScores(items []gridItem, scores []float64) []gridItem {
	ranked := append([]gridItem(nil), items...)
	if len(ranked) != len(scores) {
		return ranked
	}
	type scoredItem struct {
		item  gridItem
		score float64
	}
	scored := make([]scoredItem, len(ranked))
	for i := range ranked {
		scored[i] = scoredItem{item: ranked[i], score: scores[i]}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	for i := range scored {
		ranked[i] = scored[i].item
	}
	return ranked
}

// itemCacheGridMatchROI 比最终 52×40 模板每边多保留 4 px，允许不同格子的图标存在轻微偏移。
func itemCacheGridMatchROI(item gridItem) [4]int {
	return [4]int{item.CenterX - 30, item.CenterY - 29, 60, 48}
}

func rankBagGridItemsByCache(ctx *maa.Context, img image.Image, items []gridItem, itemName string) []gridItem {
	if ctx == nil || img == nil || len(items) == 0 {
		return append([]gridItem(nil), items...)
	}
	bagNodeJSON, bagErr := ctx.GetNodeJSON(itemCacheBagNode)
	repoNodeJSON, repoErr := ctx.GetNodeJSON(itemCacheRepoNode)
	if bagErr != nil {
		log.Warn().Err(bagErr).
			Str("component", componentName).
			Str("node", itemCacheBagNode).
			Msg("failed to read bag cache node for OCR ranking")
		bagNodeJSON = ""
	}
	if repoErr != nil {
		log.Warn().Err(repoErr).
			Str("component", componentName).
			Str("node", itemCacheRepoNode).
			Msg("failed to read repo cache node for OCR ranking")
		repoNodeJSON = ""
	}
	bagTemplate := runtimeItemCacheImageName(bagNodeJSON)
	repoTemplate := runtimeItemCacheImageName(repoNodeJSON)
	templateNode := selectBagRankingTemplateNode(bagNodeJSON, repoNodeJSON)
	if templateNode == "" {
		log.Info().
			Str("component", componentName).
			Int("grid_count", len(items)).
			Str("bag_template", bagTemplate).
			Str("repo_template", repoTemplate).
			Msg("no runtime cache template available; bag OCR keeps grid order")
		return append([]gridItem(nil), items...)
	}
	templateName := repoTemplate
	if templateNode == itemCacheBagNode {
		templateName = bagTemplate
	}
	log.Info().
		Str("component", componentName).
		Str("template_node", templateNode).
		Str("template_name", templateName).
		Str("bag_template", bagTemplate).
		Str("repo_template", repoTemplate).
		Int("grid_count", len(items)).
		Msg("runtime cache template selected for bag OCR ranking")

	scores := make([]float64, len(items))
	for i, item := range items {
		scores[i] = -1
		roi := itemCacheGridMatchROI(item)
		detail, err := ctx.RunRecognition(templateNode, img, map[string]any{
			templateNode: map[string]any{
				"enabled":   true,
				"roi":       roi,
				"method":    5,
				"threshold": 0,
				"order_by":  "Score",
			},
		})
		if err != nil {
			log.Warn().Err(err).
				Str("component", componentName).
				Str("template_node", templateNode).
				Str("template_name", templateName).
				Int("grid_index", i).
				Int("center_x", item.CenterX).
				Int("center_y", item.CenterY).
				Ints("roi", roi[:]).
				Msg("bag grid template score failed")
			continue
		}
		if score, _, ok := bestItemCacheTemplateResult(detail); ok {
			scores[i] = score
		}
	}

	return rankGridItemsByScores(items, scores)
}

func bestItemCacheTemplateResult(detail *maa.RecognitionDetail) (float64, maa.Rect, bool) {
	if detail == nil || detail.Results == nil || detail.Results.Best == nil {
		return 0, maa.Rect{}, false
	}
	result, ok := detail.Results.Best.AsTemplateMatch()
	if !ok {
		return 0, maa.Rect{}, false
	}
	return result.Score, result.Box, true
}
