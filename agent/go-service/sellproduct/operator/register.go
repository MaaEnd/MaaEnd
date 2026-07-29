package operator

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册干员相关的全部 SellProduct Custom 组件。
func Register() {
	maa.AgentServerRegisterCustomRecognition(selectBestOperatorRecognitionName, &SelectBestOperatorRecognition{})
	maa.AgentServerRegisterCustomRecognition(currentBestOperatorRecognitionName, &CurrentBestOperatorRecognition{})
	maa.AgentServerRegisterCustomRecognition(currentOperatorUncachedRecognitionName, &CurrentOperatorUncachedRecognition{})
	maa.AgentServerRegisterCustomRecognition(operatorCacheReadyRecognitionName, &OperatorCacheReadyRecognition{})
	maa.AgentServerRegisterCustomRecognition(operatorListBottomRecognitionName, &OperatorListBottomRecognition{})
	maa.AgentServerRegisterCustomRecognition(operatorScanOutcomeRecognitionName, &OperatorScanOutcomeRecognition{})
	maa.AgentServerRegisterCustomRecognition(operatorConflictRecognitionName, &OperatorConflictRecognition{})
	maa.AgentServerRegisterCustomAction(operatorSessionActionName, &OperatorSessionAction{})
}
