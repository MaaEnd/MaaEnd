package rtsscheck

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.TaskerEventSink = &RTSSChecker{}
)

// Register registers the RTSS OSD checker as a tasker sink.
func Register() {
	maa.AgentServerAddTaskerSink(&RTSSChecker{})
}
