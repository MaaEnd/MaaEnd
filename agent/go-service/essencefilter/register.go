package essencefilter

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	_ maa.ResourceEventSink = &resourcePathSink{}

	_ maa.CustomActionRunner = &EssenceFilterInitAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemAction{}
	_ maa.CustomActionRunner = &EssenceFilterCheckItemLevelAction{}
	_ maa.CustomActionRunner = &EssenceFilterRowCollectAction{}
	_ maa.CustomActionRunner = &EssenceFilterRowNextItemAction{}
	_ maa.CustomActionRunner = &EssenceFilterSkillDecisionAction{}
	_ maa.CustomActionRunner = &EssenceFilterFinishAction{}
	_ maa.CustomActionRunner = &EssenceFilterSwipeCalibrateAction{}
	_ maa.CustomActionRunner = &EssenceFilterTraceAction{}
	_ maa.CustomActionRunner = &OCREssenceInventoryNumberAction{}
)

func Register() {
	maa.AgentServerAddResourceSink(&resourcePathSink{})
	maa.AgentServerRegisterCustomAction("EssenceFilterInitAction", &EssenceFilterInitAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterCheckItemAction", &EssenceFilterCheckItemAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterCheckItemLevelAction", &EssenceFilterCheckItemLevelAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterRowCollectAction", &EssenceFilterRowCollectAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterRowNextItemAction", &EssenceFilterRowNextItemAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterSkillDecisionAction", &EssenceFilterSkillDecisionAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterFinishAction", &EssenceFilterFinishAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterSwipeCalibrateAction", &EssenceFilterSwipeCalibrateAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterTraceAction", &EssenceFilterTraceAction{})
	maa.AgentServerRegisterCustomAction("OCREssenceInventoryNumberAction", &OCREssenceInventoryNumberAction{})
}
