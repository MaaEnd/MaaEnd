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
	groupsByLocation, err := loadItemPriorityGroupsFunc()
	if err != nil {
		log.Error().Err(err).Str("component", priorityItemRecognitionName).Msg("failed to load item priorities")
		return nil, false
	}
	policy := priorityPolicySnapshot()
	allGroups := groupsByLocation[param.Location]
	groups := prioritizeItemGroups(allGroups, policy.Preferred, policy.OnlyPreferred)
	if len(groups) == 0 {
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
	return runGoodsSelectionRecognition(ctx, arg, param, allGroups, groups, policy)
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

// runGoodsSelectionRecognition 只扫描第一页完整可见的货品格子。
// 所有模式用库存排除零库存商品，再由当前策略选择第一页候选。
func runGoodsSelectionRecognition(
	ctx *maa.Context,
	arg *maa.CustomRecognitionArg,
	param *priorityItemRecognitionParam,
	allGroups []itemPriorityGroup,
	candidateGroups []itemPriorityGroup,
	policy prioritySelectionPolicy,
) (*maa.CustomRecognitionResult, bool) {
	if param.Result == priorityResultExhausted {
		return buildGoodsSelectionExhaustedResult(param.Location)
	}
	if param.Result != priorityResultSelect {
		return nil, false
	}
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
	prioritySelectionMarkScannedOutOfStock(page.Items)
	observations := buildStockObservations(page.Items, allGroups)
	targetItemID, recognized := selectGoodsTarget(param.Location, candidateGroups, policy, observations)
	if targetItemID == "" {
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
	prioritySelectionSetPending(param.Location, targetItemID)
	return buildGoodsSelectionResult(item, len(observations)), true
}

func buildGoodsSelectionResult(item stockPageItem, scannedItemCount int) *maa.CustomRecognitionResult {
	detailJSON, _ := json.Marshal(map[string]any{
		"item_id":            item.ItemID,
		"stock_quantity":     item.Quantity,
		"stock_box":          item.StockBox,
		"scanned_item_count": scannedItemCount,
	})
	return &maa.CustomRecognitionResult{Box: item.ClickBox, Detail: string(detailJSON)}
}
