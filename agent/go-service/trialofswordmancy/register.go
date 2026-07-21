package trialofswordmancy

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register 注册选剑演武包提供的自定义识别器与动作。
//
//   - TrialOfSwordmancy.Recognize：总成识别（一图多位置 → GameState；读取已缓存的放弃次数）。
//   - TrialOfSwordmancy.RecognizeAband：从放弃弹窗文本识别并缓存剩余放弃次数。
//   - TrialOfSwordmancy.Decide：MDP 单步决策 → OverrideNext 路由执行。
func Register() {
	maa.AgentServerRegisterCustomRecognition(recognitionName, &Recognition{})
	maa.AgentServerRegisterCustomRecognition(abandRecognitionName, &AbandRecognition{})
	maa.AgentServerRegisterCustomAction(decideName, &DecideAction{})

	log.Info().
		Str("component", component).
		Msg("trialofswordmancy custom recognition/actions registered")
}
