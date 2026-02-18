package batchaddfriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomAction("BatchAddFriendsAction", &BatchAddFriendsAction{})
	maa.AgentServerRegisterCustomAction("BatchAddFriendsChangeBatchAction", &BatchAddFriendsChangeBatchAction{})
}
