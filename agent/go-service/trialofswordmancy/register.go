// Package trialofswordmancy 实现选剑演武的牌库识别、MDP 求解与单步决策。
// 界面跳转由 pipeline 的 TrialOfSwordmancy*.json 负责，Go 侧只做识别、求解与路由。
package trialofswordmancy

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
	"github.com/rs/zerolog/log"
)

// Register 注册选剑演武的自定义识别器与动作。
func Register() {
	maa.AgentServerRegisterCustomRecognition(recognizeBoardName, &BoardRecognition{})
	maa.AgentServerRegisterCustomRecognition(recognizeDeckName, &DeckRecognition{})
	maa.AgentServerRegisterCustomRecognition(recognizeFullDeckName, &FullDeckRecognition{})
	maa.AgentServerRegisterCustomRecognition(recognizeRemainAbandonName, &RemainAbandonRecognition{})
	maa.AgentServerRegisterCustomAction(decideName, &DecideAction{})
	maa.AgentServerRegisterCustomAction(freeCalcDecideName, &FreeCalcDecideAction{})
	maa.AgentServerRegisterCustomAction(initSolverName, &InitSolverAction{})

	log.Info().
		Str("component", component).
		Msg("trialofswordmancy custom recognition/actions registered")
}
