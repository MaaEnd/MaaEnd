package batchaddfriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("BatchAddFriendsAction", &BatchAddFriendsAction{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsUIDOnAddAction", &BatchAddFriendsUIDOnAddAction{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsUIDOnEmptyAction", &BatchAddFriendsUIDOnEmptyAction{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsUIDFinishAction", &BatchAddFriendsUIDFinishAction{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsStrangersOnAddAction", &BatchAddFriendsStrangersOnAddAction{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsStrangersFinishAction", &BatchAddFriendsStrangersFinishAction{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsFriendListFullAction", &BatchAddFriendsFriendListFullAction{})
	maa.AgentServerRegisterCustomRecognition("BatchAddFriendsStrangersPickRowRecognition", &PickStrangerRowRecognition{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsStrangersRecordRejectAction", &RecordRejectAction{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsStrangersClearRejectsAction", &ClearRejectsAction{})
}
