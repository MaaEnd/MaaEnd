package autosell

import "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom recognition and action components for autosell package
func Register() {
	maa.AgentServerRegisterCustomRecognition("AutoSellScanItemRecognition", &AutoSellScanItemRecognition{})
	maa.AgentServerRegisterCustomAction("AutoSellItemExecuteItemTaskAction", &AutoSellItemExecuteItemTaskAction{})
	maa.AgentServerRegisterCustomRecognition("AutoSellPriceCompareRecognition", &AutoSellPriceCompareRecognition{})
}
