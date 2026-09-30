package automission

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册 AutoMission 包的自定义组件。
func Register() {
	maa.AgentServerRegisterCustomRecognition("AutoMissionMatchQuest", &MatchQuestRecognition{})
}
