package autosell

import "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom recognition and action components for autosell package
func Register() {
	maa.AgentServerRegisterCustomRecognition("AutoSellPriceCompareRecognition", &AutoSellPriceCompareRecognition{})
	maa.AgentServerRegisterCustomAction("AutoSellItemRecordAction", &AutoSellItemRecordAction{})
	maa.AgentServerRegisterCustomRecognition("AutoSellStockRedistributionOpenItemTextRecognition", &AutoSellStockRedistributionOpenItemTextRecognition{})
	maa.AgentServerRegisterCustomAction("AutoSellStockRedistributionOpenItemTextAction", &AutoSellStockRedistributionOpenItemTextAction{})
}
