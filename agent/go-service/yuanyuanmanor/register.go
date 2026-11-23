package yuanyuanmanor

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the innkeeping decision action with the Maa agent server.
func Register() {
	maa.AgentServerRegisterCustomAction("YuanyuanManorInnkeepingDecide", &DecisionAction{})
}
