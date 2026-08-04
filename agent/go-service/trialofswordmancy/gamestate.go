package trialofswordmancy

import (
	"github.com/MaaXYZ/MaaEnd/agent/go-service/trialofswordmancy/solver"
)

// stepReadings 是单步识别的原始读数汇总：适配器（Recognition.Run）从缓存与 OCR/模板收集
// 真实值后喂给 deriveGameState 做纯推导。全部字段都是「读到了什么」，不含任何推导规则。
//
//   - deck：RecognizeDeck 完整识别（remainDeck + Hand）后缓存的总牌量，轮次内恒定。
//   - remainDeck：本步 OCR 读到的牌库剩余库存。
//   - remainCalc / remainDouble / isDoubled：本步 OCR / 模板读到的演算次数、翻倍次数、翻倍态。
//   - aband：RecognizeAband 从放弃弹窗识别后写入的缓存；-1 = 未知（透传给求解器判不可达）。
type stepReadings struct {
	deck         [5]int
	remainDeck   [5]int
	remainCalc   int
	remainAband  int
	remainDouble int
	isDoubled    bool
}

// deriveGameState 把原始读数推导为求解器可查询的 GameState（纯函数，无 ctx / 无缓存依赖）。
//
// 唯一失败面：手牌推导与牌库读数互相矛盾（deck - remainDeck 出现负值或总张数 > 5）。
// 两个读数必有一个是错的，不在矛盾信息上做决策，返回 ok=false 由调用方中止。
// 其余情况一律透传：OCR 读到模型外的值（如演算次数 4）**不算失败**——状态合法性由
// solver 的 stateFilter 声明，越界读数原样送入，由求解器判不可达并中止（见 ADR-0001）。
//
// 偏移规则：屏幕的「本日剩余奖励演算次数」显示的是「当前进行中这局之外的剩余」——
// 进入抽牌界面即扣 1，而求解器把进行中这局也算作可用 → solver = OCR + 1
// （常规状态 RemainCalc 1..3，对应 OCR 0..2）。仅演算次数有此偏移：放弃/翻倍次数
// 界面显示的就是真实值，直接用。跨天残局那局白送：OCR 读到 3 → RemainCalc=4，
// 由求解器按特殊规则处理。
func deriveGameState(r stepReadings) (GameState, bool) {
	// 推导手牌并校验：remainDeck 读数必须与缓存 Deck 自洽（各点数非负、总张数 ≤ 5）。
	// 违例 = remainDeck OCR 抖动或缓存过期，显式中止；日志带缓存 Deck 与刚读的 remainDeck 供对账。
	var handCounts [5]int
	for i := 0; i < 5; i++ {
		handCounts[i] = r.deck[i] - r.remainDeck[i]
	}
	if !validHand(handCounts) {
		return GameState{}, false
	}

	// 推导路径没有槽位级识别结果：按点数升序合成 HandRaw 供 focus 展示（不影响求解）。
	handRaw := synthesizeHandRaw(handCounts)

	cfg := solver.DefaultConfig
	cfg.Deck = r.deck

	state := solver.State{
		RemainCalc:   r.remainCalc + 1,
		RemainAband:  r.remainAband,
		RemainDouble: r.remainDouble,
		IsDoubled:    r.isDoubled,
		Hand:         handCounts,
	}
	return GameState{
		State:   state,
		Config:  cfg,
		HandRaw: handRaw,
	}, true
}

// validHand 校验手牌计数合法性：各点数非负且总张数 ≤ 5。
func validHand(hand [5]int) bool {
	total := 0
	for _, c := range hand {
		if c < 0 {
			return false
		}
		total += c
	}
	return total <= 5
}

// synthesizeHandRaw 从手牌计数合成槽位展示数组：点数升序填槽（0=空槽）。
// 推导路径没有槽位级识别结果；HandRaw 仅用于 focus 展示，槽位顺序不影响求解。
func synthesizeHandRaw(hand [5]int) (handRaw [5]int) {
	slot := 0
	for point := 1; point <= 5 && slot < 5; point++ {
		for c := 0; c < hand[point-1] && slot < 5; c++ {
			handRaw[slot] = point
			slot++
		}
	}
	return handRaw
}
