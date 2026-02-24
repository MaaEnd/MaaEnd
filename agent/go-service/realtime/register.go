package realtime

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomRecognitionRunner = &RealTimeAutoFightEntryRecognition{}
	_ maa.CustomRecognitionRunner = &RealTimeAutoFightExitRecognition{}
	_ maa.CustomRecognitionRunner = &RealTimeAutoFightSkillRecognition{}
	_ maa.CustomRecognitionRunner = &RealTimeAutoFightEndSkillRecognition{}
	_ maa.CustomRecognitionRunner = &RealTimeAutoFightDodgeRecognition{}
	_ maa.CustomActionRunner      = &RealTimeAutoFightSkillAction{}
	_ maa.CustomActionRunner      = &RealTimeAutoFightEndSkillAction{}
	_ maa.CustomActionRunner      = &RealTimeAutoFightDodgeAction{}
)

// Register registers all custom recognition and action components for realtime package
func Register() {
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightEntryRecognition", &RealTimeAutoFightEntryRecognition{})
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightExitRecognition", &RealTimeAutoFightExitRecognition{})
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightSkillRecognition", &RealTimeAutoFightSkillRecognition{})
	maa.AgentServerRegisterCustomAction("RealTimeAutoFightSkillAction", &RealTimeAutoFightSkillAction{})
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightEndSkillRecognition", &RealTimeAutoFightEndSkillRecognition{})
	maa.AgentServerRegisterCustomAction("RealTimeAutoFightEndSkillAction", &RealTimeAutoFightEndSkillAction{})
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightDodgeRecognition", &RealTimeAutoFightDodgeRecognition{})
	maa.AgentServerRegisterCustomAction("RealTimeAutoFightDodgeAction", &RealTimeAutoFightDodgeAction{})
}
