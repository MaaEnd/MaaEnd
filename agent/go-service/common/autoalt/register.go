package autoalt

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("AutoAltClickAction", &AutoAltClickAction{})
	maa.AgentServerRegisterCustomAction("AutoAltSwipeAction", &AutoAltSwipeAction{})
}
