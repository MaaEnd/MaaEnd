package killproc

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the KillProcess custom action.
func Register() {
	maa.AgentServerRegisterCustomAction("KillProcess", &KillProcAction{})
}
