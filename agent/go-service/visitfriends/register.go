package visitfriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("VisitFriendsMainAction", &VisitFriendsMainAction{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanTargetFriendOpenRecognition", &VisitFriendsMenuScanTargetFriendOpenRecognition{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuScanTargetFriendOpenAction", &VisitFriendsMenuScanTargetFriendOpenAction{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanDetailSaveRecognition", &VisitFriendsMenuScanDetailSaveRecognition{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanScrollFinishRecognition", &VisitFriendsMenuScanScrollFinishRecognition{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanScrollFullRecognition", &VisitFriendsMenuScanScrollFullRecognition{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuClueExchangeAction", &VisitFriendsMenuClueExchangeAction{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuClueAssistAction", &VisitFriendsMenuClueAssistAction{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuClueExchangeFullAction", &VisitFriendsMenuClueExchangeFullAction{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuClueAssistFullAction", &VisitFriendsMenuClueAssistFullAction{})
}
