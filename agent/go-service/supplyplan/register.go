package supplyplan

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the operator growth planner and its optional daily red-label check.
func Register() {
	maa.AgentServerRegisterCustomAction("SupplyPlanOperatorPlan", &OperatorPlanAction{})
	maa.AgentServerRegisterCustomAction("SupplyPlanRedCheckNext", &RedCheckNextAction{})
}
