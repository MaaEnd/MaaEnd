package aspectratio

import "github.com/MaaXYZ/maa-framework-go/v3"

var (
	_ maa.TaskerEventSink = &AspectRatioChecker{}
)

// Register registers the aspect ratio checker as a tasker sink
func Register() {
	maa.AgentServerAddTaskerSink(&AspectRatioChecker{})
}
