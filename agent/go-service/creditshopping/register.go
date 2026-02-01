package creditshopping

import maa "github.com/MaaXYZ/maa-framework-go/v3"

// Register registers all custom action components for creditshopping package
func Register() {
	maa.AgentServerRegisterCustomAction("CreditShoppingAction", &CreditShoppingAction{})
}
