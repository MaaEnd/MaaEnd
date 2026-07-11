package failurecollector

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("FailureCollectorReset", &ResetAction{})
	maa.AgentServerRegisterCustomAction("FailureCollectorRunTask", &RunTaskAction{})
	maa.AgentServerRegisterCustomAction("FailureCollectorFinish", &FinishAction{})
}
