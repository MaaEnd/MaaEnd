package autosell

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// SelectItemRecognition 提供一个未处理商品或确认遍历结束，不执行界面操作或子任务。
type SelectItemRecognition struct{}

type selectItemParam struct {
	Result string `json:"result"`
}

var _ maa.CustomRecognitionRunner = (*SelectItemRecognition)(nil)

func (r *SelectItemRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil {
		return nil, false
	}
	var param selectItemParam
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &param); err != nil {
		log.Error().Err(err).Str("component", "AutoSellSelectItem").Msg("invalid params")
		return nil, false
	}
	if param.Result != "record" && param.Result != "sell" && param.Result != "exhausted" {
		log.Error().Str("component", "AutoSellSelectItem").Str("result", param.Result).Msg("invalid result")
		return nil, false
	}

	sessionMu.Lock()
	region := session.Regions[session.Region]
	if region == nil {
		sessionMu.Unlock()
		return nil, false
	}
	if param.Result == "exhausted" {
		exhausted := session.Index >= len(region.Items)
		sessionMu.Unlock()
		if !exhausted {
			return nil, false
		}
		return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: `{"exhausted":true}`}, true
	}

	index := nextItemIndex(region.Items, session.Index, param.Result == "record")
	if index >= len(region.Items) {
		session.Index = index
		sessionMu.Unlock()
		return nil, false
	}
	item := region.Items[index]
	threshold := 0
	if item.Category >= 0 {
		threshold = region.Prices[item.Category]
	}
	if err := ctx.OverridePipeline(itemOverrides(item, threshold)); err != nil {
		sessionMu.Unlock()
		log.Error().Err(err).Str("component", "AutoSellSelectItem").Str("item_name", item.Name).Msg("configure item nodes")
		return nil, false
	}
	session.Index = index
	sessionMu.Unlock()

	if param.Result == "sell" {
		key := []string{"autosell.check_item_price_moderate", "autosell.check_item_price_large", "autosell.check_item_price_massive"}[item.Category]
		maafocus.Print(ctx, i18n.T(key, item.Name))
	}
	detailJSON, _ := json.Marshal(map[string]any{"item_name": item.Name, "own_price": item.OwnPrice, "threshold": threshold})
	return &maa.CustomRecognitionResult{Box: arg.Roi, Detail: string(detailJSON)}, true
}

func nextItemIndex(items []itemState, start int, record bool) int {
	for start < len(items) {
		if !items[start].Sold && (record || items[start].Category >= 0) {
			break
		}
		start++
	}
	return start
}

func itemOverrides(item itemState, threshold int) map[string]any {
	override := map[string]any{
		"AutoSellStockRedistributionItemFindTextRecognition": map[string]any{
			"expected": []string{regexp.QuoteMeta(item.Name)},
		},
	}
	// 只注入单件商品的比较数据，比较分支和完整日志由 Pipeline / ExpressionRecognition 负责。
	for _, comparison := range []struct {
		Node   string
		Source string
		Price  int
	}{
		{"AutoSellFriendsPricesExpected", "AutoSellFriendsPriceRecognition", threshold},
		{"AutoSellFriendsHomePriceExpected", "AutoSellFriendsPriceRecognition", item.OwnPrice},
		{"AutoSellFriendsHomePriceLogOnly", "AutoSellFriendsPriceRecognition", item.OwnPrice},
		{"AutoSellFriendsPricesExpectedBuy", "AutoSellFriendsPriceCurrentRecognition", threshold},
		{"AutoSellCurrentHomePriceExpected", "AutoSellFriendsPriceCurrentRecognition", item.OwnPrice},
		{"AutoSellCurrentHomePriceLogOnly", "AutoSellFriendsPriceCurrentRecognition", item.OwnPrice},
		{"AutoSellOwnPricesExpected", "AutoSellOwnPriceRecognition", threshold},
	} {
		override[comparison.Node] = map[string]any{
			"custom_recognition_param": map[string]any{
				// Custom 参数整体替换，须与表达式一起写入日志开关。
				"expression":                          fmt.Sprintf("{%s} >= %d", comparison.Source, comparison.Price),
				"focus_matched_resolved_expression":   true,
				"focus_unmatched_resolved_expression": true,
			},
		}
	}
	return override
}
