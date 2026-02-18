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

var locals = map[string]map[string]string{
	"zh_cn": {
		// 干员名称 OperatorName
		"伊冯":   "伊冯",
		"余烬":   "余烬",
		"别礼":   "别礼",
		"洁尔佩塔": "洁尔佩塔",
		"管理员":  "管理员",
		"艾尔黛拉": "艾尔黛拉",
		"莱万汀":  "莱万汀",
		"骏卫":   "骏卫",
		"黎风":   "黎风",
		"佩丽卡":  "佩丽卡",
		"大潘":   "大潘",
		"弧光":   "弧光",
		"昼雪":   "昼雪",
		"狼卫":   "狼卫",
		"艾维文娜": "艾维文娜",
		"赛希":   "赛希",
		"阿列什":  "阿列什",
		"陈千语":  "陈千语",
		"卡契尔":  "卡契尔",
		"埃特拉":  "埃特拉",
		"安塔尔":  "安塔尔",
		"秋栗":   "秋栗",
		"萤石":   "萤石",
		// 显示文本 Display Text
		"target_pulls":     "目标抽数",
		"target_operator":  "目标干员",
		"target_num":       "目标数量",
		"prefer_mode":      "偏好模式",
		"fallback_1x":      "距离目标抽数小于10抽，切换为单抽模式",
		"results":          "抽取结果: %s",
		"final_results":    "最终结果: %v",
		"used_pulls":       "已抽取次数",
		"task_unknown_err": "未知错误",
		"done":             "完成 %d 次抽取，共获取 %d 个目标干员（%s）",
	},
}
