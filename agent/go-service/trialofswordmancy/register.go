package trialofswordmancy

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register 注册选剑演武包提供的自定义识别器与动作。
//
//   - TrialOfSwordmancy.Recognize：总成识别（一图多位置 → GameState；放弃次数持久化+探测）。
//   - TrialOfSwordmancy.Decide：MDP 单步决策 → OverrideNext 路由执行。
func Register() {
	maa.AgentServerRegisterCustomRecognition(recognitionName, &Recognition{})
	maa.AgentServerRegisterCustomAction(decideName, &DecideAction{})

	log.Info().
		Str("component", component).
		Msg("trialofswordmancy custom recognition/actions registered")
}
