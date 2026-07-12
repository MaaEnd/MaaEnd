package processcheck

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.TaskerEventSink = &ProcessChecker{}
)

// Register registers the process checker as a tasker sink
func Register() {
	maa.AgentServerAddTaskerSink(&ProcessChecker{})
}
