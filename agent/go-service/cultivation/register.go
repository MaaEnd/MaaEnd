package cultivation

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register installs the material-reading action and visual guard.
func Register() {
	maa.AgentServerRegisterCustomAction("CultivationAction", &Action{})
	maa.AgentServerRegisterCustomRecognition("CultivationGuard", &Guard{})
}
