package trialofswordmancy

// 日志 component 与 Custom 组件注册名。
const (
	component = "trialofswordmancy"

	recognitionName      = "TrialOfSwordmancy.Recognize"      // pipeline 节点的 custom_recognition
	abandRecognitionName = "TrialOfSwordmancy.RecognizeAband" // 放弃弹窗文本 custom_recognition
	decideName           = "TrialOfSwordmancy.Decide"         // pipeline 节点的 custom_action
)

// pipeline 节点名常量。
const (
	// 决策循环入口：recognition=Recognize、action=Decide；next 由 Decide 运行时 override 到下方执行节点。
	decideNode = "TrialOfSwordmancyDecide"

	// 抽牌界面通用识别节点（定义在 TrialOfSwordmancyCommon.json）。
	nodeDoubleReward        = "TrialOfSwordmancyDoubleReward"        // 翻倍按钮
	nodeOverflowExclamation = "TrialOfSwordmancyOverflowExclamation" // 溢出（爆表）叹号

	// 决策执行节点（节点自行点击按钮 + 等动画，完成后 next 回 Decide）。
	nodeDoDrawCard            = "TrialOfSwordmancyDoDrawCard"            // 抽一张牌
	nodeDoDrawCardConfirm     = "TrialOfSwordmancyDoDrawCardConfirm"     // 第三抽「抽取后无法更改翻倍」弹窗
	nodeDoWaitDrawCardFreezes = "TrialOfSwordmancyDoWaitDrawCardFreezes" // 等抽牌动画结束
	nodeDoDoubleReward        = "TrialOfSwordmancyDoDoubleReward"        // 选择本局翻倍

	// 既有执行链入口。
	nodeGiveUp     = "TrialOfSwordmancyDailyGiveUp" // 放弃本局 → 确认 → 重置寻路 → 回主入口
	nodeStartTrial = "TrialOfSwordmancyStartTrial"  // 开始演算 → 编队 → 战斗 → 领奖

)

// go-service 专用识别节点名（定义在 TrialOfSwordmancyCommon.json 的 [go] 区，ROI/模板都在 JSON 里）。
// Go 经 ctx.RunRecognition 按名调用并解析结果，不硬编码坐标。
const (
	nodeRemainCalc     = "TrialOfSwordmancyRemainCalc"     // OCR：本日剩余演算次数
	nodeRemainDouble   = "TrialOfSwordmancyRemainDouble"   // OCR：剩余翻倍次数
	nodeAbandPopup     = "TrialOfSwordmancyAbandPopup"     // OCR：放弃确认弹窗文本
	nodeAbandExhausted = "TrialOfSwordmancyAbandExhausted" // ColorMatch：放弃次数耗尽时的红色文本
	nodeIsDoubled      = "TrialOfSwordmancyIsDoubled"      // 模板：已翻倍指示

	nodeDeck               = "TrialOfSwordmancyDeck"         // OCR：牌库整列库存数
	nodeDeckCountPrefix    = "TrialOfSwordmancyDeckCount"    // + "1".."5"：牌库各点数库存数 OCR
	nodeHandPointPrefix    = "TrialOfSwordmancyHandPoint"    // + "1".."5"：各点数模板整行匹配
	nodeHandPositionPrefix = "TrialOfSwordmancyHandPosition" // + "1".."5"：手牌槽位 ROI
)
