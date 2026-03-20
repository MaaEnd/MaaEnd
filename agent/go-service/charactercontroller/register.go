package charactercontroller

import "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom recognition and action components for charactercontroller package
func Register() {
	maa.AgentServerRegisterCustomAction("CharacterControllerYawDeltaAction", &CharacterControllerYawDeltaAction{})
	maa.AgentServerRegisterCustomAction("CharacterControllerPitchDeltaAction", &CharacterControllerPitchDeltaAction{})
	maa.AgentServerRegisterCustomAction("CharacterControllerForwardAxisAction", &CharacterControllerForwardAxisAction{})
	maa.AgentServerRegisterCustomAction("CharacterMoveToTargetAction", &CharacterMoveToTargetAction{})
	maa.AgentServerRegisterCustomAction("CharacterMoveToTargetNotFoundAction", &CharacterMoveToTargetNotFoundAction{})
}
