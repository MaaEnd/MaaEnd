package friendvisit

import maa "github.com/MaaXYZ/maa-framework-go/v3"

// Register registers friend visit custom components.
func Register() {
	maa.AgentServerRegisterCustomAction("ProductionAssistOCRAction", &ProductionAssistOCRAction{})
	maa.AgentServerRegisterCustomAction("ClueExchangeOCRAction", &ClueExchangeOCRAction{})
}
