package charactercontroller

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner = &CharacterControllerYawDeltaAction{}
	_ maa.CustomActionRunner = &CharacterControllerPitchDeltaAction{}
	_ maa.CustomActionRunner = &CharacterControllerForwardAxisAction{}
)

 // Register registers all custom recognition and action components for charactercontroller package
func Register() {
	maa.AgentServerRegisterCustomAction("CharacterControllerYawDeltaAction", &CharacterControllerYawDeltaAction{})
	maa.AgentServerRegisterCustomAction("CharacterControllerPitchDeltaAction", &CharacterControllerPitchDeltaAction{})
	maa.AgentServerRegisterCustomAction("CharacterControllerForwardAxisAction", &CharacterControllerForwardAxisAction{})
}
