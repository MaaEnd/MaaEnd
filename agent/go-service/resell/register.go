package resell

import maa "github.com/MaaXYZ/maa-framework-go/v3"

// Register registers all custom action components for resell package
func Register() {
	maa.AgentServerRegisterCustomAction("ResellInitAction", &ResellInitAction{})
	maa.AgentServerRegisterCustomAction("ResellFinishAction", &ResellFinishAction{})
}
