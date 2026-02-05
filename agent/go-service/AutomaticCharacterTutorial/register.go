package AutomaticCharacterTutorial

import (
	"github.com/MaaXYZ/maa-framework-go/v3"
	"github.com/rs/zerolog/log"
)

// Register registers the custom components
// 注册自定义组件
func Register() {
	// Register Custom Recognition
	// 注册自定义识别，名字 "DynamicMatchRecognition" 将在 Pipeline JSON 中使用
	maa.AgentServerRegisterCustomRecognition("DynamicMatchRecognition", &DynamicMatchRecognition{})

	// Register Custom Action
	// 注册自定义动作，名字 "DynamicMatchAction" 将在 Pipeline JSON 中使用
	maa.AgentServerRegisterCustomAction("DynamicMatchAction", &DynamicMatchAction{})

	// Register Ultimate Skill Components
	// 注册终结技组件
	maa.AgentServerRegisterCustomRecognition("UltimateSkillRecognition", &UltimateSkillRecognition{})
	maa.AgentServerRegisterCustomAction("UltimateSkillAction", &UltimateSkillAction{})

	log.Info().Msg("Registered example components for dynamic matching and ultimate skills")
}
