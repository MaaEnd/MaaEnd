package visitfriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomRecognition("VisitFriendsSelectFriendRecognition", &VisitFriendsSelectFriendRecognition{})
}
