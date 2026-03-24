package creditshopping

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom action components for creditshopping package
func Register() {
	maa.AgentServerRegisterCustomAction("CreditShoppingParseParams", &CreditShoppingParseParams{})
}
