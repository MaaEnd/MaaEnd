package sprayscan

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers spray scan custom actions.
func Register() {
	maa.AgentServerRegisterCustomAction("WorldToolSprayScanAction", &WorldToolSprayScanAction{})
}
