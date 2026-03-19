package hdrcheck

import "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.TaskerEventSink = &HDRChecker{}
)

// Register registers the HDR checker as a tasker sink
func Register() {
	maa.AgentServerAddTaskerSink(&HDRChecker{})
}
