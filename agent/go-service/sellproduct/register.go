package sellproduct

import "github.com/MaaXYZ/maa-framework-go/v4"

// Register 向 MaaAgentServer 注册本包中的所有 Custom 组件。
func Register() {
	maa.AgentServerRegisterCustomRecognition(componentName, &NormalizedMatchRecognition{})
}
