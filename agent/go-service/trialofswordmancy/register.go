package trialofswordmancy

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register 注册选剑演武包提供的自定义识别器与动作。
//
//   - TrialOfSwordmancy.Recognize：总成识别（每步只读 remainDeck，手牌由缓存的 Deck 反推；读取已缓存的放弃次数）。
//   - TrialOfSwordmancy.RecognizeDeck：完整牌库识别（remainDeck + Hand → 缓存总牌量 Deck，轮次开始时调用）。
//   - TrialOfSwordmancy.RecognizeAband：从放弃弹窗文本识别并缓存剩余放弃次数。
//   - TrialOfSwordmancy.Decide：MDP 单步决策 → OverrideNext 路由执行（开始演算/放弃时重置牌库缓存）。
func Register() {
	maa.AgentServerRegisterCustomRecognition(recognitionName, &Recognition{})
	maa.AgentServerRegisterCustomRecognition(deckRecognitionName, &DeckRecognition{})
	maa.AgentServerRegisterCustomRecognition(abandRecognitionName, &AbandRecognition{})
	maa.AgentServerRegisterCustomAction(decideName, &DecideAction{})

	log.Info().
		Str("component", component).
		Msg("trialofswordmancy custom recognition/actions registered")
}
