package batchdeletefriends

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers BatchDeleteFriends custom components.
func Register() {
	maa.AgentServerRegisterCustomRecognition(
		"BatchDeleteFriendsInactiveRecognition",
		&InactiveRecognition{},
	)
}
