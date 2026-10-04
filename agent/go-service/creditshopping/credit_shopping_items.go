package creditshopping

// creditShoppingItem 信用交易所可购物品：任务 case 名与 IconRecognition catalog item_id。
type creditShoppingItem struct {
	CaseID            string
	Name              string
	RecognitionItemID string
}

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

var creditShoppingItemCatalog = []creditShoppingItem{
	{CaseID: "ArmsInspector", Name: "武器检查单元", RecognitionItemID: "item_weapon_expcard_low"},
	{CaseID: "ArmsINSPKit", Name: "武器检查装置", RecognitionItemID: "item_weapon_expcard_mid"},
	{CaseID: "ArsenalTicket", Name: "武库配额", RecognitionItemID: "item_gachabyproducts_weapongold"},
	{CaseID: "CastDie", Name: "强固模具", RecognitionItemID: "item_weapon_break_low"},
	{CaseID: "ElementaryCognitiveCarrier", Name: "初级认知载体", RecognitionItemID: "item_expcard_stage2_low"},
	{CaseID: "ElementaryCombatRecord", Name: "初级作战记录", RecognitionItemID: "item_expcard_2_1"},
	{CaseID: "IntermediateCombatRecord", Name: "中级作战记录", RecognitionItemID: "item_expcard_2_2"},
	{CaseID: "HeavyCastDie", Name: "重型强固模具", RecognitionItemID: "item_weapon_break_high"},
	{CaseID: "Oroberyl", Name: "嵌晶玉", RecognitionItemID: "item_diamond"},
	{CaseID: "Protodisk", Name: "协议圆盘", RecognitionItemID: "item_char_break_stage_1_2"},
	{CaseID: "Protoprism", Name: "协议棱柱", RecognitionItemID: "item_char_skill_level_1_6"},
	{CaseID: "Protohedron", Name: "协议棱柱组", RecognitionItemID: "item_char_skill_level_7_12"},
	{CaseID: "Protoset", Name: "协议圆盘组", RecognitionItemID: "item_char_break_stage_3_4"},
	{CaseID: "TCreds", Name: "折金票", RecognitionItemID: "item_gold"},
}

// creditTradeIconROIWin32 为 IconRecognition grid_type=credit_trade 的 Win32 参考 ROI（720p）。
var creditTradeIconROIWin32 = []int{70, 95, 1140, 415}

func priorityItemByID(caseID string) (creditShoppingItem, bool) {
	for _, item := range creditShoppingItemCatalog {
		if item.CaseID == caseID {
			return item, true
		}
	}
	return creditShoppingItem{}, false
}

func creditCaseIDFromRecognitionItemID(recognitionItemID string) (caseID string, ok bool) {
	for _, item := range creditShoppingItemCatalog {
		if item.RecognitionItemID == recognitionItemID {
			return item.CaseID, true
		}
	}
	return "", false
}

func creditShoppingRecognitionItemIDs(caseIDs []string) []string {
	out := make([]string, 0, len(caseIDs))
	for _, caseID := range caseIDs {
		item, ok := priorityItemByID(caseID)
		if !ok {
			continue
		}
		out = append(out, item.RecognitionItemID)
	}
	return out
}

func allCreditShoppingRecognitionItemIDs() []string {
	out := make([]string, 0, len(creditShoppingItemCatalog))
	for _, item := range creditShoppingItemCatalog {
		out = append(out, item.RecognitionItemID)
	}
	return out
}
