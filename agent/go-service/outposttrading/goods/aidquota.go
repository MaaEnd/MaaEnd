package goods

import (
	"fmt"
	"image"
	"regexp"
	"strconv"

	"github.com/MaaXYZ/MaaEnd/agent/go-service/outposttrading/internal/selectiondata"
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const aidQuotaBalanceNodeName = "OutpostTradingAidQuotaBalance"

var aidQuotaDigitsPattern = regexp.MustCompile(`\d+`)

// aidQuotaDecision 描述按调度券余量对活动物品售卖数量做出的限制决策。
type aidQuotaDecision struct {
	// Applied 为 true 表示本次按调度券余量限制了售卖数量。
	Applied bool
	// Balance 是识别到的据点当前可兑换调度券余量。
	Balance int
	// Limit 是按调度券余量与单价算出的可兑换数量上限。
	Limit int
	// Target 是本次实际应该售卖的数量。
	Target int
	// ItemID 是被限制的当前售卖物品。
	ItemID string
	// UnitPrice 是该物品在当前据点的单价。
	UnitPrice int
}

// resolveAidQuotaLimit 读取据点当前可兑换调度券余量，为活动物品计算售卖数量上限。
// 常驻物品不受调度券余量限制（ActivityID 为空），直接返回未应用的决策。
// 识别失败时返回未应用的决策并记录警告，售卖流程回退到保留规则。
//
// 活动物品的目标数量就是调度券可兑换上限，统一关闭 ReverseTarget；
// BetterSliding 的 ClampTargetToSliderMax 会把目标压到库存，无需在此读库存。
func resolveAidQuotaLimit(ctx *maa.Context, itemID string) aidQuotaDecision {
	decision := aidQuotaDecision{ItemID: itemID}
	if ctx == nil || itemID == "" {
		return decision
	}

	unitPrice, activityID, ok := currentItemValueAttrs(itemID)
	if !ok {
		return decision
	}
	if activityID == "" {
		return decision
	}
	decision.UnitPrice = unitPrice

	img, err := captureAidQuotaImage(ctx)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", reserveSessionActionName).
			Str("item_id", itemID).
			Msg("failed to capture image for aid quota limit, skip quantity limit")
		return decision
	}

	balance, err := runAidQuotaOCR(ctx, img, aidQuotaBalanceNodeName)
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", reserveSessionActionName).
			Str("item_id", itemID).
			Msg("failed to read outpost aid quota balance, skip quantity limit")
		return decision
	}
	decision.Balance = balance
	decision.Limit = balance / unitPrice
	decision.Applied = true
	decision.Target = decision.Limit
	return decision
}

// currentItemValueAttrs 返回当前物品的单价与活动标记。
// 同一物品在各据点的单价一致（rewardMoneyCount 按物品确定），取任一据点即可。
func currentItemValueAttrs(itemID string) (unitPrice int, activityID string, ok bool) {
	data, err := selectiondata.LoadCached()
	if err != nil {
		log.Warn().
			Err(err).
			Str("component", reserveSessionActionName).
			Str("item_id", itemID).
			Msg("failed to load selection data for aid quota limit")
		return 0, "", false
	}
	for _, location := range data.Locations {
		for _, item := range location.Items {
			if item.ItemID == itemID {
				return item.UnitPrice, item.ActivityID, true
			}
		}
	}
	return 0, "", false
}

func captureAidQuotaImage(ctx *maa.Context) (image.Image, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is nil")
	}
	tasker := ctx.GetTasker()
	if tasker == nil {
		return nil, fmt.Errorf("tasker is nil")
	}
	controller := tasker.GetController()
	if controller == nil {
		return nil, fmt.Errorf("controller is nil")
	}
	controller.PostScreencap().Wait()
	img, err := controller.CacheImage()
	if err != nil {
		return nil, fmt.Errorf("cache image: %w", err)
	}
	if img == nil {
		return nil, fmt.Errorf("cached image is nil")
	}
	return img, nil
}

func runAidQuotaOCR(ctx *maa.Context, img image.Image, nodeName string) (int, error) {
	detail, err := ctx.RunRecognition(nodeName, img, nil)
	if err != nil {
		return 0, fmt.Errorf("run recognition %s: %w", nodeName, err)
	}
	text, ok := firstOCRTextFromDetail(detail)
	if !ok {
		return 0, fmt.Errorf("recognition %s has no OCR text", nodeName)
	}
	value, err := parseAidQuotaNumber(text)
	if err != nil {
		return 0, fmt.Errorf("node %s: %w", nodeName, err)
	}
	return value, nil
}

func parseAidQuotaNumber(text string) (int, error) {
	match := aidQuotaDigitsPattern.FindString(text)
	if match == "" {
		return 0, fmt.Errorf("ocr text %q does not contain digits", text)
	}
	value, err := strconv.Atoi(match)
	if err != nil {
		return 0, fmt.Errorf("parse number %q: %w", match, err)
	}
	if value < 0 {
		return 0, fmt.Errorf("value must be non-negative, got %d", value)
	}
	return value, nil
}

// firstOCRTextFromDetail 从识别结果中提取第一段 OCR 文本。
func firstOCRTextFromDetail(detail *maa.RecognitionDetail) (string, bool) {
	if detail == nil {
		return "", false
	}
	if text, ok := ocrTextFromResults(detail.Results); ok {
		return text, true
	}
	if len(detail.CombinedResult) == 0 {
		return "", false
	}
	for _, child := range detail.CombinedResult {
		if child == nil {
			continue
		}
		if child.Box == detail.Box {
			if text, ok := ocrTextFromResults(child.Results); ok {
				return text, true
			}
		}
	}
	return "", false
}

func ocrTextFromResults(results *maa.RecognitionResults) (string, bool) {
	if results == nil {
		return "", false
	}
	for _, bucket := range [][]*maa.RecognitionResult{
		{results.Best},
		results.Filtered,
		results.All,
	} {
		for _, r := range bucket {
			if r == nil {
				continue
			}
			ocr, ok := r.AsOCR()
			if !ok || ocr == nil {
				continue
			}
			if ocr.Text != "" {
				return ocr.Text, true
			}
		}
	}
	return "", false
}
