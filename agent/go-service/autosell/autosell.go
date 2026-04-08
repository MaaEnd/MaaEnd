package autosell

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var scannedItemNameList []string

type AutoSellPriceCompareRecognition struct{}

func (r *AutoSellPriceCompareRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}

	var params struct {
		LowestPrice int `json:"lowest_price"`
	}

	if paramsErr := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); paramsErr != nil {
		log.Error().Err(paramsErr).Str("component", "autosell").Str("step", "price_compare").Msg("parse params")
		return nil, false
	}
	lowestPrice := params.LowestPrice

	detail, recoErr := ctx.RunRecognition("AutoSellFriendsPriceRecognition", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Str("component", "autosell").Str("step", "price_compare").Msg("run recognition")
		return nil, false
	}

	if !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 2 {
		log.Warn().Str("component", "autosell").Str("step", "price_compare").Msg("recognition miss")
		return nil, false
	}

	var detailJson struct {
		Best struct {
			Text string `json:"text"`
		} `json:"best"`
	}
	// Results.Best是空，暂时只能这样获取
	if detailJsonErr := json.Unmarshal([]byte(detail.CombinedResult[1].DetailJson), &detailJson); detailJsonErr != nil {
		log.Error().Err(detailJsonErr).Str("component", "autosell").Str("step", "price_compare").Msg("parse detail json")
		return nil, false
	}

	ocrPrice, atoiErr := strconv.Atoi(detailJson.Best.Text)
	if atoiErr != nil {
		log.Error().Err(atoiErr).Str("component", "autosell").Str("step", "price_compare").Str("raw_text", detailJson.Best.Text).Msg("parse ocr price")
		return nil, false
	}

	log.Info().Str("component", "autosell").Str("step", "price_compare").Int("ocr_price", ocrPrice).Int("lowest_price", lowestPrice).Msg("price compare")
	if ocrPrice < lowestPrice {
		maafocus.Print(ctx, i18n.T("autosell.price_compare_fail", ocrPrice, lowestPrice))
		return nil, false
	}

	maafocus.Print(ctx, i18n.T("autosell.price_compare_ok", ocrPrice, lowestPrice))
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type AutoSellItemRecordAction struct{}

func (a *AutoSellItemRecordAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var params struct {
		RecordType string `json:"record_type"`
		ItemName   string `json:"item_name"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
		log.Error().
			Err(err).
			Str("component", "autosell").Str("step", "item_record").
			Msg("parse params")
		return false
	}

	switch params.RecordType {
	case "init":
		scannedItemNameList = []string{}
		log.Info().Str("component", "autosell").Str("step", "item_record").Msg("init scan list")
	case "record":
		if slices.Contains(scannedItemNameList, params.ItemName) {
			log.Info().Str("component", "autosell").Str("step", "item_record").Str("item_name", params.ItemName).Msg("item already scanned")
			return true
		}
		scannedItemNameList = append(scannedItemNameList, params.ItemName)
		log.Info().Str("component", "autosell").Str("step", "item_record").Str("item_name", params.ItemName).Msg("record item")
	}
	return true
}

type scanItem struct {
	Box  []int  `json:"box"`
	Text string `json:"text"`
}

type AutoSellStockRedistributionOpenItemTextRecognition struct{}

func (r *AutoSellStockRedistributionOpenItemTextRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}
	detail, recoErr := ctx.RunRecognition("AutoSellStockRedistributionOpenItemText", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Str("component", "autosell").Str("step", "scan_item_text").Msg("run recognition")
		return nil, false
	}

	if !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 3 {
		log.Warn().Str("component", "autosell").Str("step", "scan_item_text").Msg("recognition miss")
		return nil, false
	}

	var detailJson struct {
		Filtered []struct {
			Box   []int   `json:"box"`
			Score float64 `json:"score"`
			Text  string  `json:"text"`
		} `json:"filtered"`
	}
	// Results.Best是空，暂时只能这样获取
	if detailJsonErr := json.Unmarshal([]byte(detail.CombinedResult[2].DetailJson), &detailJson); detailJsonErr != nil {
		log.Error().Err(detailJsonErr).Str("component", "autosell").Str("step", "scan_item_text").Msg("parse detail json")
		return nil, false
	}

	var resultItem scanItem

	for _, item := range detailJson.Filtered {
		if slices.Contains(scannedItemNameList, item.Text) {
			log.Info().Str("component", "autosell").Str("step", "scan_item_text").Str("item_name", item.Text).Msg("item already scanned")
			continue
		}
		resultItem.Box = item.Box
		resultItem.Text = item.Text
		break
	}

	if len(resultItem.Text) == 0 {
		log.Info().Str("component", "autosell").Str("step", "scan_item_text").Msg("no new item")
		return nil, false
	}

	resultJson, marshalErr := json.Marshal(resultItem)
	if marshalErr != nil {
		log.Error().Err(marshalErr).Str("component", "autosell").Str("step", "scan_item_text").Msg("marshal result")
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(resultJson),
	}, true
}

// firstContainedKeyword 按 subs 顺序返回首个被 s 包含的关键词，无匹配则返回空串。
func firstContainedKeyword(s string, subs []string) string {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return sub
		}
	}
	return ""
}

var (
	moderatePriceKeywords = []string{"锚点", "悬空", "巫术", "天使", "岳硏", "冬虫", "武陵", "武侠"}
	largePriceKeywords    = []string{"谷地水", "团结", "塞什", "星体"}
	massivePriceKeywords  = []string{"源石", "警戒", "硬脑", "边角"}
)

type AutoSellStockRedistributionOpenItemTextAction struct{}

func (a *AutoSellStockRedistributionOpenItemTextAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	customResult, ok := arg.RecognitionDetail.Results.Best.AsCustom()
	if !ok {
		log.Error().Str("component", "autosell").Str("step", "open_item_text").Msg("get custom result")
		return false
	}
	var resultItem scanItem
	if err := json.Unmarshal([]byte(customResult.Detail), &resultItem); err != nil {
		log.Error().
			Err(err).
			Str("component", "autosell").Str("step", "open_item_text").
			Msg("parse custom result")
		return false
	}

	var param struct {
		ModeratePrice int `json:"moderate_price"`
		LargePrice    int `json:"large_price"`
		MassivePrice  int `json:"massive_price"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().
			Err(err).
			Str("component", "autosell").Str("step", "open_item_text").
			Msg("parse params")
		return false
	}

	// 翻译有缘再写
	targetPrice := 4600
	targetName := "unknown"
	if k := firstContainedKeyword(resultItem.Text, moderatePriceKeywords); k != "" {
		targetPrice = param.ModeratePrice
		targetName = k
		maafocus.Print(ctx, i18n.T("autosell.check_item_price_moderate", resultItem.Text))
	} else if k := firstContainedKeyword(resultItem.Text, largePriceKeywords); k != "" {
		targetPrice = param.LargePrice
		targetName = k
		maafocus.Print(ctx, i18n.T("autosell.check_item_price_large", resultItem.Text))
	} else if k := firstContainedKeyword(resultItem.Text, massivePriceKeywords); k != "" {
		targetPrice = param.MassivePrice
		targetName = k
		maafocus.Print(ctx, i18n.T("autosell.check_item_price_massive", resultItem.Text))
	} else {
		log.Warn().
			Str("component", "autosell").
			Str("step", "open_item_text").
			Str("item_name", resultItem.Text).
			Msg("unknown item, default price")
		maafocus.Print(ctx, i18n.T("autosell.check_item_price_unknown", resultItem.Text))
	}

	if len(resultItem.Box) != 4 {
		log.Error().Str("component", "autosell").Str("step", "open_item_text").Msg("invalid bbox")
		return false
	}

	override := map[string]any{
		"AutoSellStockRedistributionItemOpen": map[string]any{
			"target": maa.Rect{
				resultItem.Box[0],
				resultItem.Box[1],
				resultItem.Box[2],
				resultItem.Box[3],
			},
		},
		"AutoSellStockRedistributionItemTicketByText": map[string]any{
			"roi": maa.Rect{
				resultItem.Box[0],
				resultItem.Box[1],
				resultItem.Box[2],
				resultItem.Box[3],
			},
		},
		"AutoSellStockRedistributionItemCountEmpty": map[string]any{
			"custom_action_param": map[string]any{
				"item_name":   resultItem.Text,
				"record_type": "record",
			},
		},
		"AutoSellSellGoodsEmpty": map[string]any{
			"custom_action_param": map[string]any{
				"item_name":   resultItem.Text,
				"record_type": "record",
			},
		},
		"AutoSellFriendsPricesUnExpected": map[string]any{
			"custom_action_param": map[string]any{
				"item_name":   resultItem.Text,
				"record_type": "record",
			},
		},
		"AutoSellFriendsPricesExpectedBuyRecord": map[string]any{
			"custom_action_param": map[string]any{
				"item_name":   resultItem.Text,
				"record_type": "record",
			},
		},
		"AutoSellFriendsPricesExpected": map[string]any{
			"custom_recognition_param": map[string]any{
				"lowest_price": targetPrice,
			},
		},
		"AutoSellStockRedistributionItemFindTextRecognition": map[string]any{
			"expected": targetName,
		},
	}

	_, err := ctx.RunTask("AutoSellStockRedistributionItemOpenPrepare", override)
	if err != nil {
		log.Error().Err(err).Str("component", "autosell").Str("step", "open_item_text").Msg("run task")
		return false
	}
	return true
}

// Compile-time interface checks
var (
	_ maa.CustomRecognitionRunner = (*AutoSellPriceCompareRecognition)(nil)
	_ maa.CustomActionRunner      = (*AutoSellItemRecordAction)(nil)
	_ maa.CustomRecognitionRunner = (*AutoSellStockRedistributionOpenItemTextRecognition)(nil)
	_ maa.CustomActionRunner      = (*AutoSellStockRedistributionOpenItemTextAction)(nil)
)
