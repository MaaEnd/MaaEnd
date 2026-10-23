package touchcheck

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers touch-input preflight, forwarding and task-state cleanup.
func Register() {
	maa.AgentServerRegisterCustomAction("TouchInputCheck", &Action{})
	maa.AgentServerRegisterCustomAction("TouchInputAction", &GuardedAction{})
	maa.AgentServerAddTaskerSink(&Sink{})
}
