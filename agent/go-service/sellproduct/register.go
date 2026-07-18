package sellproduct

import "github.com/MaaXYZ/maa-framework-go/v4"

// Register 向 MaaAgentServer 注册本包中的所有 Custom 组件。
func Register() {
	maa.AgentServerRegisterCustomRecognition(priorityItemRecognitionName, &PriorityItemRecognition{})
	maa.AgentServerRegisterCustomRecognition(selectBestOperatorRecognitionName, &SelectBestOperatorRecognition{})
	maa.AgentServerRegisterCustomRecognition(currentBestOperatorRecognitionName, &CurrentBestOperatorRecognition{})
	maa.AgentServerRegisterCustomRecognition(operatorCacheReadyRecognitionName, &OperatorCacheReadyRecognition{})
	maa.AgentServerRegisterCustomRecognition(operatorListBottomRecognitionName, &OperatorListBottomRecognition{})
	maa.AgentServerRegisterCustomRecognition(operatorScanOutcomeRecognitionName, &OperatorScanOutcomeRecognition{})
	maa.AgentServerRegisterCustomAction(operatorSessionActionName, &OperatorSessionAction{})
	maa.AgentServerRegisterCustomAction(reserveSessionActionName, &ReserveSessionAction{})
	maa.AgentServerRegisterCustomAction(prioritySessionActionName, &PrioritySessionAction{})
}
