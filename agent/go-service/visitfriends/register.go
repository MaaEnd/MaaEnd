package visitfriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("VisitFriendsBtwSellSlidingAction", &SlidingAction{})
	maa.AgentServerRegisterCustomAction("VisitFriendsBtwSellSettleAction", &SettleAction{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsBtwSellFinishedRecognition", &FinishedRecognition{})
}
