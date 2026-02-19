package autoheadhunting

var lang = "zh_cn"

func t(key string) string {
	if langMap, exists := locals[lang]; exists {
		if translated, exists := langMap[key]; exists {
			return translated
		}
	}
	return key
}

func o(key string) (string, string) {
	if opMap, exists := operators[key]; exists {
		if translated, exists := opMap[lang]; exists {
			return translated, opMap["stars"]
		}
	}
	return key, "0"
}

// oByLocalName 通过任意语言的本地化名称反向查找干员信息
// 返回 (当前语言的本地化名称, 星级)
// 使用预构建的反向索引进行 O(1) 查找
func oByLocalName(localName string) (string, string) {
	if key, exists := reverseOperatorMap[localName]; exists {
		return o(key)
	}
	return localName, "0"
}

// reverseOperatorMap 缓存从任意语言的本地化名称到英文 key 的反向映射
var reverseOperatorMap map[string]string

// isSixStar 判断干员名（任意语言的本地化名称）是否为六星干员
// 使用预构建的 set 进行 O(1) 查找
func isSixStar(localizedName string) bool {
	_, exists := sixStarSet[localizedName]
	return exists
}

// sixStarSet 缓存当前语言下所有六星干员的本地化名称
var sixStarSet map[string]struct{}

/*
buildOperatorCaches 根据当前 lang 从 operators 表预构建：
1. sixStarSet: 所有语言下六星干员本地化名称的集合
2. reverseOperatorMap: 所有语言下本地化名称到英文 key 的反向映射
应在 lang 设置后调用一次
*/
func buildOperatorCaches() {
	sixStarSet = make(map[string]struct{})
	reverseOperatorMap = make(map[string]string)
	for key, opMap := range operators {
		for langKey, name := range opMap {
			if langKey == "stars" {
				continue
			}
			reverseOperatorMap[name] = key
		}
		if opMap["stars"] == "6" {
			// 将所有语言的六星干员名称都加入 set，确保 OCR 任何语言结果都能匹配
			for langKey, name := range opMap {
				if langKey == "stars" {
					continue
				}
				sixStarSet[name] = struct{}{}
			}
		}
	}
}

// 显示文本 Display Text
var locals = map[string]map[string]string{
	"zh_cn": {
		"params":           "配置参数",
		"target_pulls":     "目标抽数",
		"target_operator":  "目标干员",
		"target_num":       "目标数量",
		"prefer_mode":      "偏好模式",
		"fallback_1x":      "距离目标抽数小于10抽，切换为单抽模式",
		"results":          "抽取结果: %s",
		"final_results":    "最终结果: %v",
		"used_pulls":       "已抽取次数: %d / %d",
		"unenough_pulls":   "剩余抽数不足，已终止任务。",
		"task_unknown_err": "未知错误",
		"done":             "完成 %d 次抽取，共获取 %d 个目标干员（%s）",
		"any_6_stars":      "任意★6干员",
	},
}

// 干员信息占位符 以后会加载外部 json
var operators = map[string]map[string]string{
	"Perlica": {
		"zh_cn": "佩丽卡",
		"en_us": "Perlica",
		"ja_jp": "ペリカ",
		"ko_kr": "펠리카",
		"zh_tw": "佩麗卡",
		"stars": "5",
	},
	"Yvonne": {
		"zh_cn": "伊冯",
		"en_us": "Yvonne",
		"ja_jp": "イヴォンヌ",
		"ko_kr": "이본",
		"zh_tw": "伊馮",
		"stars": "6",
	},
	"Ardelia": {
		"zh_cn": "艾尔黛拉",
		"en_us": "Ardelia",
		"ja_jp": "アルデリア",
		"ko_kr": "아델리아",
		"zh_tw": "艾爾黛拉",
		"stars": "6",
	},
	"Gilberta": {
		"zh_cn": "洁尔佩塔",
		"en_us": "Gilberta",
		"ja_jp": "ギルベルタ",
		"ko_kr": "질베르타",
		"zh_tw": "潔爾佩塔",
		"stars": "6",
	},
	"ChenQianyu": {
		"zh_cn": "陈千语",
		"en_us": "Chen Qianyu",
		"ja_jp": "チェン・センユー",
		"ko_kr": "진천우",
		"zh_tw": "陳千語",
		"stars": "5",
	},
	"Wulfgard": {
		"zh_cn": "狼卫",
		"en_us": "Wulfgard",
		"ja_jp": "ウルフガード",
		"ko_kr": "울프가드",
		"zh_tw": "狼衛",
		"stars": "5",
	},
	"Antal": {
		"zh_cn": "安塔尔",
		"en_us": "Antal",
		"ja_jp": "アンタル",
		"ko_kr": "안탈",
		"zh_tw": "安塔爾",
		"stars": "4",
	},
	"Xaihi": {
		"zh_cn": "赛希",
		"en_us": "Xaihi",
		"ja_jp": "ザイヒ",
		"ko_kr": "자이히",
		"zh_tw": "賽希",
		"stars": "5",
	},
	"Lifeng": {
		"zh_cn": "黎风",
		"en_us": "Lifeng",
		"ja_jp": "リーフォン",
		"ko_kr": "여풍",
		"zh_tw": "黎風",
		"stars": "6",
	},
	"Catcher": {
		"zh_cn": "卡契尔",
		"en_us": "Catcher",
		"ja_jp": "キャッチャー",
		"ko_kr": "카치르",
		"zh_tw": "卡契爾",
		"stars": "4",
	},
	"Arclight": {
		"zh_cn": "弧光",
		"en_us": "Arclight",
		"ja_jp": "アークライト",
		"ko_kr": "아크라이트",
		"zh_tw": "弧光",
		"stars": "5",
	},
	"Avywenna": {
		"zh_cn": "艾维文娜",
		"en_us": "Avywenna",
		"ja_jp": "アイビーエナ",
		"ko_kr": "아비웨나",
		"zh_tw": "艾維文娜",
		"stars": "5",
	},
	"Laevatain": {
		"zh_cn": "莱万汀",
		"en_us": "Laevatain",
		"ja_jp": "レーヴァテイン",
		"ko_kr": "레바테인",
		"zh_tw": "萊萬汀",
		"stars": "6",
	},
	"Alesh": {
		"zh_cn": "阿列什",
		"en_us": "Alesh",
		"ja_jp": "アレッシュ",
		"ko_kr": "알레쉬",
		"zh_tw": "阿列什",
		"stars": "5",
	},
	"Pogranichnik": {
		"zh_cn": "骏卫",
		"en_us": "Pogranichnik",
		"ja_jp": "ポグラニチニク",
		"ko_kr": "포그라니치니크",
		"zh_tw": "駿衛",
		"stars": "6",
	},
	"LastRite": {
		"zh_cn": "别礼",
		"en_us": "Last Rite",
		"ja_jp": "ラストライト",
		"ko_kr": "라스트 라이트",
		"zh_tw": "別禮",
		"stars": "6",
	},
	"Ember": {
		"zh_cn": "余烬",
		"en_us": "Ember",
		"ja_jp": "エンバー",
		"ko_kr": "엠버",
		"zh_tw": "餘燼",
		"stars": "6",
	},
	"Snowshine": {
		"zh_cn": "昼雪",
		"en_us": "Snowshine",
		"ja_jp": "スノーシャイン",
		"ko_kr": "스노우샤인",
		"zh_tw": "晝雪",
		"stars": "5",
	},
	"DaPan": {
		"zh_cn": "大潘",
		"en_us": "Da Pan",
		"ja_jp": "ダパン",
		"ko_kr": "판",
		"zh_tw": "大潘",
		"stars": "5",
	},
	"Fluorite": {
		"zh_cn": "萤石",
		"en_us": "Fluorite",
		"ja_jp": "フローライト",
		"ko_kr": "플루라이트",
		"zh_tw": "螢石",
		"stars": "4",
	},
	"Akekuri": {
		"zh_cn": "秋栗",
		"en_us": "Akekuri",
		"ja_jp": "アケクリ",
		"ko_kr": "아케쿠리",
		"zh_tw": "秋栗",
		"stars": "4",
	},
	"Estella": {
		"zh_cn": "埃特拉",
		"en_us": "Estella",
		"ja_jp": "エステーラ",
		"ko_kr": "에스텔라",
		"zh_tw": "埃特拉",
		"stars": "4",
	},
	"Endministrator": {
		"zh_cn": "管理员",
		"en_us": "Endmin",
		"ja_jp": "管理人",
		"ko_kr": "관리자",
		"zh_tw": "管理員",
		"stars": "6",
	},
}
