package aerosalvage

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the Aerial Salvage custom components.
func Register() {
	maa.AgentServerRegisterCustomAction(aeroSalvageConfigureSwipeActionName, &ConfigureSwipeAction{})
	maa.AgentServerRegisterCustomRecognition(aeroSalvageBalloonStateRecognitionName, &BalloonStateRecognition{})
	maa.AgentServerRegisterCustomRecognition("AeroSalvageGridRecognition", &GridRecognition{})
	maa.AgentServerRegisterCustomRecognition("AeroSalvageInitialStateRecognition", &InitialStateRecognition{})
}
