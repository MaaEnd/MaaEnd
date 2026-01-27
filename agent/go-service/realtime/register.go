package realtime

import "github.com/MaaXYZ/maa-framework-go/v3"

// Register registers all custom recognition and action components for realtime package
func Register() {
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightEntryRecognition", &RealTimeAutoFightEntryRecognition{})
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightExitRecognition", &RealTimeAutoFightExitRecognition{})
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightSkillRecognition", &RealTimeAutoFightSkillRecognition{})
	maa.AgentServerRegisterCustomAction("RealTimeAutoFightSkillAction", &RealTimeAutoFightSkillAction{})
	maa.AgentServerRegisterCustomRecognition("RealTimeAutoFightEndSkillRecognition", &RealTimeAutoFightEndSkillRecognition{})
	maa.AgentServerRegisterCustomAction("RealTimeAutoFightEndSkillAction", &RealTimeAutoFightEndSkillAction{})
}
