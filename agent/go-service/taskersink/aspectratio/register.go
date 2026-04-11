package aspectratio

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.TaskerEventSink  = &AspectRatioChecker{}
	_ maa.ContextEventSink = &AspectRatioChecker{}
)

// Register registers the aspect ratio checker as a tasker sink
func Register() {
	sink := &AspectRatioChecker{}
	maa.AgentServerAddTaskerSink(sink)
	maa.AgentServerAddContextSink(sink)
}
