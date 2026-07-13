package itemtransfer

import (
	"fmt"
	"image"
	"time"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/common/runtimeimagecache"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	itemCacheModule                     = "ItemTransfer"
	itemCacheCropWidth                  = 52
	itemCacheCropHeight                 = 50
	itemCacheStableTime                 = 400 * time.Millisecond
	itemCacheStableTimeout              = 5 * time.Second
	itemCacheMatchMethod                = 5
	itemCacheVerifyThreshold            = 0.9
	itemCacheDirectThreshold            = 0.97
	itemCacheRepoNode                   = "ItemTransferFindCachedItemInRepo"
	itemCacheBagNode                    = "ItemTransferFindCachedItemInBag"
	itemCacheBagReturnNode              = "ItemTransferFindCachedItemInBagReturn"
	itemCacheRepoLowConfidenceNode      = "ItemTransferFindCachedItemInRepoLowConfidence"
	itemCacheBagLowConfidenceNode       = "ItemTransferFindCachedItemInBagLowConfidence"
	itemCacheBagReturnLowConfidenceNode = "ItemTransferFindCachedItemInBagReturnLowConfidence"
)

var itemCacheROIOffset = [4]int{0, 0, 0, -10}

var (
	itemCacheRepoROI = maa.Rect{158, 203, 551, 292}
	itemCacheBagROI  = maa.Rect{768, 209, 349, 285}
)

func itemCacheKey(itemName, side string) (string, error) {
	if side != "repo" && side != "bag" {
		return "", fmt.Errorf("unsupported item cache side %q", side)
	}
	return "Items/" + runtimeimagecache.EscapeKeyComponent(itemName) + "/" + side + ".png", nil
}

func itemCacheSourceRect(centerX, centerY int) image.Rectangle {
	left := centerX - itemCacheCropWidth/2
	top := centerY - itemCacheCropHeight/2
	return image.Rect(left, top, left+itemCacheCropWidth, top+itemCacheCropHeight)
}

type itemCacheLowConfidenceParam struct {
	ItemName  string `json:"item_name"`
	Side      string `json:"side"`
	CacheNode string `json:"cache_node"`
}

type itemCacheScoreDecision uint8

const (
	itemCacheScoreMiss itemCacheScoreDecision = iota
	itemCacheScoreVerifyOCR
	itemCacheScoreDirect
)

func classifyItemCacheScore(score float64) itemCacheScoreDecision {
	switch {
	case score >= itemCacheDirectThreshold:
		return itemCacheScoreDirect
	case score >= itemCacheVerifyThreshold:
		return itemCacheScoreVerifyOCR
	default:
		return itemCacheScoreMiss
	}
}

func itemCacheNodePatch(imageName, itemName, side string) (map[string]map[string]any, error) {
	type cacheNodePair struct {
		cacheNode         string
		lowConfidenceNode string
	}
	var cacheNodes []cacheNodePair
	switch side {
	case "repo":
		cacheNodes = []cacheNodePair{
			{itemCacheRepoNode, itemCacheRepoLowConfidenceNode},
		}
	case "bag":
		cacheNodes = []cacheNodePair{
			{itemCacheBagNode, itemCacheBagLowConfidenceNode},
			{itemCacheBagReturnNode, itemCacheBagReturnLowConfidenceNode},
		}
	default:
		return nil, fmt.Errorf("unsupported item cache side %q", side)
	}

	patch := make(map[string]map[string]any, len(cacheNodes)*2)
	for _, pair := range cacheNodes {
		patch[pair.cacheNode] = map[string]any{
			"enabled":   true,
			"template":  imageName,
			"method":    itemCacheMatchMethod,
			"threshold": itemCacheDirectThreshold,
			"order_by":  "Score",
		}
		param := itemCacheLowConfidenceParam{
			ItemName:  itemName,
			Side:      side,
			CacheNode: pair.cacheNode,
		}
		patch[pair.lowConfidenceNode] = map[string]any{
			"enabled": true,
			"recognition": map[string]any{
				"param": map[string]any{
					"custom_recognition_param": param,
				},
			},
			"action": map[string]any{
				"param": map[string]any{
					"custom_action_param": param,
				},
			},
		}
	}
	return patch, nil
}

func runCacheBeforeTransfer(cache func() error, transfer func() bool) bool {
	_ = cache()
	return transfer()
}

func itemCacheWaitConfig(side string) (time.Duration, *maa.WaitFreezesParam, error) {
	var roi maa.Rect
	switch side {
	case "repo":
		roi = itemCacheRepoROI
	case "bag":
		roi = itemCacheBagROI
	default:
		return 0, nil, fmt.Errorf("unsupported item cache side %q", side)
	}
	return itemCacheStableTime, &maa.WaitFreezesParam{
		Target:    maa.NewTargetRect(roi),
		Threshold: 0.95,
		Method:    5,
		RateLimit: 100 * time.Millisecond,
		Timeout:   itemCacheStableTimeout,
	}, nil
}

func cacheAndCtrlClick(ctx *maa.Context, ctrl *maa.Controller, itemName, side string, centerX, centerY int) bool {
	return runCacheBeforeTransfer(
		func() error {
			err := cacheOCRMatchedItem(ctx, ctrl, itemName, side, centerX, centerY)
			if err != nil {
				log.Warn().
					Err(err).
					Str("component", componentName).
					Str("item_name", itemName).
					Str("side", side).
					Int("x", centerX).
					Int("y", centerY).
					Msg("failed to cache OCR matched item; continuing transfer")
			}
			return err
		},
		func() bool {
			return ctrlClick(ctrl, centerX, centerY)
		},
	)
}

// cacheOCRMatchedItem 在 tooltip OCR 命中后获取无污染截图并建立当前 Context 的物品模板。
// 缓存仅是性能优化，调用方应在返回错误时继续执行本次搬运。
func cacheOCRMatchedItem(ctx *maa.Context, ctrl *maa.Controller, itemName, side string, centerX, centerY int) error {
	moveMouseSafe(ctrl)
	stableTime, waitParam, err := itemCacheWaitConfig(side)
	if err != nil {
		return err
	}
	if err := ctx.WaitFreezes(stableTime, nil, waitParam); err != nil {
		return fmt.Errorf("wait for clean item image: %w", err)
	}

	ctrl.PostScreencap().Wait()
	img, err := ctrl.CacheImage()
	if err != nil {
		return fmt.Errorf("get clean item screenshot: %w", err)
	}
	key, err := itemCacheKey(itemName, side)
	if err != nil {
		return err
	}
	entry, err := runtimeimagecache.Store(
		itemCacheModule,
		key,
		img,
		itemCacheSourceRect(centerX, centerY),
		itemCacheROIOffset,
		ctx.OverrideImage,
	)
	if err != nil {
		return fmt.Errorf("store item cache: %w", err)
	}
	patch, err := itemCacheNodePatch(entry.ImageName, itemName, side)
	if err != nil {
		return fmt.Errorf("build item cache node patch: %w", err)
	}
	if err := ctx.OverridePipeline(patch); err != nil {
		return fmt.Errorf("enable item cache nodes: %w", err)
	}

	log.Info().
		Str("component", componentName).
		Str("item_name", itemName).
		Str("side", side).
		Str("image_name", entry.ImageName).
		Ints("roi", []int{entry.Rect.Min.X, entry.Rect.Min.Y, entry.Rect.Dx(), entry.Rect.Dy()}).
		Msg("OCR matched item image cached in context")
	return nil
}
