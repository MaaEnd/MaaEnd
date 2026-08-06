package trialofswordmancy

// 日志 component 与 Custom 组件注册名。
const (
	component = "trialofswordmancy"

	recognitionName      = "TrialOfSwordmancy.Recognize"      // pipeline 节点的 custom_recognition
	deckRecognitionName  = "TrialOfSwordmancy.RecognizeDeck"  // 完整牌库识别 custom_recognition（缓存 Deck）
	abandRecognitionName = "TrialOfSwordmancy.RecognizeAband" // 放弃弹窗文本 custom_recognition
	decideName           = "TrialOfSwordmancy.Decide"         // pipeline 节点的 custom_action
)

// pipeline 节点名常量。
const (
	// 决策执行节点（节点自行点击按钮 + 等动画，完成后 next 回 Decide）。
	nodeDoDrawCard     = "TrialOfSwordmancyDoDrawCard"     // 抽一张牌
	nodeDoDoubleReward = "TrialOfSwordmancyDoDoubleReward" // 选择本局翻倍

	// 抽牌成功等待节点；Decide 决策抽牌时按落位槽覆盖其 all_of。
	nodeDoDrawCardSuccess = "TrialOfSwordmancyDoDrawCardSuccess"

	// 决策节点：异步预求解要读它的 custom_action_param.overflowMode 来构造求解配置
	// （action.go decideOverflowMode）——预求解配置必须与决策配置同源，读不到即中止。
	nodeDecide = "TrialOfSwordmancyDecide"

	// 既有执行链入口。
	nodeGiveUp     = "TrialOfSwordmancyDailyGiveUp" // 放弃本局 → 确认 → 重置寻路 → 回主入口
	nodeStartTrial = "TrialOfSwordmancyStartTrial"  // 开始演算 → 编队 → 战斗 → 领奖

	// 第 N 张在场卡牌（定义在 Common.json 上半区，pipeline 可见）；+ "1".."5" 组成 EnemyCardN。
	// Decide 覆盖 DoDrawCardSuccess 的 all_of 用，标识第 N 张牌已落地。
	nodeEnemyCardPrefix = "TrialOfSwordmancyEnemyCard"

	// 战力点锚点（Common.json 上半区）；+ "0".."10" 组成 BattlePtsN，命中 ⟺ 战力点 == N
	// （手牌点数总和 % 11，见 solver.PowerOf）。Decide 覆盖 DoDrawCard 的 wait_node 用。
	nodeBattlePtsPrefix = "TrialOfSwordmancyBattlePts"
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
