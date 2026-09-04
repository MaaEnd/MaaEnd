package stashbackpack

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers StashBackpack state and queue components.
func Register() {
	maa.AgentServerRegisterCustomAction("StashBackpackSnapshotAction", &SnapshotAction{})
	maa.AgentServerRegisterCustomAction("StashBackpackStateAction", &StateAction{})
	maa.AgentServerRegisterCustomAction("StashBackpackShiftClickAction", &ShiftClickAction{})
	maa.AgentServerRegisterCustomAction("StashBackpackWarningAction", &WarningAction{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackDepotRecognition", &DepotRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackBagPageRecognition", &BagPageRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackBagPageFailedRecognition", &BagPageFailedRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackBagTargetsExhaustedRecognition", &BagTargetsExhaustedRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackNextItemRecognition", &NextItemRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackRetrievedItemRecognition", &RetrievedItemRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackTargetCategoryRecognition", &TargetCategoryRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackFullCompleteRecognition", &FullCompleteRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackPlatformSupportedRecognition", &PlatformSupportedRecognition{})
	maa.AgentServerRegisterCustomRecognition("StashBackpackSnapshotChangedRecognition", &SnapshotChangedRecognition{})
}
