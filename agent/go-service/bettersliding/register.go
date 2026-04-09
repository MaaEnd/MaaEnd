package bettersliding

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the BetterSliding custom action.
func Register() {
	maa.AgentServerRegisterCustomAction(betterSlidingActionName, &BetterSlidingAction{})
}
