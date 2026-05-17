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
// 未识别到花费时返回 0，表示初次进入货架、尚未对应到某次刷新花费。
func resolveRefreshIndex(ctx *maa.Context, img image.Image) (refreshIndex int, refreshCost int) {
	cost, ok := refreshCostFromImage(ctx, img)
	if !ok {
		return 0, 0
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
