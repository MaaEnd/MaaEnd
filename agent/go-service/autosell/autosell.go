package autosell

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	regionItemMap = make(map[string][]string)
)

type AutoSellScanItemRecognition struct{}

func (r *AutoSellScanItemRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if arg == nil || arg.Img == nil {
		return nil, false
	}

	var params struct {
		Region string `json:"region"`
	}
	if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
		log.Error().Err(err).Str("component", "autosell").Str("step", "scan_item").Msg("parse params")
		return nil, false
	}
	if params.Region == "" {
		log.Error().Str("component", "autosell").Str("step", "scan_item").Msg("empty region param")
		return nil, false
	}

	detail, recoErr := ctx.RunRecognition("AutoSellStockRedistributionItemText", arg.Img)
	if recoErr != nil || detail == nil {
		log.Error().Err(recoErr).Str("component", "autosell").Str("step", "scan_item").Msg("run recognition")
		return nil, false
	}
	if !detail.Hit {
		log.Warn().Str("component", "autosell").Str("step", "scan_item").Msg("recognition not hit")
		return nil, false
	}
	if len(detail.CombinedResult) < 6 {
		log.Warn().Str("component", "autosell").Str("step", "scan_item").Msg("recognition miss")
		return nil, false
	}

	var detailJson struct {
		Filtered []struct {
			Score float64 `json:"score"`
			Text  string  `json:"text"`
		} `json:"filtered"`
	}
	// Results.Best是空，暂时只能这样获取
	if err := json.Unmarshal([]byte(detail.CombinedResult[5].DetailJson), &detailJson); err != nil {
		log.Error().Err(err).Str("component", "autosell").Str("step", "scan_item").Msg("parse detail json")
		return nil, false
	}

	names := make([]string, 0, len(detailJson.Filtered))
	for _, item := range detailJson.Filtered {
		names = append(names, item.Text)
	}
	regionItemMap[params.Region] = names

	log.Info().
		Str("component", "autosell").
		Str("step", "scan_item").
		Str("region", params.Region).
		Int("count", len(names)).
		Strs("items", names).
		Msg("save region items")

	maafocus.Print(ctx, i18n.T("autosell.scan_item_owned", strings.Join(names, i18n.Separator())))

	return &maa.CustomRecognitionResult{
		Box:    arg.Roi,
		Detail: `{"custom": "fake result"}`,
	}, true
}

type AutoSellItemExecuteItemTaskAction struct{}

func (a *AutoSellItemExecuteItemTaskAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	var param struct {
		Region        string `json:"region"`
		ModeratePrice int    `json:"moderate_price"`
		LargePrice    int    `json:"large_price"`
		MassivePrice  int    `json:"massive_price"`
	}
	if err := json.Unmarshal([]byte(arg.CustomActionParam), &param); err != nil {
		log.Error().Err(err).Str("component", "autosell").Str("step", "execute_sell").Msg("parse params")
		return false
	}
	if param.Region == "" {
		log.Error().Str("component", "autosell").Str("step", "execute_sell").Msg("empty region param")
		return false
	}

	names, ok := regionItemMap[param.Region]
	if !ok {
		log.Warn().Str("component", "autosell").Str("step", "execute_sell").Str("region", param.Region).Msg("no scanned items for region")
		return true
	}

	hasError := false
	for _, name := range names {
		// 翻译有缘再写
		targetPrice := 9999
		targetName := "unknown"
		if k := firstContainedKeyword(name, moderatePriceKeywords); k != "" {
			targetPrice = param.ModeratePrice
			targetName = k
			maafocus.Print(ctx, i18n.T("autosell.check_item_price_moderate", name))
		} else if k := firstContainedKeyword(name, largePriceKeywords); k != "" {
			targetPrice = param.LargePrice
			targetName = k
			maafocus.Print(ctx, i18n.T("autosell.check_item_price_large", name))
		} else if k := firstContainedKeyword(name, massivePriceKeywords); k != "" {
			targetPrice = param.MassivePrice
			targetName = k
			maafocus.Print(ctx, i18n.T("autosell.check_item_price_massive", name))
		} else {
			log.Warn().
				Str("component", "autosell").
				Str("step", "execute_sell").
				Str("item_name", name).
				Msg("unknown item, default price")
			maafocus.Print(ctx, i18n.T("autosell.check_item_price_unknown", name))
			continue
		}

		override := map[string]any{
			"AutoSellStockRedistributionItemOpenPrepareRegionalDevelopmentValleyIV": map[string]any{
				"enabled": param.Region == "ValleyIV",
			},
			"AutoSellStockRedistributionItemOpenPrepareRegionalDevelopmentWuling": map[string]any{
				"enabled": param.Region == "Wuling",
			},
			"AutoSellStockRedistributionItemOpenPrepareFriendsSwitchValleyIV": map[string]any{
				"enabled": param.Region == "ValleyIV",
			},
			"AutoSellStockRedistributionItemOpenPrepareFriendsSwitchWuling": map[string]any{
				"enabled": param.Region == "Wuling",
			},
			"AutoSellStockRedistributionItemOpenPrepareFriendsFailedToValleyIV": map[string]any{
				"enabled": param.Region == "ValleyIV",
			},
			"AutoSellStockRedistributionItemOpenPrepareFriendsFailedToWuling": map[string]any{
				"enabled": param.Region == "Wuling",
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

		detail, err := ctx.RunTask("AutoSellStockRedistributionItemOpenPrepare", override)
		if detail == nil || err != nil {
			log.Error().Err(err).Str("component", "autosell").Str("step", "execute_sell").Str("item_name", name).Msg("run prepare task")
			hasError = true
			break
		}
	}
	if hasError {
		return false
	}

	return true
}

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
	moderatePriceKeywords = []string{"锚点", "悬空", "巫术", "天使", "岳研", "冬虫", "武陵", "武侠"}
	largePriceKeywords    = []string{"谷地水", "团结", "塞什", "星体", "天师", "息壤", "清波"}
	massivePriceKeywords  = []string{"源石", "警戒", "硬脑", "边角"}
)

// Compile-time interface checks
var (
	_ maa.CustomRecognitionRunner = (*AutoSellScanItemRecognition)(nil)
	_ maa.CustomRecognitionRunner = (*AutoSellPriceCompareRecognition)(nil)
	_ maa.CustomActionRunner      = (*AutoSellItemExecuteItemTaskAction)(nil)
)
