package trialofswordmancy

// 本包日志 component 与 Custom 组件注册名（与 AutoStockpile.* 同风格）。
const (
	component = "trialofswordmancy"

	recognitionName = "TrialOfSwordmancy.Recognize"
	decideName      = "TrialOfSwordmancy.Decide"
)

// pipeline 节点名常量。识别/决策运行时通过这些名字复用现有节点或路由执行。
const (
	// Decide 节点：recognition=Custom(TrialOfSwordmancy.Recognize) + action=Custom(TrialOfSwordmancy.Decide)。
	decideNode = "TrialOfSwordmancyDecide"

	// 复用的现有识别节点（TrialOfSwordmancyCommon.json）。
	nodeRewardMode         = "TrialOfSwordmancyRewardMode"
	nodeDrawCard           = "TrialOfSwordmancyDrawCard"
	nodeDoubleReward       = "TrialOfSwordmancyDoubleReward"
	nodeOverflowExclamation = "TrialOfSwordmancyOverflowExclamation"

	// 卡牌在场识别节点（5 个槽位，固定 ROI，模板 EnemyCard.png）。
	nodeEnemyCardPrefix = "TrialOfSwordmancyEnemyCard" // + "1".."5"

	// 点数值识别的通用节点：Go 端运行时 override template + roi 后复用（仿 autostockpile locateGoods）。
	nodePointValue = "TrialOfSwordmancyPointValue"

	// 决策 → 执行映射的 Do 入口节点（节点自行点击按钮 + 等动画，next 回 Decide）。
	nodeDoDrawCard          = "TrialOfSwordmancyDoDrawCard"           // 抽牌按钮 + 第三抽弹窗 + 等动画
	nodeDoDrawCardConfirm   = "TrialOfSwordmancyDoDrawCardConfirm"   // 第三抽「抽取后无法更改翻倍」弹窗确认
	nodeDoWaitDrawCardFreezes = "TrialOfSwordmancyDoWaitDrawCardFreezes" // 等抽牌动画结束
	nodeDoDoubleReward      = "TrialOfSwordmancyDoDoubleReward"      // 翻倍按钮 + 等动画

	// 复用的既有执行链节点。
	nodeGiveUp     = "TrialOfSwordmancyDailyGiveUp" // 放弃 → 确认 → 重置寻路 → 回主入口
	nodeStartTrial = "TrialOfSwordmancyStartTrial"  // 开始演算 → 编队 → 战斗 → 领奖
	// 注：TrialOfSwordmancyDailyFinish（奖励耗尽终点）由 pipeline 自行汇入（CheckRewardMode /
	// Fight.json 的 RewardExhausted），Go 不路由到它；不可达则 action 直接 return false。
)

// 点数模板路径前缀：Point1.png ... Point5.png（实现期从游戏截图裁取）。
const pointTemplatePrefix = "TrialOfSwordmancy/Point"

// 卡牌槽位 ROI（与 TrialOfSwordmancyEnemyCard1..5 一致，用于点数值匹配时的 override roi）。
var cardSlotROI = [5][4]int{
	{119, 318, 125, 118}, // 槽位 1（点数 1 的牌所在位）
	{318, 318, 125, 118}, // 槽位 2
	{517, 318, 125, 118}, // 槽位 3
	{716, 318, 125, 118}, // 槽位 4
	{915, 318, 125, 118}, // 槽位 5
}

// TODO(需真机截图校准): 以下为「剩余次数 / 翻倍态 / 牌库构成」识别所需的 ROI 与模板，
// 与 Point1-5.png 同属识别前置数据。未校准前 recognition 读不到对应值，
// State 相关字段为 0/false，Decide 会判定状态不可达并安全结束任务（见 action.go）。
// 校准时只需改这里的常量。
var (
	roiRemainCalc   = [4]int{0, 0, 0, 0} // 剩余演算次数 OCR 区域
	roiRemainAband  = [4]int{0, 0, 0, 0} // 剩余放弃次数 OCR 区域
	roiRemainDouble = [4]int{0, 0, 0, 0} // 剩余翻倍次数 OCR 区域
	tplIsDoubled    = "TrialOfSwordmancy/DoubleRewardSelected.png" // 已翻倍指示模板（待裁取）
	roiIsDoubled    = [4]int{0, 0, 0, 0}                            // 已翻倍指示模板匹配区域

	// 牌库构成（每 72h 轮换，必须 OCR，不能硬编码）。roiDeckCount[i] = 第 (i+1) 点牌的库存数 OCR 区域。
	roiDeckCount = [5][4]int{
		{0, 0, 0, 0}, // 点数 1 库存数
		{0, 0, 0, 0}, // 点数 2
		{0, 0, 0, 0}, // 点数 3
		{0, 0, 0, 0}, // 点数 4
		{0, 0, 0, 0}, // 点数 5
	}
)
