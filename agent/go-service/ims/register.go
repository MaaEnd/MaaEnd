package ims

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers IMS custom components.
func Register() {
	maa.AgentServerRegisterCustomRecognition("ItemDataReady", &ItemDataReady{})
	maa.AgentServerRegisterCustomAction("SyncItemData", &SyncItemData{})
}
