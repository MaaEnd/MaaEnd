package trialofswordmancy

import "github.com/rs/zerolog/log"

// 完整牌库 Deck（总牌量）的持久化缓存。
//
// 为何单独缓存、不能每步从截图读：一轮演算内 Deck 恒定不变，而完整识别需要跑 Hand 模板匹配
// （不稳定）。故轮次开始时由 RecognizeDeck 完整识别一次（remainDeck + Hand）推导出 Deck 并缓存，
// 之后总成识别只读 remainDeck（OCR，稳定）反推 Hand = Deck - remainDeck，跳过 Hand 模板识别。
//
// 生命周期：
//   - 进程内初始化为 invalid（未识别）。
//   - 每轮任务开始时由 RecognizeDeck 写入（完整识别成功才写，失败不写半截结果）。
//   - 决策到开始演算 / 放弃演算时由 DecideAction 重置（下一轮是新牌库）。
//
// MaaFramework 保证任务回调单线程同步执行，无需加锁。
var (
	deckCache    [5]int
	deckCacheSet bool
)

func getDeck() ([5]int, bool) {
	return deckCache, deckCacheSet
}

func setDeck(deck [5]int) {
	deckCache = deck
	deckCacheSet = true
}

func resetDeckCache() {
	deckCache = [5]int{}
	deckCacheSet = false
	log.Debug().Str("component", component).Msg("deck cache reset")
}
