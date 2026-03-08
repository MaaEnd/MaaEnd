package clearhitcount

import maa "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner = &ClearHitCountAction{}
)

func Register() {
	maa.AgentServerRegisterCustomAction("ClearHitCount", &ClearHitCountAction{})
}
