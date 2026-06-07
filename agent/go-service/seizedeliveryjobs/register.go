package seizedeliveryjobs

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
	maa.AgentServerRegisterCustomRecognition("SeizeDeliveryJobsScanTargetRecognition", &SeizeDeliveryJobsScanTargetRecognition{})
	maa.AgentServerRegisterCustomAction("SeizeDeliveryJobsScanTargetAction", &SeizeDeliveryJobsScanTargetAction{})
	maa.AgentServerRegisterCustomAction("SeizeDeliveryJobsResetScanStateAction", &SeizeDeliveryJobsResetScanStateAction{})
}
