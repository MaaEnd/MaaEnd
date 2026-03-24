package clearhitcount

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("ClearHitCount", &ClearHitCountAction{})
}
