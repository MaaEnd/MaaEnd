package resell

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomRecognitionRunner = &ResellCheckQuotaRecognition{}
	_ maa.CustomActionRunner      = &ResellInitAction{}
	_ maa.CustomActionRunner      = &ResellCheckQuotaAction{}
	_ maa.CustomActionRunner      = &ResellScanAction{}
	_ maa.CustomActionRunner      = &ResellScanSkipEmptyAction{}
	_ maa.CustomActionRunner      = &ResellScanCostAction{}
	_ maa.CustomActionRunner      = &ResellScanFriendPriceAction{}
	_ maa.CustomActionRunner      = &ResellScanNextAction{}
	_ maa.CustomActionRunner      = &ResellDecideAction{}
	_ maa.CustomActionRunner      = &ResellFinishAction{}
)

// Register registers all custom action components for resell package
func Register() {
	maa.AgentServerRegisterCustomRecognition("ResellCheckQuotaRecognition", &ResellCheckQuotaRecognition{})
	maa.AgentServerRegisterCustomAction("ResellInitAction", &ResellInitAction{})
	maa.AgentServerRegisterCustomAction("ResellCheckQuotaAction", &ResellCheckQuotaAction{})
	maa.AgentServerRegisterCustomAction("ResellScanAction", &ResellScanAction{})
	maa.AgentServerRegisterCustomAction("ResellScanSkipEmptyAction", &ResellScanSkipEmptyAction{})
	maa.AgentServerRegisterCustomAction("ResellScanCostAction", &ResellScanCostAction{})
	maa.AgentServerRegisterCustomAction("ResellScanFriendPriceAction", &ResellScanFriendPriceAction{})
	maa.AgentServerRegisterCustomAction("ResellScanNextAction", &ResellScanNextAction{})
	maa.AgentServerRegisterCustomAction("ResellDecideAction", &ResellDecideAction{})
	maa.AgentServerRegisterCustomAction("ResellFinishAction", &ResellFinishAction{})
}
