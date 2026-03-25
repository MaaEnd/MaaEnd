package quantizedsliding

import maa "github.com/MaaXYZ/maa-framework-go/v4"

// Register registers the quantized sliding custom action.
func Register() {
	maa.AgentServerRegisterCustomAction("QuantizedSliding", &QuantizedSlidingAction{})
}
