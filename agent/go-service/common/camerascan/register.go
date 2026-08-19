package camerascan

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers camera scan custom actions.
func Register() {
	maa.AgentServerRegisterCustomAction("CameraScanAction", &CameraScanAction{})
}
