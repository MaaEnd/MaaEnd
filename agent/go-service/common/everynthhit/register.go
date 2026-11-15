package everynthhit

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册 EveryNthHit 计数识别与 EveryNthHitReset 复位动作。
func Register() {
	maa.AgentServerRegisterCustomRecognition(recognitionName, &EveryNthHit{})
	maa.AgentServerRegisterCustomAction(actionName, &EveryNthHitReset{})
}
