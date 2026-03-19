package aspectratio

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.TaskerEventSink = &AspectRatioChecker{}
)

// Register registers the aspect ratio checker as a tasker sink
func Register() {
	maa.AgentServerAddTaskerSink(&AspectRatioChecker{})
}
