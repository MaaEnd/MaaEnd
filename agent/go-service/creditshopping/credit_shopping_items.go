package creditshopping

// component 是 zerolog 日志的组件名。
const component = "creditshopping"

// creditItemIDByCase 把任务 checkbox 的 case 名映射到 IconRecognition catalog 的 item_id。
var creditItemIDByCase = map[string]string{
	"ArmsInspector":              "item_weapon_expcard_low",
	"ArmsINSPKit":                "item_weapon_expcard_mid",
	"ArsenalTicket":              "item_gachabyproducts_weapongold",
	"CastDie":                    "item_weapon_break_low",
	"ElementaryCognitiveCarrier": "item_expcard_stage2_low",
	"ElementaryCombatRecord":     "item_expcard_stage1_low",
	"HeavyCastDie":               "item_weapon_break_high",
	"IntermediateCombatRecord":   "item_expcard_stage1_mid",
	"Oroberyl":                   "item_diamond",
	"Protodisk":                  "item_char_break_stage_1_2",
	"Protohedron":                "item_char_skill_level_7_12",
	"Protoprism":                 "item_char_skill_level_1_6",
	"Protoset":                   "item_char_break_stage_3_4",
	"TCreds":                     "item_gold",
}
