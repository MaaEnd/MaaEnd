package friendvisit

import maa "github.com/MaaXYZ/maa-framework-go/v3"

// Register registers all custom action components for friend visit.
func Register() {
	maa.AgentServerRegisterCustomAction("FriendVisitResetAction", &FriendVisitResetAction{})
	maa.AgentServerRegisterCustomAction("FriendVisitScanAction", &FriendVisitScanAction{})
	maa.AgentServerRegisterCustomAction("FriendVisitCheckQuotaAction", &FriendVisitCheckQuotaAction{})
	maa.AgentServerRegisterCustomAction("FriendVisitFirstEntryExitAction", &FriendVisitFirstEntryExitAction{})
}
