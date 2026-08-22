package goods

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register 注册货品相关的全部 SellProduct Custom 组件。
func Register() {
	maa.AgentServerRegisterCustomRecognition(priorityItemRecognitionName, &PriorityItemRecognition{})
	maa.AgentServerRegisterCustomRecognition(currentGoodsRecognitionName, &CurrentGoodsRecognition{})
	maa.AgentServerRegisterCustomAction(reserveSessionActionName, &ReserveSessionAction{})
	maa.AgentServerRegisterCustomAction(prioritySessionActionName, &PrioritySessionAction{})
}
