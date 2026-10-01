package creditshopping

import (
	"image"
	"strings"

	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

const (
	pipelineNodeRecordItemDiscount = "RecordItemDiscount"
	discountNone                   = "None"
)

type recordItem struct {
	Node string
	ID   string
	Name string
}

// recordItems 是货架记录用的全物品模板节点，不看购买选项。
var recordItems = []recordItem{
	{Node: "CreditShoppingRecordItemArmsInspector", ID: "ArmsInspector", Name: "武器检查单元"},
	{Node: "CreditShoppingRecordItemArmsINSPKit", ID: "ArmsINSPKit", Name: "武器检查装置"},
	{Node: "CreditShoppingRecordItemArsenalTicket", ID: "ArsenalTicket", Name: "武库配额"},
	{Node: "CreditShoppingRecordItemCastDie", ID: "CastDie", Name: "强固模具"},
	{Node: "CreditShoppingRecordItemElementaryCognitiveCarrier", ID: "ElementaryCognitiveCarrier", Name: "初级认知载体"},
	{Node: "CreditShoppingRecordItemElementaryCombatRecord", ID: "ElementaryCombatRecord", Name: "初级作战记录"},
	{Node: "CreditShoppingRecordItemIntermediateCombatRecord", ID: "IntermediateCombatRecord", Name: "中级作战记录"},
	{Node: "CreditShoppingRecordItemHeavyCastDie", ID: "HeavyCastDie", Name: "重型强固模具"},
	{Node: "CreditShoppingRecordItemOroberyl", ID: "Oroberyl", Name: "嵌晶玉"},
	{Node: "CreditShoppingRecordItemProtodisk", ID: "Protodisk", Name: "协议圆盘"},
	{Node: "CreditShoppingRecordItemProtoprism", ID: "Protoprism", Name: "协议棱柱"},
	{Node: "CreditShoppingRecordItemProtohedron", ID: "Protohedron", Name: "协议棱柱组"},
	{Node: "CreditShoppingRecordItemProtoset", ID: "Protoset", Name: "协议圆盘组"},
	{Node: "CreditShoppingRecordItemTCreds", ID: "TCreds", Name: "折金票"},
}

type SlotRecord struct {
	Slot     int    `json:"slot"`
	Name     string `json:"name"`
	ID       string `json:"id,omitempty"`
	Discount string `json:"discount"`
}

func scanShelfNameHits(ctx *maa.Context, img image.Image) []ocrNameHit {
	hits := make([]ocrNameHit, 0, len(recordItems))
	for _, item := range recordItems {
		detail, err := ctx.RunRecognition(item.Node, img, nil)
		if err != nil || detail == nil || !detail.Hit {
			continue
		}
		for _, result := range recognitionResults(detail) {
			matched, ok := result.AsTemplateMatch()
			if !ok || matched == nil || !rectValid(matched.Box) {
				continue
			}
			hits = append(hits, ocrNameHit{Box: matched.Box, Text: item.Name, ID: item.ID})
		}
	}
	if len(hits) == 0 {
		log.Info().Str("component", component).Msg("shelf scan: no record item template hit")
	}
	return hits
}

// ScanShelfSlotsPC 单次截图：按 Y 聚类为两排，上 7 槽（0–6）、下 3 槽（7–9），按 X 排序。
func ScanShelfSlotsPC(ctx *maa.Context, img image.Image) []SlotRecord {
	hits := scanShelfNameHits(ctx, img)
	return buildSlotRecords(ctx, img, hits, slotAssignPC)
}

func recordDiscountAtNameBox(ctx *maa.Context, img image.Image, nameBox maa.Rect) string {
	override := recordItemDiscountPipelineOverride(nameBox)
	detail, err := ctx.RunRecognition(pipelineNodeRecordItemDiscount, img, override)
	if err != nil || detail == nil || !detail.Hit {
		return discountNone
	}
	text := strings.TrimSpace(bestOCRText(detail))
	if text == "" {
		return discountNone
	}
	return text
}
