package restaurant

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the restaurant decision action with the Maa agent server.
func Register() {
	maa.AgentServerRegisterCustomAction("RestaurantDecisionAction", &DecisionAction{})
}
