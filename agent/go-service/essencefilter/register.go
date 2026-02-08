package essencefilter

import (
	maa "github.com/MaaXYZ/maa-framework-go/v4"
)

var (
	_ maa.ResourceEventSink = &resourcePathSink{}
)

func Register() {
	maa.AgentServerAddResourceSink(&resourcePathSink{})
	maa.AgentServerRegisterCustomAction("EssenceFilterInitAction", &EssenceFilterInitAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterCheckItemAction", &EssenceFilterCheckItemAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterRowCollectAction", &EssenceFilterRowCollectAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterRowNextItemAction", &EssenceFilterRowNextItemAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterSkillDecisionAction", &EssenceFilterSkillDecisionAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterFinishAction", &EssenceFilterFinishAction{})
	maa.AgentServerRegisterCustomAction("EssenceFilterTraceAction", &EssenceFilterTraceAction{})

}
