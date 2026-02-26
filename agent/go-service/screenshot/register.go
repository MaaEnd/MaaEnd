package screenshot

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册截图保存相关自定义动作
func Register() {
	maa.AgentServerRegisterCustomAction("ScreenShot", &ScreenShot{})
}
