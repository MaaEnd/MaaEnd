package notify

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册任务失败事件监听。
func Register() {
	maa.AgentServerAddTaskerSink(&Sink{})
	maa.AgentServerAddContextSink(&ConfigSink{})
}
