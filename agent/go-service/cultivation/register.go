package cultivation

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register installs the native cultivation data actions and visual guard.
func Register() {
	maa.AgentServerRegisterCustomAction("CultivationAction", &Action{})
	maa.AgentServerRegisterCustomRecognition("CultivationGuard", &Guard{})
}
