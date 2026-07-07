package essencefilter

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	_ maa.ResourceEventSink = &resourcePathSink{}

	_ maa.CustomActionRunner = &EssenceFilterInitAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemLevelAction{}
	_ maa.CustomActionRunner = &EssenceFilterSkillDecisionAction{}
	_ maa.CustomActionRunner = &EssenceFilterFinishAction{}
	_ maa.CustomActionRunner = &EssenceFilterTraceAction{}
)

func Register() {
	maa.AgentServerAddResourceSink(&resourcePathSink{})
	maa.AgentServerRegisterCustomAction("EssenceFilterInitAction", &EssenceFilterInitAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterCheckItemAction", &EssenceFilterCheckItemAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterCheckItemLevelAction", &EssenceFilterCheckItemLevelAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterSkillDecisionAction", &EssenceFilterSkillDecisionAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterFinishAction", &EssenceFilterFinishAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterTraceAction", &EssenceFilterTraceAction{})

	//战斗后识别版本
	maa.AgentServerRegisterCustomAction("EssenceFilterAfterBattleSkillDecisionAction", &EssenceFilterAfterBattleSkillDecisionAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterAfterBattleTierGateAction", &EssenceFilterAfterBattleTierGateAction{})
	maa.AgentServerRegisterCustomRecognition("EssenceFilterAfterBattleNthRecognition", &EssenceFilterAfterBattleNthRecognition{})
}
