package itemtransfer

import (
	"encoding/json"
	"fmt"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// ItemTransferCacheLowConfidenceRecognition 对 0.9～0.97 的模板候选执行 tooltip OCR 校验。
// 高于或等于 0.97 的候选由前置原生 TemplateMatch 节点直接处理。
type ItemTransferCacheLowConfidenceRecognition struct{}

var _ maa.CustomRecognitionRunner = &ItemTransferCacheLowConfidenceRecognition{}

func (r *ItemTransferCacheLowConfidenceRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	param, err := parseItemCacheLowConfidenceParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("invalid low-confidence item cache recognition parameters")
		return nil, false
	}

	detail, err := ctx.RunRecognition(param.CacheNode, arg.Img, map[string]any{
		param.CacheNode: map[string]any{
			"enabled":   true,
			"method":    itemCacheMatchMethod,
			"threshold": itemCacheVerifyThreshold,
			"order_by":  "Score",
		},
	})
	if err != nil {
		log.Warn().Err(err).
			Str("component", componentName).
			Str("cache_node", param.CacheNode).
			Msg("low-confidence item cache TemplateMatch failed")
		return nil, false
	}

	score, box, ok := bestItemCacheTemplateResult(detail)
	if !ok || classifyItemCacheScore(score) != itemCacheScoreVerifyOCR {
		return nil, false
	}

	tasker := ctx.GetTasker()
	ctrl := tasker.GetController()
	centerX := box.X() + box.Width()/2
	centerY := box.Y() + box.Height()/2
	names := hoverAndOCR(ctx, tasker, ctrl, centerX, centerY)
	accepted := acceptLowConfidenceCacheMatch(score, names, param.ItemName)
	log.Info().
		Str("component", componentName).
		Str("item_name", param.ItemName).
		Str("side", param.Side).
		Str("cache_node", param.CacheNode).
		Float64("cache_score", score).
		Strs("ocr_names", names).
		Bool("ocr_verified", accepted).
		Ints("box", []int{box.X(), box.Y(), box.Width(), box.Height()}).
		Msg("low-confidence item cache candidate verified by OCR")
	if !accepted {
		moveMouseSafe(ctrl)
		return nil, false
	}

	detailJSON, _ := json.Marshal(map[string]any{
		"score":        score,
		"ocr_names":    names,
		"ocr_verified": true,
	})
	return &maa.CustomRecognitionResult{
		Box:    box,
		Detail: string(detailJSON),
	}, true
}

// ItemTransferCacheVerifiedAction 在低置信度候选通过 OCR 后刷新同侧模板。
// 缓存失败只影响性能，后续 Pipeline 仍会继续执行本次搬运。
type ItemTransferCacheVerifiedAction struct{}

var _ maa.CustomActionRunner = &ItemTransferCacheVerifiedAction{}

func (a *ItemTransferCacheVerifiedAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil {
		return false
	}
	param, err := parseItemCacheLowConfidenceParam(arg.CustomActionParam)
	if err != nil {
		log.Error().Err(err).Str("component", componentName).Msg("invalid verified item cache action parameters")
		return false
	}
	centerX := arg.Box.X() + arg.Box.Width()/2
	centerY := arg.Box.Y() + arg.Box.Height()/2
	ctrl := ctx.GetTasker().GetController()
	if err := cacheOCRMatchedItem(ctx, ctrl, param.ItemName, param.Side, centerX, centerY); err != nil {
		log.Warn().Err(err).
			Str("component", componentName).
			Str("item_name", param.ItemName).
			Str("side", param.Side).
			Msg("failed to refresh OCR-verified item cache; continuing transfer")
	}
	return true
}

func parseItemCacheLowConfidenceParam(raw string) (itemCacheLowConfidenceParam, error) {
	var param itemCacheLowConfidenceParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return itemCacheLowConfidenceParam{}, fmt.Errorf("parse low-confidence item cache parameters: %w", err)
	}
	if param.ItemName == "" {
		return itemCacheLowConfidenceParam{}, fmt.Errorf("item_name is required")
	}
	switch param.Side {
	case "repo":
		if param.CacheNode != itemCacheRepoNode {
			return itemCacheLowConfidenceParam{}, fmt.Errorf("repo side requires cache node %q", itemCacheRepoNode)
		}
	case "bag":
		if param.CacheNode != itemCacheBagNode && param.CacheNode != itemCacheBagReturnNode {
			return itemCacheLowConfidenceParam{}, fmt.Errorf("bag side requires a bag cache node")
		}
	default:
		return itemCacheLowConfidenceParam{}, fmt.Errorf("unsupported side %q", param.Side)
	}
	return param, nil
}

func acceptLowConfidenceCacheMatch(score float64, names []string, itemName string) bool {
	return classifyItemCacheScore(score) == itemCacheScoreVerifyOCR && matchesAnyTarget(names, itemName)
}
