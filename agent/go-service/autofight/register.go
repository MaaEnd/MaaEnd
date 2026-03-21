package autofight

import "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom recognition and action components for autofight package
func Register() {
	maa.AgentServerRegisterCustomRecognition("AutoFightEntryRecognition", &AutoFightEntryRecognition{})
	maa.AgentServerRegisterCustomRecognition("AutoFightExitRecognition", &AutoFightExitRecognition{})
	maa.AgentServerRegisterCustomRecognition("AutoFightPauseRecognition", &AutoFightPauseRecognition{})
	maa.AgentServerRegisterCustomRecognition("AutoFightExecuteRecognition", &AutoFightExecuteRecognition{})
	maa.AgentServerRegisterCustomAction("AutoFightExecuteAction", &AutoFightExecuteAction{})
}
