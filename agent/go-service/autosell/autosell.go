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
					"expression":                          "{AutoSellFriendsPriceRecognition} >= " + strconv.Itoa(targetPrice),
					"focus_matched_resolved_expression":   true,
					"focus_unmatched_resolved_expression": true,
				},
			},
			"AutoSellFriendsPricesExpectedBuy": map[string]any{
				"custom_recognition_param": map[string]any{
					"expression": "{AutoSellFriendsPriceCurrentRecognition} >= " + strconv.Itoa(targetPrice),
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
		if !detail.Status.Success() {
			hasError = true
			break
		}
	}
	if hasError {
		return false
	}

	return true
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

// flattenKeywordGroups 把「同一物资的不同语言/措辞变体」分组列表展开成扁平的关键词清单，
// 提供给 firstContainedKeyword 做 strings.Contains 匹配。
func flattenKeywordGroups(groups [][]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// 每一组是同一件物资的所有已知语言/措辞变体（简体 / 繁体 / 混写形 / EN / JP / KR）。
// 中文部分已对照 EndfieldData ItemTable（type=56 domainshop_cargo）与繁体客户端实际
// 物资全名核实；EN/JP/KR 由用户对照游戏内实际显示文字提供，未经二次验证的部分见各组注释。
// 多数是简繁转字，但也有部分繁体客户端用完全不同的词，不是简繁转换函式能推算出来的，见各组注释。
var (
	moderatePriceKeywordGroups = [][]string{
		{"锚点", "錨點", "锚點", "Ankhorilling Kitchenware", "アンカー製調理器具", "앵커 주방 도구"}, // "锚點" 是繁体客户端实际出现的混写形（简体"锚"+繁体"點"），t2s/s2t 整词转换都对不上
		{"悬空", "懸空", "Musbeast Scrimshaw Dangles", "浮かぶ骨彫刻", "공중 무스비스트 뼈 조각상"},
		{"巫术", "巫術", "Witchcraft Mining Drill", "巫術ドリル", "주술 광산 다이아"},
		{"天使", "Aggeloi War Tins", "アンゲロスの缶詰", "아겔로스 통조림"},
		{"岳研", "Eureka Anti-smog Tincture", "岳研瘴気除け茶", "악연 피장차"}, // 繁体客户端仍写"岳研"，未简化为"嶽研"，故不加"嶽研"
		{"冬虫", "冬蟲", "Nymphsprout", "冬虫夏筍", "동충하순"},
		{"武陵", "Wuling Frozen Pears", "武陵冷凍梨", "무릉 얼린 배"},
		{"武侠", "武俠", "Wuxia Movies", "武侠映画", "무협 영화"},
	}
	largePriceKeywordGroups = [][]string{
		{"谷地水", "Valley Hydroculture Fillets", "谷地水培養肉", "협곡 수경 배양육"},
		{"团结", "團結", "Unity Syrup", "ユナイト製のシロップ剤", "단결 브랜드 물약"},
		{"塞什", "Seš'qamam Knucklebones", "セシュカ製のアシュク", "세쉬카 동물 뼈"},
		{"星体", "星體", "Astarron Crystals", "アスタロン結晶", "별체 결정 조각"},
		// 「天师龙泡泡」与「天师桩机芯」共用「天师」前缀，分别用「天师龙/天师桩」区分，避免 FindText 串货。
		{"天师龙", "天師龍", "Chubby Lung Tianshi", "天師龍泡泡", "천사 드래곤 버블"},
		{"天师桩", "天師樁"}, // 新物资，暂无 EN/JP/KR 资料
		{"息壤净", "息壤淨", "Xiranite Filter Cores", "息壌浄水カートリッジ", "식양 정수 필터"},
		{"息壤色", "Xiran-Hue Fireworks", "息壌色の花火", "식양색을 띤 폭죽"},
		{"息壤桥", "息壤橋", "Xiranite Bridge", "息壌橋", "식양 다리"},
		{"清波", "Qingbo Rafts", "清波いかだ", "청파벌"},
		{"飞天", "飛天", "Aerial Receptionists", "空飛ぶ迎賓係", "비행 안내원"},
		{"选剑", "選劍", "Swordmancer Forge", "選剣鋳造炉", "선검 용광로"},
		{"界石"},  // 新物资，暂无其他语言资料
		{"浮空艇"}, // 新物资，暂无其他语言资料
	}
	massivePriceKeywordGroups = [][]string{
		{"源石", "Originium Saplings", "源石の枝の苗", "작은 오리지늄 나무"},
		{"警戒", "Vigilant Pickaxes", "警戒者のツルハシ", "경계자의 곡괭이"},
		{"硬脑", "硬頭殼", "Hard Noggin Helmets", "石頭ヘルメット", "단단한 헬멧"}, // 繁体客户端实际叫"硬頭殼"，不是简繁转换出的"硬腦"
		{"边角", "碎料", "Scrap Toy Blocks", "端材ブロック", "재활용 블록"},      // 繁体客户端实际叫"碎料"，不是简繁转换出的"邊角"
	}
)

var (
	moderatePriceKeywords = flattenKeywordGroups(moderatePriceKeywordGroups)
	largePriceKeywords    = flattenKeywordGroups(largePriceKeywordGroups)
	massivePriceKeywords  = flattenKeywordGroups(massivePriceKeywordGroups)
)

// Compile-time interface checks
var (
	_ maa.CustomRecognitionRunner = (*AutoSellScanItemRecognition)(nil)
	_ maa.CustomActionRunner      = (*AutoSellItemExecuteItemTaskAction)(nil)
)
