package baker

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner = &NodeClearHitCountAction{}
)

// 为导入任务包注册所有自定义动作组件
func Register() {
	maa.AgentServerRegisterCustomAction("NodeClearHitCountAction", &NodeClearHitCountAction{})
}
