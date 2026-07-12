package gfnwindow

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.TaskerEventSink = &GFNWindowChecker{}
)

// Register registers the GFN window checker as a tasker sink
func Register() {
	maa.AgentServerAddTaskerSink(&GFNWindowChecker{})
}
