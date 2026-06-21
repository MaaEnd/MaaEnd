package trialofswordmancy

// 日志 component 与 Custom 组件注册名。
const (
	component = "trialofswordmancy"

	recognitionName = "TrialOfSwordmancy.Recognize" // pipeline 节点的 custom_recognition
	decideName      = "TrialOfSwordmancy.Decide"    // pipeline 节点的 custom_action
)

// pipeline 节点名常量。
const (
	// 决策循环入口：recognition=Recognize、action=Decide；next 由 Decide 运行时 override 到下方执行节点。
	decideNode = "TrialOfSwordmancyDecide"

	// 抽牌界面通用识别节点（定义在 TrialOfSwordmancyCommon.json）。
	nodeRewardMode          = "TrialOfSwordmancyRewardMode"          // 奖励演算模式标志
	nodeDrawCard            = "TrialOfSwordmancyDrawCard"            // 抽牌按钮
	nodeDoubleReward        = "TrialOfSwordmancyDoubleReward"        // 翻倍按钮
	nodeOverflowExclamation = "TrialOfSwordmancyOverflowExclamation" // 溢出（爆表）叹号
	nodeEnemyCard1          = "TrialOfSwordmancyEnemyCard1"          // 第1张卡牌 Lv（在场=已回抽牌界面）

	// 决策执行节点（节点自行点击按钮 + 等动画，完成后 next 回 Decide）。
	nodeDoDrawCard            = "TrialOfSwordmancyDoDrawCard"            // 抽一张牌
	nodeDoDrawCardConfirm     = "TrialOfSwordmancyDoDrawCardConfirm"     // 第三抽「抽取后无法更改翻倍」弹窗
	nodeDoWaitDrawCardFreezes = "TrialOfSwordmancyDoWaitDrawCardFreezes" // 等抽牌动画结束
	nodeDoDoubleReward        = "TrialOfSwordmancyDoDoubleReward"        // 选择本局翻倍

	// 既有执行链入口。
	nodeGiveUp     = "TrialOfSwordmancyDailyGiveUp" // 放弃本局 → 确认 → 重置寻路 → 回主入口
	nodeStartTrial = "TrialOfSwordmancyStartTrial"  // 开始演算 → 编队 → 战斗 → 领奖

	// 探测剩余放弃次数时点击的两个按钮。
	nodeGiveUpButton = "TrialOfSwordmancyGiveUp" // 放弃按钮（点击弹放弃确认框）
	nodeCancelButton = "CancelButton"            // 通用取消按钮（点击不放弃、回正常界面）
)

// go-service 专用识别节点名（定义在 TrialOfSwordmancyCommon.json 的 [go] 区，ROI/模板都在 JSON 里）。
// Go 经 ctx.RunRecognition 按名调用并解析结果，不硬编码坐标。
const (
	nodeRemainCalc   = "TrialOfSwordmancyRemainCalc"   // OCR：本日剩余演算次数
	nodeRemainDouble = "TrialOfSwordmancyRemainDouble" // OCR：剩余翻倍次数
	nodeAbandPopup   = "TrialOfSwordmancyAbandPopup"   // OCR：放弃确认弹窗文本
	nodeIsDoubled    = "TrialOfSwordmancyIsDoubled"    // 模板：已翻倍指示

	nodeDeckCountPrefix = "TrialOfSwordmancyDeckCount" // + "1".."5"：牌库各点数库存数 OCR
	nodeHandPointPrefix = "TrialOfSwordmancyHandPoint" // + "1".."5"：手牌各槽点数位（模板，运行时 override template）
)

// Point 模板路径前缀：Point1.png … Point5.png。
// recognizePointValue 运行时把对应模板 override 到 TrialOfSwordmancyHandPoint{slot} 上（roi 由该节点定），
// 取各点数最高分得该槽点数。
const pointTemplatePrefix = "TrialOfSwordmancy/Point"
