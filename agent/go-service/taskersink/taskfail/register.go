package taskfail

import maa "github.com/MaaXYZ/maa-framework-go/v4"

var _ maa.TaskerEventSink = &Sink{}

// Register registers the task failure feedback sink.
func Register() {
	maa.AgentServerAddTaskerSink(&Sink{})
}
