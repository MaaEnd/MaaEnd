package subtask

import maa "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner = &SubTaskAction{}
)

func Register() {
	maa.AgentServerRegisterCustomAction("SubTask", &SubTaskAction{})
}
