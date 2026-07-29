package goods

import (
	"encoding/json"
	"fmt"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

type priorityItemRecognitionParam struct {
	Location            string `json:"location"`
	Result              string `json:"result"`
	StockNameOffset     []int  `json:"stock_name_offset"`
	StockQuantityOffset []int  `json:"stock_quantity_offset"`
	StockClickOffset    []int  `json:"stock_click_offset"`
	stockCellOffsets    stockCellOffsets
}

// PriorityItemRecognition 在选择货品界面中，按当前优先策略返回下一个未尝试货品。
// exhausted 需要连续两次观察到相同的“只剩已尝试货品”集合，避免单帧 OCR 波动误判结束。
type PriorityItemRecognition struct{}

var _ maa.CustomRecognitionRunner = (*PriorityItemRecognition)(nil)

func (r *PriorityItemRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	param, err := parsePriorityItemRecognitionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", priorityItemRecognitionName).Msg("invalid params")
		return nil, false
	}

	// 静态数据提供当前据点可售物品及其 OCR 名称、稀有度和单价，
	// 会话策略再决定是否只保留用户为当前地区配置的优先物品。
	groupsByLocation, err := loadItemPriorityGroupsFunc()
	if err != nil {
		log.Error().Err(err).Str("component", priorityItemRecognitionName).Msg("failed to load item priorities")
		return nil, false
	}
	policy := priorityPolicySnapshot()
	allGroups := groupsByLocation[param.Location]
	groups := prioritizeItemGroups(allGroups, policy.Preferred, policy.OnlyPreferred)
	if len(groups) == 0 {
		// 严格优先模式允许当前据点没有适用配置，但仍需连续两帧确认后再结束。
		if policy.OnlyPreferred && param.Result == priorityResultExhausted {
			return buildPriorityExhaustedResult(param.Location, nil)
		}
		if policy.OnlyPreferred {
			return nil, false
		}
		log.Error().Str("component", priorityItemRecognitionName).Str("location", param.Location).
			Msg("item priority list is empty")
		return nil, false
	}
	if param.Result == priorityResultExhausted {
		// select 分支先保存本帧识别集合；exhausted 分支只负责确认该集合是否连续稳定。
		itemIDs := goodsSelectionExhaustedItemsSnapshot(param.Location)
		if len(itemIDs) == 0 {
			return nil, false
		}
		return buildPriorityExhaustedResult(param.Location, itemIDs)
	}

	// 每轮 select 都重新扫描第一页。完整格子读取名称和库存，底部半截格子只保留名称；
	// 稀有度和单价策略可以选择后者，库存策略会将其排除。
	beginGoodsSelection(param.Location)
	page, err := recognizeStockPage(ctx, arg, allGroups, param.stockCellOffsets)
	if err != nil {
		log.Warn().Err(err).
			Str("component", priorityItemRecognitionName).
			Str("location", param.Location).
			Str("result", param.Result).
			Msg("stock page recognition failed")
		return nil, false
	}

	// 已知零库存会进入任务级缺货集合，其余画面结果再结合已尝试、保留规则和当前策略过滤。
	prioritySelectionMarkScannedOutOfStock(page.Items)
	observations := buildStockObservations(page.Items, allGroups)
	targetItemID, recognized := selectGoodsTarget(param.Location, groups, policy, observations)
	if targetItemID == "" {
		// pending 表示上次点击尚未确认，不能改选其他物品或误判耗尽；
		// 没有 pending 时保存本帧集合，交给下一个 exhausted 节点做稳定确认。
		_, _, pending := prioritySelectionSnapshot(param.Location)
		if pending != "" {
			return nil, false
		}
		setGoodsSelectionExhaustedItems(param.Location, recognized)
		return nil, false
	}
	prioritySelectionResetExhaustion(param.Location)
	item, visible := findStockPageItem(page.Items, targetItemID)
	if !visible {
		return nil, false
	}

	// 先记为 pending，只有 Pipeline 确认返回据点界面后才会提交为已尝试物品。
	prioritySelectionSetPending(param.Location, targetItemID)
	detail := map[string]any{
		"item_id":            item.ItemID,
		"stock_known":        item.StockKnown,
		"scanned_item_count": len(observations),
	}
	if item.StockKnown {
		detail["stock_quantity"] = item.Quantity
		detail["stock_box"] = item.StockBox
	}
	detailJSON, _ := json.Marshal(detail)
	return &maa.CustomRecognitionResult{Box: item.ClickBox, Detail: string(detailJSON)}, true
}

func parsePriorityItemRecognitionParam(raw string) (*priorityItemRecognitionParam, error) {
	var param priorityItemRecognitionParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, fmt.Errorf("unmarshal custom_recognition_param: %w", err)
	}
	param.Location = strings.TrimSpace(param.Location)
	param.Result = strings.TrimSpace(param.Result)
	if param.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	if param.Result != priorityResultSelect && param.Result != priorityResultExhausted {
		return nil, fmt.Errorf("invalid result %q", param.Result)
	}
	if param.Result == priorityResultSelect {
		offsets, err := parseStockCellOffsets(
			param.StockNameOffset,
			param.StockQuantityOffset,
			param.StockClickOffset,
		)
		if err != nil {
			return nil, err
		}
		param.stockCellOffsets = offsets
	}
	return &param, nil
}

// buildPriorityExhaustedResult 只在连续两次候选集合一致时确认耗尽。
// 严格优先模式允许空集合，以便没有适用配置的地区正常结束售卖。
func buildPriorityExhaustedResult(location string, recognized []string) (*maa.CustomRecognitionResult, bool) {
	if !prioritySelectionObserveExhaustion(location, recognized) {
		return nil, false
	}
	detailJSON, _ := json.Marshal(map[string]any{
		"location":            location,
		"recognized_item_ids": recognized,
	})
	return &maa.CustomRecognitionResult{Detail: string(detailJSON)}, true
}
