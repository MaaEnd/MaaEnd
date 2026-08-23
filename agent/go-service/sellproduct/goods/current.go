package goods

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/iconrecognition"
	sellstrategy "github.com/MaaXYZ/MaaEnd/agent/go-service/sellproduct/goods/strategy"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const currentGoodsRecognitionName = "SellProductCurrentGoods"

type currentGoodsRecognitionParam struct {
	Location string `json:"location"`
	ROI      []int  `json:"roi"`
}

// currentGoodsDetail 是当前货品识别结果与 adopt 动作之间传递的结构化 detail。
type currentGoodsDetail struct {
	Location string  `json:"location"`
	ItemID   string  `json:"item_id"`
	Score    float64 `json:"score"`
}

// CurrentGoodsRecognition 在据点售卖主界面识别当前选中的货品图标。
// 稀有度和单价策略仅在当前货品恰好是下一候选时命中；库存策略只沿用可由
// 优先槽位直接确定的下一候选，其余情况由 Pipeline 进入选货列表重新扫描库存。
type CurrentGoodsRecognition struct{}

var _ maa.CustomRecognitionRunner = (*CurrentGoodsRecognition)(nil)

func (r *CurrentGoodsRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
	if ctx == nil || arg == nil || arg.Img == nil {
		return nil, false
	}
	param, err := parseCurrentGoodsRecognitionParam(arg.CustomRecognitionParam)
	if err != nil {
		log.Error().Err(err).Str("component", currentGoodsRecognitionName).Msg("invalid params")
		return nil, false
	}
	policy := priorityPolicySnapshot()
	if policy.Strategy == sellstrategy.KindStock && len(policy.Preferred) == 0 {
		log.Debug().
			Str("component", currentGoodsRecognitionName).
			Str("location", param.Location).
			Msg("current goods adoption skipped for stock strategy without preferred items")
		return nil, false
	}

	// 候选只保留当前据点的可售物品，缩小 IconRecognition 的模板匹配范围。
	groupsByLocation, err := loadItemPriorityGroupsFunc()
	if err != nil {
		log.Error().Err(err).Str("component", currentGoodsRecognitionName).Msg("failed to load item priorities")
		return nil, false
	}
	groups := groupsByLocation[param.Location]
	if len(groups) == 0 {
		log.Error().Str("component", currentGoodsRecognitionName).Str("location", param.Location).
			Msg("location has no sellable items")
		return nil, false
	}
	itemIDs := make([]string, 0, len(groups))
	for _, group := range groups {
		itemIDs = append(itemIDs, group.ItemID)
	}

	detail, err := ctx.RunRecognitionDirect(
		maa.RecognitionTypeCustom,
		&maa.CustomRecognitionParam{
			ROI:               maa.NewTargetRect(maa.Rect{param.ROI[0], param.ROI[1], param.ROI[2], param.ROI[3]}),
			CustomRecognition: iconrecognition.CustomRecognitionName,
			CustomRecognitionParam: iconrecognition.NewParams(
				iconrecognition.WithGridType(iconrecognition.GridTypeSingleROI),
				iconrecognition.WithItemIDs(itemIDs...),
			),
		},
		arg.Img,
	)
	if err != nil {
		log.Warn().Err(err).
			Str("component", currentGoodsRecognitionName).
			Str("location", param.Location).
			Msg("current goods icon recognition failed")
		return nil, false
	}
	parsed, _, err := iconrecognition.ParseRecognitionDetail(detail)
	if err != nil {
		log.Warn().Err(err).
			Str("component", currentGoodsRecognitionName).
			Str("location", param.Location).
			Msg("failed to parse current goods icon recognition detail")
		return nil, false
	}
	// no_match 表示识别正常完成但没有候选达到阈值，交由 Pipeline 回落到换货流程。
	if isCurrentGoodsNoMatch(parsed.Error) {
		log.Debug().
			Str("component", currentGoodsRecognitionName).
			Str("location", param.Location).
			Msg("current goods slot is empty or unrecognized")
		return nil, false
	}
	if parsed.Error != nil {
		log.Warn().
			Str("component", currentGoodsRecognitionName).
			Str("location", param.Location).
			Str("error_code", string(parsed.Error.Code)).
			Str("error_message", parsed.Error.Message).
			Msg("current goods icon recognition returned error")
		return nil, false
	}
	if !parsed.Matched || len(parsed.Matches) == 0 {
		return nil, false
	}

	match := parsed.Matches[0]
	if !currentGoodsMatchesSelection(param.Location, match.ItemID, groups, policy) {
		log.Debug().
			Str("component", currentGoodsRecognitionName).
			Str("location", param.Location).
			Str("item_id", match.ItemID).
			Msg("current goods recognized but not next selection")
		return nil, false
	}

	log.Info().
		Str("component", currentGoodsRecognitionName).
		Str("location", param.Location).
		Str("item_id", match.ItemID).
		Float64("score", match.Score).
		Msg("current goods recognized")
	detailJSON, _ := json.Marshal(currentGoodsDetail{
		Location: param.Location,
		ItemID:   match.ItemID,
		Score:    match.Score,
	})
	return &maa.CustomRecognitionResult{Box: match.CellBox, Detail: string(detailJSON)}, true
}

func isCurrentGoodsNoMatch(detailErr *iconrecognition.DetailError) bool {
	return detailErr != nil && detailErr.Code == iconrecognition.ErrorCodeNoMatch
}

func parseCurrentGoodsRecognitionParam(raw string) (*currentGoodsRecognitionParam, error) {
	var param currentGoodsRecognitionParam
	if err := json.Unmarshal([]byte(raw), &param); err != nil {
		return nil, fmt.Errorf("unmarshal custom_recognition_param: %w", err)
	}
	param.Location = strings.TrimSpace(param.Location)
	if param.Location == "" {
		return nil, fmt.Errorf("location is empty")
	}
	if len(param.ROI) != 4 {
		return nil, fmt.Errorf("roi length is %d, expected 4", len(param.ROI))
	}
	// IconRecognition 的 single_roi 要求正方形 ROI。
	if param.ROI[2] <= 0 || param.ROI[2] != param.ROI[3] {
		return nil, fmt.Errorf("roi must be a positive square, got %v", param.ROI)
	}
	return &param, nil
}

// currentGoodsMatchesSelection 判断当前货品是否等于当前选品规则的下一候选。
// 库存策略只能静态确定优先槽位中的候选；没有可用优先货品时，因库存未知而
// 不会选中普通候选，Pipeline 会打开列表读取实时库存。
func currentGoodsMatchesSelection(
	location string,
	itemID string,
	groups []itemPriorityGroup,
	policy prioritySelectionPolicy,
) bool {
	groups = prioritizeItemGroups(groups, policy.Preferred, policy.OnlyPreferred)
	observations := make(map[string]sellstrategy.Candidate, len(groups))
	for _, group := range groups {
		observations[group.ItemID] = sellstrategy.Candidate{
			ItemID:    group.ItemID,
			Rarity:    group.Rarity,
			UnitPrice: group.UnitPrice,
		}
	}
	targetItemID, _ := selectGoodsTarget(location, groups, policy, observations)
	return targetItemID == itemID
}

// currentGoodsItemIDFromRecognition 从识别节点的 detail 中解析当前货品 ID，
// 供 adopt 动作读取；识别节点不命中时不会进入 adopt，此处失败视为契约破坏。
func currentGoodsItemIDFromRecognition(detail *maa.RecognitionDetail) (string, error) {
	if detail == nil || detail.Results == nil {
		return "", fmt.Errorf("recognition detail is empty")
	}
	result := detail.Results.Best
	if result == nil && len(detail.Results.All) > 0 {
		result = detail.Results.All[0]
	}
	if result == nil {
		return "", fmt.Errorf("custom result is empty")
	}
	custom, ok := result.AsCustom()
	if !ok || custom == nil {
		return "", fmt.Errorf("result is not custom recognition")
	}
	return currentGoodsItemIDFromCustomResult(custom)
}

func currentGoodsItemIDFromCustomResult(result *maa.CustomRecognitionResult) (string, error) {
	if result == nil {
		return "", fmt.Errorf("custom result is empty")
	}
	return parseCurrentGoodsDetail(result.Detail)
}

func parseCurrentGoodsDetail(raw string) (string, error) {
	var parsed currentGoodsDetail
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return "", fmt.Errorf("parse current goods detail: %w", err)
	}
	parsed.ItemID = strings.TrimSpace(parsed.ItemID)
	if parsed.ItemID == "" {
		return "", fmt.Errorf("item_id is empty")
	}
	return parsed.ItemID, nil
}
