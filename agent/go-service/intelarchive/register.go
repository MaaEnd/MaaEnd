package intelarchive

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers Intel Archive custom components.
func Register() {
	maa.AgentServerRegisterCustomRecognition("IntelArchiveScanItems", &ScanItems{})
	maa.AgentServerRegisterCustomAction("IntelArchiveResolveTruncatedItems", &ResolveTruncatedItems{})
}
