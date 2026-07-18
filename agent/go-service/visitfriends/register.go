package visitfriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("VisitFriendsMainAction", &VisitFriendsMainAction{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsSelectFriendRecognition", &VisitFriendsSelectFriendRecognition{})
	// 旧识别器保留注册，待完全切换后删除
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanTargetFriendOpenRecognition", &VisitFriendsMenuScanTargetFriendOpenRecognition{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuScanTargetFriendOpenAction", &VisitFriendsMenuScanTargetFriendOpenAction{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanDetailClueExchangeRecognition", &VisitFriendsMenuScanDetailClueExchangeRecognition{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanDetailAssistRecognition", &VisitFriendsMenuScanDetailAssistRecognition{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanScrollFinishRecognition", &VisitFriendsMenuScanScrollFinishRecognition{})
	maa.AgentServerRegisterCustomRecognition("VisitFriendsMenuScanScrollFullRecognition", &VisitFriendsMenuScanScrollFullRecognition{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuClueExchangeAction", &VisitFriendsMenuClueExchangeAction{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuClueAssistAction", &VisitFriendsMenuClueAssistAction{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuClueExchangeFullAction", &VisitFriendsMenuClueExchangeFullAction{})
	maa.AgentServerRegisterCustomAction("VisitFriendsMenuClueAssistFullAction", &VisitFriendsMenuClueAssistFullAction{})
}
