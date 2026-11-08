package visitfriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("VisitFriendsBtwSellSwipeAction", &SwipeAction{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsBtwSellFinishedRecognition", &FinishedRecognition{})
}
