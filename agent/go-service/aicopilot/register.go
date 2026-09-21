package aicopilot

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the AI copilot components.
func Register() {
	maa.AgentServerRegisterCustomAction("AICopilotStandbyAction", &StandbyAction{})
}
