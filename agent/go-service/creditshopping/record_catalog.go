package creditshopping

// recordCatalog 货架记录用全物品 Pipeline 节点（与 record.json / CreditShoppingRecordAllItems 的 any_of 一致，不看购买选项）。
var recordCatalog = []recordCatalogItem{
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
