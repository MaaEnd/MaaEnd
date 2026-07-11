package environmentmonitoring

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("EnvironmentMonitoringReset", &ResetAction{})
	maa.AgentServerRegisterCustomAction("EnvironmentMonitoringSetCurrentRoute", &SetCurrentRouteAction{})
	maa.AgentServerRegisterCustomAction("EnvironmentMonitoringRecordFailure", &RecordFailureAction{})
	maa.AgentServerRegisterCustomAction("EnvironmentMonitoringFinish", &FinishAction{})
}
