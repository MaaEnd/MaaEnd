package yuanyuanmanor

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/pkg/ocrnum"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	targetDishCount     = 6
	targetDrinkCount    = 3
	maxDishesPerTab     = 2
	minMenuCardQuantity = 10
)

var _ maa.CustomActionRunner = &MenuDecideAction{}

// MenuDecideAction 根据 InnkeepingMenuRecognition 的识别结果决定当前页签是否继续选卡及目标卡片位置。
type MenuDecideAction struct{}

// Run 解析当前页签与已选数量，覆写目标卡片锚点与点击成功计数节点，或结束当前页签。
func (a *MenuDecideAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		log.Error().Str("component", component).Msg("menu decide context, arg or recognition detail is nil")
		return false
	}

	reading, ok := parseMenuReading(arg.RecognitionDetail)
	if !ok {
		return false
	}

	innkeepingState.onTabRecognized(reading.Tab, reading.Count)
	pickedInTab := innkeepingState.pickedInCurrentTab(reading.Count)
	cardIndex := innkeepingState.nextCardIndex()

	if !shouldContinueMenuTab(reading.Tab, reading.Count, pickedInTab, cardIndex) {
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{}); err != nil {
			log.Error().Err(err).Str("component", component).Str("tab", string(reading.Tab)).Msg("failed to finish menu tab")
			return false
		}
		log.Info().
			Str("component", component).
			Str("tab", string(reading.Tab)).
			Int("count", reading.Count).
			Int("pickedInTab", pickedInTab).
			Int("cardIndex", cardIndex).
			Msg("menu tab selection finished")
		return true
	}

	cardNode := nodeInnkeepingMenuCardPositions[cardIndex]
	roi, err := nodeROI(ctx, cardNode)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("cardNode", cardNode).Msg("failed to read menu card position ROI")
		return false
	}

	countNode := menuCountNode(reading.Tab)
	expectedCount := strconv.Itoa(reading.Count+1) + "/8"

	if err := ctx.OverridePipeline(map[string]any{
		nodeInnkeepingMenuCardPositionAnchor: map[string]any{
			"roi": roi,
		},
		countNode: map[string]any{
			"expected": []string{expectedCount},
		},
		nodeInnkeepingMenuPickCardSuccess: map[string]any{
			"all_of": []string{countNode},
		},
	}); err != nil {
		log.Error().Err(err).Str("component", component).Str("cardNode", cardNode).Msg("failed to override menu card nodes")
		return false
	}

	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{
		{Name: nodeInnkeepingMenuCheckCard},
		{Name: nodeInnkeepingMenuDecide},
	}); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to route menu decide to check card")
		return false
	}

	log.Info().
		Str("component", component).
		Str("tab", string(reading.Tab)).
		Int("count", reading.Count).
		Int("pickedInTab", pickedInTab).
		Int("card", cardIndex+1).
		Str("cardNode", cardNode).
		Str("expectedCount", expectedCount).
		Msg("menu card selected for check")
	return true
}

func shouldContinueMenuTab(tab MenuTab, count, pickedInTab, cardIndex int) bool {
	if cardIndex < 0 || cardIndex >= len(nodeInnkeepingMenuCardPositions) {
		return false
	}
	if tab == TabDrinks {
		return count < targetDrinkCount && pickedInTab < targetDrinkCount
	}
	return count < targetDishCount && pickedInTab < maxDishesPerTab
}

func parseMenuReading(detail *maa.RecognitionDetail) (menuReading, bool) {
	detailJSON := unwrapCustomDetail(detail)
	if detailJSON == "" {
		log.Error().Str("component", component).Msg("menu recognition detail json is empty")
		return menuReading{}, false
	}
	var reading menuReading
	if err := json.Unmarshal([]byte(detailJSON), &reading); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to parse menu reading")
		return menuReading{}, false
	}
	return reading, true
}

func unwrapCustomDetail(detail *maa.RecognitionDetail) string {
	if detail == nil || detail.DetailJson == "" {
		return ""
	}
	var wrapped struct {
		Best struct {
			Detail json.RawMessage `json:"detail"`
		} `json:"best"`
	}
	if err := json.Unmarshal([]byte(detail.DetailJson), &wrapped); err == nil && len(wrapped.Best.Detail) > 0 {
		return rawJSONToString(wrapped.Best.Detail)
	}
	return detail.DetailJson
}

func rawJSONToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return string(raw)
		}
		return s
	}
	return string(raw)
}

var _ maa.CustomActionRunner = &MenuCheckCardAction{}

// MenuCheckCardAction 校验当前目标卡片的可烹饪/库存数量并在足够时记录菜品信息。
type MenuCheckCardAction struct{}

// Run 检查卡片左下角数量是否大于 10；不足则跳过当前卡片回到决策节点，足够则记录菜品并进入点击流程。
func (a *MenuCheckCardAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
	if ctx == nil || arg == nil || arg.RecognitionDetail == nil {
		log.Error().Str("component", component).Msg("menu check card context, arg or recognition detail is nil")
		return false
	}

	observed := indexObservations(arg.RecognitionDetail)

	itemID, err := readIcon(observed, nodeInnkeepingMenuCardIconRecognition)
	if err != nil || itemID == "" {
		log.Error().Err(err).Str("component", component).Msg("failed to read menu card icon")
		return false
	}

	quantity, err := readCardQuantity(observed, nodeInnkeepingMenuCardQuantity)
	if err != nil {
		log.Error().Err(err).Str("component", component).Str("itemID", itemID).Msg("failed to read menu card quantity")
		return false
	}

	card := innkeepingState.checkedCardNumber()

	if quantity <= minMenuCardQuantity {
		if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: nodeInnkeepingMenuDecide}}); err != nil {
			log.Error().Err(err).Str("component", component).Msg("failed to skip card on insufficient quantity")
			return false
		}
		log.Info().
			Str("component", component).
			Str("tab", string(innkeepingState.currentTab)).
			Int("card", card).
			Str("itemID", itemID).
			Int("quantity", quantity).
			Msg("menu card quantity insufficient, skipping card")
		return true
	}

	innkeepingState.recordMenuItem(innkeepingState.currentTab, itemID, quantity)

	if err := ctx.OverrideNext(arg.CurrentTaskName, []maa.NextItem{{Name: nodeInnkeepingMenuPickCard}}); err != nil {
		log.Error().Err(err).Str("component", component).Msg("failed to route check card to pick card")
		return false
	}

	log.Info().
		Str("component", component).
		Str("tab", string(innkeepingState.currentTab)).
		Int("card", card).
		Str("itemID", itemID).
		Int("quantity", quantity).
		Msg("menu card checked and recorded")
	return true
}

func readCardQuantity(observed observations, nodeName string) (int, error) {
	detail := observed[nodeName]
	if detail == nil || detail.Results == nil {
		return 0, fmt.Errorf("missing card quantity OCR %s", nodeName)
	}
	for _, results := range [][]*maa.RecognitionResult{{detail.Results.Best}, detail.Results.Filtered, detail.Results.All} {
		for _, result := range results {
			if result == nil {
				continue
			}
			text, ok := result.AsOCR()
			if ok && text != nil && text.Text != "" {
				quantity, err := ocrnum.Parse(text.Text)
				if err == nil {
					return quantity, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("card quantity OCR %s has no usable number", nodeName)
}
