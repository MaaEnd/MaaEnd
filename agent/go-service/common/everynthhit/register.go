package everynthhit

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the EveryNthHit components.
func Register() {
	maa.AgentServerRegisterCustomRecognition(recognitionName, &EveryNthHit{})
	maa.AgentServerRegisterCustomAction(actionName, &EveryNthHitReset{})
	maa.AgentServerAddTaskerSink(&stateResetSink{})
}
