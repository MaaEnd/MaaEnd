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

var scanedItemNameList []string

type AutoSellPriceCompareRecognition struct{}

func (r *AutoSellPriceCompareRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}

	var params struct {
		LowestPrice int `json:"lowest_price"`
	}

	if paramsErr := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); paramsErr != nil {
		log.Error().Err(paramsErr).Msg("Failed to parse CustomRecognitionParam")
		return nil, false
	}
	lowestPrice := params.LowestPrice

	detail, recoErr := ctx.RunRecognition("AutoSellFriendsPriceRecognition", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Msg("Failed to run AutoSellFriendsPriceRecognition")
		return nil, false
	}

	if !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 2 {
		log.Warn().Msg("AutoSellFriendsPriceRecognition did not hit or returned insufficient results")
		return nil, false
	}

	var detailJson struct {
		Best struct {
			Text string `json:"text"`
		} `json:"best"`
	}
	// Results.Best是空，暂时只能这样获取
	if detailJsonErr := json.Unmarshal([]byte(detail.CombinedResult[1].DetailJson), &detailJson); detailJsonErr != nil {
		log.Error().Err(detailJsonErr).Msg("Failed to parse DetailJson from AutoSellFriendsPriceRecognition")
		return nil, false
	}

	ocrPrice, atoiErr := strconv.Atoi(detailJson.Best.Text)
	if atoiErr != nil {
		log.Error().Err(atoiErr).Msg("Failed to convert OCR result to integer")
		return nil, false
	}

	log.Info().Int("ocrPrice", ocrPrice).Int("lowestPrice", lowestPrice).Msg("OCR price extracted, comparing with lowest price")
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
			Msg("failed to parse CustomActionParam")
		return false
	}

	switch params.RecordType {
	case "init":
		scanedItemNameList = []string{}
		log.Info().Msg("Cleared scanned item name list")
	case "record":
		if slices.Contains(scanedItemNameList, params.ItemName) {
			log.Info().Str("item_name", params.ItemName).Msg("Item name already recorded, skipping")
			return true
		}
		scanedItemNameList = append(scanedItemNameList, params.ItemName)
		log.Info().Str("item_name", params.ItemName).Msg("Recorded scanned item name")
	}
	return true
}

type scanItem struct {
	Box  []int  `json:"box"`
	Text string `json:"text"`
}

type AutoSellStockRedistributionOpenItemTextRecognition struct{}

func (r *AutoSellStockRedistributionOpenItemTextRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {

	detail, recoErr := ctx.RunRecognition("AutoSellStockRedistributionOpenItemText", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Msg("Failed to run AutoSellStockRedistributionOpenItemText")
		return nil, false
	}

	if !detail.Hit || detail.CombinedResult == nil || len(detail.CombinedResult) < 3 {
		log.Warn().Msg("AutoSellStockRedistributionOpenItemText did not hit or returned insufficient results")
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
		log.Error().Err(detailJsonErr).Msg("Failed to parse DetailJson from AutoSellFriendsPriceRecognition")
		return nil, false
	}

	var resultItem scanItem

	for _, item := range detailJson.Filtered {
		if slices.Contains(scanedItemNameList, item.Text) {
			log.Info().Str("item_name", item.Text).Msg("Item name already recorded, skipping")
			continue
		}
		resultItem.Box = item.Box
		resultItem.Text = item.Text
		break
	}

	if len(resultItem.Text) == 0 {
		log.Info().Msg("No new item name found in recognition results")
		return nil, false
	}

	resultJson, marshalErr := json.Marshal(resultItem)
	if marshalErr != nil {
		log.Error().Err(marshalErr).Msg("Failed to marshal result item to JSON")
		return nil, false
	}
	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: string(resultJson),
	}, true
}

type AutoSellStockRedistributionOpenItemTextAction struct{}

func (a *AutoSellStockRedistributionOpenItemTextAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	customResult, ok := arg.RecognitionDetail.Results.Best.AsCustom()
	if !ok {
		log.Error().Msg("Failed to get custom recognition result")
		return false
	}
	var resultItem scanItem
	if err := json.Unmarshal([]byte(customResult.Detail), &resultItem); err != nil {
		log.Error().
			Err(err).
			Msg("failed to parse CustomActionParam")
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
			Msg("failed to parse CustomActionParam")
		return false
	}

	// 翻译有缘再写
	var targetPrice = 4600
	if strings.Contains(resultItem.Text, "锚点") ||
		strings.Contains(resultItem.Text, "悬空") ||
		strings.Contains(resultItem.Text, "巫术") ||
		strings.Contains(resultItem.Text, "天使") ||
		strings.Contains(resultItem.Text, "岳硏") ||
		strings.Contains(resultItem.Text, "冬虫") ||
		strings.Contains(resultItem.Text, "武陵") ||
		strings.Contains(resultItem.Text, "武侠") {
		targetPrice = param.ModeratePrice
		maafocus.Print(ctx, i18n.T("autosell.check_item_price_moderate", resultItem.Text))
	} else if strings.Contains(resultItem.Text, "谷地水") ||
		strings.Contains(resultItem.Text, "团结") ||
		strings.Contains(resultItem.Text, "塞什") ||
		strings.Contains(resultItem.Text, "星体") {
		targetPrice = param.LargePrice
		maafocus.Print(ctx, i18n.T("autosell.check_item_price_large", resultItem.Text))
	} else if strings.Contains(resultItem.Text, "源石") ||
		strings.Contains(resultItem.Text, "警戒") ||
		strings.Contains(resultItem.Text, "硬脑") ||
		strings.Contains(resultItem.Text, "边角") {
		targetPrice = param.MassivePrice
		maafocus.Print(ctx, i18n.T("autosell.check_item_price_massive", resultItem.Text))
	} else {
		log.Warn().Str("item_name", resultItem.Text).Msg("Unrecognized item name, using default target price")
		maafocus.Print(ctx, i18n.T("autosell.check_item_price_unknown", resultItem.Text))
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
	}

	_, err := ctx.RunTask("AutoSellStockRedistributionItemOpen", override)
	if err != nil {
		log.Error().Err(err).Msg("Failed to run AutoSellStockRedistributionItemOpen")
		return false
	}
	return true
}
