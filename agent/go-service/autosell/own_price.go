package autosell

import (
	"encoding/json"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var regionOwnPriceMap = make(map[string]map[string]int)

// AutoSellCacheOwnPriceRecognition 读取本家出售窗口的单价，按地区和物资名称缓存。
type AutoSellCacheOwnPriceRecognition struct{}

type cacheOwnPriceParam struct {
	Region   string `json:"region"`
	ItemName string `json:"item_name"`
}

// Run 仅在价格识别成功且为正数时写入缓存，缺失的缓存由好友阶段按 0 比较。
func (r *AutoSellCacheOwnPriceRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}

	var param cacheOwnPriceParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", "autosell").
			Str("step", "cache_own_price").
			Msg("parse params")
		return nil, false
	}
	if param.Region == "" || strings.TrimSpace(param.ItemName) == "" {
		log.Error().
			Str("component", "autosell").
			Str("step", "cache_own_price").
			Msg("region and item name are required")
		return nil, false
	}

	detail, err := ctx.RunRecognition("AutoSellOwnPriceRecognition", arg.Img)
	if err != nil || detail == nil || !detail.Hit {
		log.Warn().
			Err(err).
			Str("component", "autosell").
			Str("step", "cache_own_price").
			Str("item_name", param.ItemName).
			Msg("own price recognition did not hit")
		return nil, false
	}
	price, err := ocrnum.Extract(detail)
	if err != nil || price <= 0 {
		log.Warn().
			Err(err).
			Str("component", "autosell").
			Str("step", "cache_own_price").
			Str("item_name", param.ItemName).
			Int("price", price).
			Msg("invalid own price")
		return nil, false
	}

	if regionOwnPriceMap[param.Region] == nil {
		regionOwnPriceMap[param.Region] = make(map[string]int)
	}
	regionOwnPriceMap[param.Region][param.ItemName] = price
	log.Info().
		Str("component", "autosell").
		Str("step", "cache_own_price").
		Str("region", param.Region).
		Str("item_name", param.ItemName).
		Int("price", price).
		Msg("cached own item price")
	maafocus.Print(ctx, i18n.T("autosell.own_price_cached", param.ItemName, price))

	data, _ := json.Marshal(map[string]int{"price": price})
	return &maa.CustomRecognitionResult{Box: detail.Box, Detail: string(data)}, true
}

var _ maa.CustomRecognitionRunner = &AutoSellCacheOwnPriceRecognition{}
