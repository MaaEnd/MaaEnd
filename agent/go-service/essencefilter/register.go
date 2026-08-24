package essencefilter

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("EssenceFilterInitAction", &EssenceFilterInitAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterCheckItemAction", &EssenceFilterCheckItemAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterCheckItemLevelAction", &EssenceFilterCheckItemLevelAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterSkillDecisionAction", &EssenceFilterSkillDecisionAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterFinishAction", &EssenceFilterFinishAction{})

	//战斗后识别版本
	maa.AgentServerRegisterCustomAction("EssenceFilterAfterBattleSkillDecisionAction", &EssenceFilterAfterBattleSkillDecisionAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterAfterBattleTierGateAction", &EssenceFilterAfterBattleTierGateAction{})
	maa.AgentServerRegisterCustomRecognition("EssenceFilterAfterBattleNthRecognition", &EssenceFilterAfterBattleNthRecognition{})
}
