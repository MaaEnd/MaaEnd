package creditshopping

import (
	"image"
	"strconv"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const pipelineNodeRefreshCost = "RefreshCost"

// 刷新按钮 OCR 花费与当日第几次刷新（1-based）的对应关系。
var refreshCostToIndex = map[int]int{
	80:  1,
	120: 2,
	160: 3,
	200: 4,
}

// refreshIndexWhenCostAbsent 当日刷新花费 OCR 未命中（显示 "-" / 次数用尽 / 冷却）时，
// 视为已完成当日最后一次（第 4 次）刷新后的货架状态。
const refreshIndexWhenCostAbsent = 4

func refreshCostFromImage(ctx *maa.Context, img image.Image) (cost int, ok bool) {
	detail, err := ctx.RunRecognition(pipelineNodeRefreshCost, img, nil)
	if err != nil || detail == nil || !detail.Hit {
		return 0, false
	}
	text := strings.TrimSpace(bestOCRText(detail))
	if text == "" {
		return 0, false
	}
	n, err := strconv.Atoi(text)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// resolveRefreshIndex 根据 RefreshCost OCR 推断当日第几次刷新（1-based）。
// 未识别到花费（"-"、次数用尽、冷却文案等）时视为第 4 次刷新后的状态。
func resolveRefreshIndex(ctx *maa.Context, img image.Image) (refreshIndex int, refreshCost int) {
	cost, ok := refreshCostFromImage(ctx, img)
	if !ok {
		return refreshIndexWhenCostAbsent, 0
	}
	if idx, known := refreshCostToIndex[cost]; known {
		return idx, cost
	}
	log.Warn().
		Str("component", component).
		Int("refresh_cost", cost).
		Msg("shelf record: unknown refresh cost, use refresh_index 0")
	return 0, cost
}
