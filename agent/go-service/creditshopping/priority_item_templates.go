package creditshopping

// priorityItemTemplate 信用购物「优先购买 N」勾选物品对应的模板识别参数（720p）。
type priorityItemTemplate struct {
	ID        string
	Template  string
	Threshold float64
}

var priorityItemShelfROI = []int{38, 95, 1194, 527}

// priorityItemCaseOrder 与 assets/tasks/CreditShopping.json 各档 Items checkbox cases 顺序一致。
var priorityItemCaseOrder = []string{
	"ArsenalTicket",
	"Oroberyl",
	"TCreds",
	"ElementaryCognitiveCarrier",
	"ElementaryCombatRecord",
	"IntermediateCombatRecord",
	"ArmsInspector",
	"ArmsINSPKit",
	"CastDie",
	"HeavyCastDie",
	"Protodisk",
	"Protoset",
	"Protoprism",
	"Protohedron",
}

// priorityItemCatalog 与 CreditShoppingItems case 名一致，模板与 assets/tasks/CreditShopping.json 勾选 override 对齐。
var priorityItemCatalog = []priorityItemTemplate{
	{ID: "ArmsInspector", Template: "CreditShopping/Item/item_weapon_expcard_low.png", Threshold: 0.7},
	{ID: "ArmsINSPKit", Template: "CreditShopping/Item/item_weapon_expcard_mid.png", Threshold: 0.7},
	{ID: "ArsenalTicket", Template: "CreditShopping/Item/item_gachabyproducts_weapongold.png", Threshold: 0.7},
	{ID: "CastDie", Template: "CreditShopping/Item/item_weapon_break_low.png", Threshold: 0.7},
	{ID: "ElementaryCognitiveCarrier", Template: "CreditShopping/Item/item_expcard_stage2_low.png", Threshold: 0.7},
	{ID: "ElementaryCombatRecord", Template: "CreditShopping/Item/item_expcard_2_1.png", Threshold: 0.92},
	{ID: "IntermediateCombatRecord", Template: "CreditShopping/Item/item_expcard_2_2.png", Threshold: 0.92},
	{ID: "HeavyCastDie", Template: "CreditShopping/Item/item_weapon_break_high.png", Threshold: 0.7},
	{ID: "Oroberyl", Template: "CreditShopping/Item/item_diamond.png", Threshold: 0.7},
	{ID: "Protodisk", Template: "CreditShopping/Item/item_char_break_stage_1_2.png", Threshold: 0.7},
	{ID: "Protoprism", Template: "CreditShopping/Item/item_char_skill_level_1_6.png", Threshold: 0.7},
	{ID: "Protohedron", Template: "CreditShopping/Item/item_char_skill_level_7_12.png", Threshold: 0.7},
	{ID: "Protoset", Template: "CreditShopping/Item/item_char_break_stage_3_4.png", Threshold: 0.7},
	{ID: "TCreds", Template: "CreditShopping/Item/item_gold.png", Threshold: 0.7},
}

func priorityItemByID(id string) (priorityItemTemplate, bool) {
	for _, item := range priorityItemCatalog {
		if item.ID == id {
			return item, true
		}
	}
	return priorityItemTemplate{}, false
}

func priorityItemNodeName(level int) string {
	return "CreditShoppingPriority" + itoaLevel(level) + "Item"
}

func itoaLevel(level int) string {
	switch level {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	default:
		return "?"
	}
}

func priorityItemTemplateListOverride(ids []string) map[string]any {
	templates := make([]string, 0, len(ids))
	thresholds := make([]float64, 0, len(ids))
	for _, id := range ids {
		item, ok := priorityItemByID(id)
		if !ok {
			continue
		}
		templates = append(templates, item.Template)
		thresholds = append(thresholds, item.Threshold)
	}
	return map[string]any{
		"recognition": "TemplateMatch",
		"roi":         priorityItemShelfROI,
		"template":    templates,
		"threshold":   thresholds,
		"green_mask":  true,
		"order_by":    "Vertical",
	}
}
