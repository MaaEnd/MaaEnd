package supplyplan

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the operator growth planner for the target-inventory mode.
func Register() {
	maa.AgentServerRegisterCustomAction("SupplyPlanOperatorPlan", &OperatorPlanAction{})
}
