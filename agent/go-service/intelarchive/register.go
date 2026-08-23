package intelarchive

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers Intel Archive custom components.
func Register() {
	maa.AgentServerRegisterCustomRecognition("IntelArchiveScanItemsRecognition", &ScanItemsRecognition{})
	maa.AgentServerRegisterCustomRecognition("IntelArchiveScanDetailRecognition", &ScanDetailRecognition{})
	maa.AgentServerRegisterCustomAction("IntelArchiveResolveTruncAction", &ResolveTruncAction{})
	maa.AgentServerRegisterCustomAction("IntelArchiveResetSessionAction", &ResetSessionAction{})
	maa.AgentServerRegisterCustomAction("IntelArchiveShowInventoryAction", &ShowInventoryAction{})
}
