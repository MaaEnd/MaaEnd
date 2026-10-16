package combatprotocol

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers all custom components of the combat protocol container:
// the run entry action, the sanity accumulator, the coupon flag setter, the
// rotation state recognition and the task outcome sink.
func Register() {
	maa.AgentServerRegisterCustomAction("CombatProtocolEnter", &EnterAction{})
	maa.AgentServerRegisterCustomAction("CombatProtocolAccumulate", &AccumulateAction{})
	maa.AgentServerRegisterCustomAction("CombatProtocolSetCoupon", &SetCouponAction{})
	maa.AgentServerRegisterCustomRecognition("CombatProtocolState", &StateRecognition{})
	maa.AgentServerAddTaskerSink(&Sink{})
}
