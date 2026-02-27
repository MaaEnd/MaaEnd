package resell

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomRecognitionRunner = &ResellCheckQuotaRecognition{}
	_ maa.CustomActionRunner      = &ResellInitAction{}
	_ maa.CustomActionRunner      = &ResellCheckQuotaAction{}
	_ maa.CustomActionRunner      = &ResellScanProductAction{}
	_ maa.CustomActionRunner      = &ResellDecideAction{}
	_ maa.CustomActionRunner      = &ResellFinishAction{}
)

// Register registers all custom action components for resell package
func Register() {
	maa.AgentServerRegisterCustomRecognition("ResellCheckQuotaRecognition", &ResellCheckQuotaRecognition{})
	maa.AgentServerRegisterCustomAction("ResellInitAction", &ResellInitAction{})
	maa.AgentServerRegisterCustomAction("ResellCheckQuotaAction", &ResellCheckQuotaAction{})
	maa.AgentServerRegisterCustomAction("ResellScanProductAction", &ResellScanProductAction{})
	maa.AgentServerRegisterCustomAction("ResellDecideAction", &ResellDecideAction{})
	maa.AgentServerRegisterCustomAction("ResellFinishAction", &ResellFinishAction{})
}
