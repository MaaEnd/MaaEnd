package resell

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner = &ResellInitAction{}
	_ maa.CustomActionRunner = &ResellFinishAction{}
)

// Register registers all custom action components for resell package
func Register() {
	maa.AgentServerRegisterCustomAction("ResellInitAction", &ResellInitAction{})
	maa.AgentServerRegisterCustomAction("ResellFinishAction", &ResellFinishAction{})
}
