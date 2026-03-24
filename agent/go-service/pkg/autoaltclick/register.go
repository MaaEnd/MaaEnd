package autoaltclick

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("AutoAltClickAction", &AutoAltClickAction{})
}
