package protocolspace

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers ProtocolSpace custom components.
func Register() {
	maa.AgentServerRegisterCustomAction("ProtocolSpaceCalculateOperatorMaterialsAction", &CalculateOperatorMaterialsAction{})
	maa.AgentServerRegisterCustomAction("ProtocolSpaceSumOperatorMaterialsAction", &SumOperatorMaterialsAction{})
	maa.AgentServerRegisterCustomAction("ProtocolSpaceOverrideOperatorSupplyPlanAction", &OverrideOperatorSupplyPlanAction{})
}
