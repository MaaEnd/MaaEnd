package exprcoord

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomRecognition("MpExprTemplateMatch", &MpExprTemplateMatchRecognition{})
	maa.AgentServerRegisterCustomRecognition("MpExprOCR", &MpExprOCRRecognition{})
	maa.AgentServerRegisterCustomRecognition("MpExprOCRBottomStable", &MpExprOCRBottomStableRecognition{})
	maa.AgentServerRegisterCustomAction("MpExprClick", &MpExprClickAction{})
	maa.AgentServerRegisterCustomAction("MpExprTouchMove", &MpExprTouchMoveAction{})
	maa.AgentServerRegisterCustomAction("MpExprSwipe", &MpExprSwipeAction{})
	maa.AgentServerRegisterCustomAction("MpExprOCRBottomStateReset", &MpExprOCRBottomStateResetAction{})
}
