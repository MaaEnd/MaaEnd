package notify

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册 NotifySendAction 自定义动作与任务失败事件监听。
func Register() {
	maa.AgentServerRegisterCustomAction("NotifySendAction", &NotifySendAction{})
	maa.AgentServerAddTaskerSink(&Sink{})
	maa.AgentServerAddContextSink(&ConfigSink{})
}
