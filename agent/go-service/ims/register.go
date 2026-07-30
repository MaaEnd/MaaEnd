package ims

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers IMS custom components.
func Register() {
	maa.AgentServerRegisterCustomRecognition("ItemDataReady", &ItemDataReady{})
	maa.AgentServerRegisterCustomRecognition("ItemQuantitySatisfied", &ItemQuantitySatisfied{})
	maa.AgentServerRegisterCustomAction("AddItemData", &AddItemData{})
	maa.AgentServerRegisterCustomAction("SyncItemData", &SyncItemData{})
	maa.AgentServerRegisterCustomAction("UpdateItemQuantity", &UpdateItemQuantity{})
}
