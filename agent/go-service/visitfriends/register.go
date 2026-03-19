package visitfriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

var (
	_ maa.CustomActionRunner      = &VisitFriendsMainAction{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanTargetFriendOpenRecognition{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuScanTargetFriendOpenAction{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanDetailSaveRecognition{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanScrollFinishRecognition{}
	_ maa.CustomRecognitionRunner = &VisitFriendsMenuScanScrollFullRecognition{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuClueExchangeAction{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuClueAssistAction{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuClueExchangeFullAction{}
	_ maa.CustomActionRunner      = &VisitFriendsMenuClueAssistFullAction{}
)

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
