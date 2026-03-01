package common

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册通用自定义动作。
func Register() {
	maa.AgentServerRegisterCustomAction("ScreenShot", &ScreenShot{})
	maa.AgentServerRegisterCustomAction("RunNode", &RunNode{})
}
