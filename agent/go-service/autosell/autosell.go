package autosell

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/i18n"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/maafocus"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/recogtarget"
	"github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

var (
	regionItemMap = make(map[string][]string)

	// 好友价格识别节点：由 ExpressionRecognition 求值时经嵌套识别子任务运行。
	priceRecognitionNode = "AutoSellFriendsPriceRecognition"
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
	soldCount, skippedCount, unknownCount, unconfirmedCount := 0, 0, 0, 0
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
			unknownCount++
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
					"expression":                        "{AutoSellFriendsPriceRecognition} >= " + strconv.Itoa(targetPrice),
					"focus_matched_resolved_expression": true,
					// 未达标时由 autosell 输出带物资名与价格的明确跳过文案，
					// 关闭通用「不匹配」提示，避免被误读为故障。
					"focus_unmatched_resolved_expression": false,
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

		sold, price, lowPrice, err := inspectItemTaskResult(ctx, detail, targetPrice)
		if err != nil {
			// 汇总信息读取失败不改变已执行的售卖流程，也不能推断未成交原因。
			log.Warn().Err(err).
				Str("component", "autosell").
				Str("step", "inspect_item_result").
				Str("item_name", name).
				Int64("task_id", detail.ID).
				Msg("could not determine item sell result")
			unconfirmedCount++
			maafocus.Print(ctx, i18n.T("autosell.item_not_sold", name))
			continue
		}
		if sold {
			soldCount++
			continue
		}
		skippedCount++
		if lowPrice {
			maafocus.Print(ctx, i18n.T("autosell.item_skipped_low_price", name, price, targetPrice))
			log.Info().
				Str("component", "autosell").
				Str("step", "execute_sell").
				Str("item_name", name).
				Int("best_price", price).
				Int("target_price", targetPrice).
				Msg("item skipped, price below minimum")
		} else {
			maafocus.Print(ctx, i18n.T("autosell.item_not_sold", name))
			log.Info().
				Str("component", "autosell").
				Str("step", "execute_sell").
				Str("item_name", name).
				Int("target_price", targetPrice).
				Msg("item sale not confirmed")
		}
	}
	if hasError {
		return false
	}

	if len(names) > 0 {
		regionName := i18n.T("autosell.region_wuling")
		if param.Region == "ValleyIV" {
			regionName = i18n.T("autosell.region_valley_iv")
		}
		if unconfirmedCount > 0 {
			maafocus.Print(ctx, i18n.T("autosell.finish_summary_unconfirmed", regionName, len(names), soldCount, skippedCount, unknownCount, unconfirmedCount))
		} else if unknownCount > 0 {
			maafocus.Print(ctx, i18n.T("autosell.finish_summary_unknown", regionName, len(names), soldCount, skippedCount, unknownCount))
		} else {
			maafocus.Print(ctx, i18n.T("autosell.finish_summary", regionName, len(names), soldCount, skippedCount))
		}
		log.Info().
			Str("component", "autosell").
			Str("step", "execute_sell").
			Str("region", param.Region).
			Int("total", len(names)).
			Int("sold", soldCount).
			Int("skipped", skippedCount).
			Int("unknown", unknownCount).
			Int("unconfirmed", unconfirmedCount).
			Msg("region sell summary")
	}

	return true
}

// inspectItemTaskResult 仅在价格拒绝分支和本次有效低价读数同时成立时确认低价跳过。
// 价格 OCR 属于嵌套识别子任务，不在父任务节点列表中；用父任务首尾节点 ID
// 限定 GetLatestNode 的读数范围，避免引用其他物资或其他任务的缓存价格。
func inspectItemTaskResult(ctx *maa.Context, detail *maa.TaskDetail, targetPrice int) (sold bool, price int, lowPrice bool, err error) {
	if detail == nil {
		return false, 0, false, fmt.Errorf("item task detail is empty")
	}
	nodes := make([]*maa.NodeDetail, 0, len(detail.Nodes))
	for _, ref := range detail.Nodes {
		nd, err := ref.GetDetail()
		if err != nil {
			return false, 0, false, fmt.Errorf("get item task node %d: %w", ref.ID(), err)
		}
		if nd == nil {
			return false, 0, false, fmt.Errorf("item task node %d detail is empty", ref.ID())
		}
		nodes = append(nodes, nd)
	}
	sold, priceRejected := itemTaskOutcome(nodes)
	if sold || !priceRejected {
		return sold, 0, false, nil
	}

	if ctx == nil {
		return false, 0, false, fmt.Errorf("get %s: context is nil", priceRecognitionNode)
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return false, 0, false, fmt.Errorf("get %s: tasker is nil", priceRecognitionNode)
	}
	nd, err := tasker.GetLatestNode(priceRecognitionNode)
	if err != nil {
		return false, 0, false, fmt.Errorf("get latest %s: %w", priceRecognitionNode, err)
	}
	if nd == nil || nd.Recognition == nil || !nd.Recognition.Hit {
		return false, 0, false, fmt.Errorf("%s has no valid price recognition", priceRecognitionNode)
	}
	if nd.ID <= nodes[0].ID || nd.ID >= nodes[len(nodes)-1].ID {
		return false, 0, false, fmt.Errorf("%s node %d is outside item task node range [%d, %d]", priceRecognitionNode, nd.ID, nodes[0].ID, nodes[len(nodes)-1].ID)
	}
	selected, err := recogtarget.SelectDetail(ctx, priceRecognitionNode, nd.Recognition)
	if err != nil {
		return false, 0, false, fmt.Errorf("select %s OCR detail: %w", priceRecognitionNode, err)
	}
	value, err := ocrnum.Extract(selected)
	if err != nil {
		return false, 0, false, fmt.Errorf("extract %s price: %w", priceRecognitionNode, err)
	}
	if value >= targetPrice {
		return false, 0, false, fmt.Errorf("%s price %d is not below minimum %d", priceRecognitionNode, value, targetPrice)
	}
	return false, value, true, nil
}

// itemTaskOutcome 只根据已完成的 Pipeline 节点判断成交和价格拒绝分支。
func itemTaskOutcome(nodes []*maa.NodeDetail) (sold, lowPrice bool) {
	for _, node := range nodes {
		if node == nil || !node.RunCompleted {
			continue
		}
		switch node.Name {
		case "AutoSellFriendsPricesExpectedBuyClick":
			if node.Action != nil && node.Action.Success {
				return true, false
			}
		case "AutoSellFriendsPricesUnExpected":
			lowPrice = true
		case "AutoSellFriendsPricesExpected", "AutoSellSellGoodsEmpty":
			lowPrice = false
		}
	}
	return false, lowPrice
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
// EN/JP/KR 关键词只取「第一个词」或「与中文关键词同义的词」，不用完整物资全名，
// 跟中文关键词的简短风格一致，也避免多个物资共用前缀词（如「Xiranite」）互相误判。
// 跟既有中文关键词完全相同的语言（如日文汉字与简中一致）不重复列出。
var (
	moderatePriceKeywordGroups = [][]string{
		{"锚点", "錨點", "锚點", "Ankhorilling", "アンカー", "앵커"}, // "锚點" 是繁体客户端实际出现的混写形（简体"锚"+繁体"點"），t2s/s2t 整词转换都对不上
		{"悬空", "懸空", "Musbeast", "浮かぶ", "공중"},
		{"巫术", "巫術", "Witchcraft", "주술"},
		{"天使", "Aggeloi", "アンゲロス", "아겔로스"},
		{"岳研", "Eureka", "악연"},
		{"冬虫", "冬蟲", "Nymphsprout", "동충하순"},
		{"武陵", "Wuling", "무릉"},
		{"武侠", "武俠", "Wuxia", "무협"},
	}
	largePriceKeywordGroups = [][]string{
		{"谷地水", "Hydroculture", "협곡 수경"},
		{"团结", "團結", "Unity", "ユナイト", "단결"},
		{"塞什", "Seš'qamam", "セシュカ", "세쉬카"},
		{"星体", "星體", "Astarron", "アスタロン", "별체"},
		// 「天师龙泡泡」与「天师桩机芯」共用「天师」前缀，分别用「天师龙/天师桩」区分，避免 FindText 串货。
		{"天师龙", "天師龍", "Chubby Lung Tianshi", "천사"},
		{"天师桩", "天師樁"}, // 新物资，暂无 EN/JP/KR 资料
		{"息壤净", "息壤淨", "Xiranite Filter", "息壌浄", "식양 정수"},
		{"息壤色", "Xiran-Hue", "息壌色", "식양색을"},
		{"息壤桥", "息壤橋", "息壌橋", "Xiranite Bridge", "식양 다리"},
		{"清波", "Qingbo", "청파"},
		{"飞天", "飛天", "Aerial", "空飛ぶ", "비행"},
		{"选剑", "選劍", "選剣", "Swordmancer", "선검"},
		{"界石"},  // 新物资，暂无其他语言资料
		{"浮空艇"}, // 新物资，暂无其他语言资料
	}
	massivePriceKeywordGroups = [][]string{
		{"源石", "Originium", "작은 오리지늄"},
		{"警戒", "Vigilant", "경계자"},
		{"硬脑", "硬頭殼", "Hard Noggin", "石頭", "단단한"},
		{"边角", "碎料", "Scrap", "端材", "재활용"},
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
