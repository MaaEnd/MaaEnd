package autoessence

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers AutoEssence inventory and target-plan custom components.
func Register() {
	maa.AgentServerRegisterCustomAction("AutoEssenceLoadInventoryAction", &LoadInventoryAction{})
	maa.AgentServerRegisterCustomAction("AutoEssenceTargetPlanAction", &TargetPlanAction{})
}
